package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iamroockie/plinth"
	"github.com/iamroockie/plinth/plinthtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testRouter(tb testing.TB, checkErr error) http.Handler {
	tb.Helper()

	return router(slog.New(slog.DiscardHandler), map[string]plinth.CheckFunc{
		"postgres": func(context.Context) error { return checkErr },
	})
}

func serve(tb testing.TB, h http.Handler, method, target string) *httptest.ResponseRecorder {
	tb.Helper()

	rec, _ := plinthtest.Serve(tb, h, plinthtest.NewRequest(tb, method, target, nil))

	return rec
}

func TestRouterHealthz(t *testing.T) {
	rec := serve(t, testRouter(t, nil), http.MethodGet, "/healthz")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", plinthtest.DecodeJSON[map[string]string](t, rec)["status"])
	assert.NotEmpty(t, rec.Header().Get(plinth.HeaderXRequestID))
}

func TestRouterHealthzIgnoresBrokenDependency(t *testing.T) {
	rec := serve(t, testRouter(t, errors.New("connection refused")), http.MethodGet, "/healthz")

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRouterReadyz(t *testing.T) {
	rec := serve(t, testRouter(t, nil), http.MethodGet, "/readyz")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ready", plinthtest.DecodeJSON[map[string]string](t, rec)["status"])
}

func TestRouterReadyzUnavailable(t *testing.T) {
	rec := serve(t, testRouter(t, errors.New("connection refused")), http.MethodGet, "/readyz")

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, plinth.CodeServiceUnavailable, plinthtest.DecodeError(t, rec).Code)
	assert.NotContains(t, rec.Body.String(), "connection refused")
	assert.NotContains(t, rec.Body.String(), "postgres")
}

func TestRouterUnknownRoute(t *testing.T) {
	rec := serve(t, testRouter(t, nil), http.MethodGet, "/nope")

	require.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, plinth.CodeNotFound, plinthtest.DecodeError(t, rec).Code)
}

func TestRouterMethodNotAllowed(t *testing.T) {
	rec := serve(t, testRouter(t, nil), http.MethodPost, "/healthz")

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, plinth.CodeMethodNotAllowed, plinthtest.DecodeError(t, rec).Code)
	assert.Contains(t, rec.Header().Get(plinth.HeaderAllow), http.MethodGet)
}

func TestRouterProbesStayOutOfRequestLog(t *testing.T) {
	var buf bytes.Buffer

	h := router(slog.New(slog.NewTextHandler(&buf, nil)), map[string]plinth.CheckFunc{
		"postgres": func(context.Context) error { return nil },
	})

	serve(t, h, http.MethodGet, "/healthz")
	serve(t, h, http.MethodGet, "/readyz")
	require.Empty(t, buf.String())

	serve(t, h, http.MethodGet, "/nope")
	assert.Contains(t, buf.String(), "/nope")
}
