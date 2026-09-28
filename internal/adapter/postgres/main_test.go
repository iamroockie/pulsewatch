package postgres_test

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/iamroockie/pulsewatch/internal/adapter/postgres"
	"github.com/iamroockie/pulsewatch/migrations"
)

const (
	postgresImage  = "postgres:18.6-trixie"
	templateDB     = "pulsewatch_template"
	templateDSNEnv = "PULSEWATCH_TEST_TEMPLATE_DSN"
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	flag.Parse()
	if testing.Short() {
		return m.Run()
	}

	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, postgresImage,
		tcpostgres.WithDatabase(templateDB),
		tcpostgres.WithUsername("pulsewatch"),
		tcpostgres.WithPassword("pulsewatch"),
		tcpostgres.BasicWaitStrategies(),
	)
	defer func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			fmt.Fprintln(os.Stderr, "terminate postgres:", err)
		}
	}()
	if err != nil {
		fmt.Fprintln(os.Stderr, "start postgres:", err)
		return 1
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintln(os.Stderr, "postgres dsn:", err)
		return 1
	}

	if err := migrate(ctx, dsn); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		return 1
	}

	if err := os.Setenv(templateDSNEnv, dsn); err != nil {
		fmt.Fprintln(os.Stderr, "set template dsn:", err)
		return 1
	}

	return m.Run()
}

func migrate(ctx context.Context, dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	migrationsFS, err := fs.Sub(migrations.SQL, "sql")
	if err != nil {
		return err
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrationsFS)
	if err != nil {
		return err
	}

	_, err = provider.Up(ctx)

	return err
}

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if testing.Short() {
		t.Skip("postgres adapter tests need docker")
	}

	templateDSN := os.Getenv(templateDSNEnv)
	name := "test_" + strings.ReplaceAll(uuid.NewV4().String(), "-", "")

	admin, err := pgx.Connect(t.Context(), withDatabase(t, templateDSN, "postgres"))
	require.NoError(t, err)
	defer admin.Close(t.Context())

	_, err = admin.Exec(t.Context(), fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s",
		pgx.Identifier{name}.Sanitize(), pgx.Identifier{templateDB}.Sanitize()))
	require.NoError(t, err)

	pool, err := postgres.NewPool(t.Context(), withDatabase(t, templateDSN, name))
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return pool
}

func withDatabase(t *testing.T, dsn, database string) string {
	t.Helper()

	u, err := url.Parse(dsn)
	require.NoError(t, err)
	u.Path = "/" + database

	return u.String()
}
