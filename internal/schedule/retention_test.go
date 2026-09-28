package schedule_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/iamroockie/pulsewatch/internal/schedule"
)

const (
	keep  = 30 * 24 * time.Hour
	every = time.Hour
)

func newRetention(t *testing.T, log *slog.Logger) (*schedule.Retention, *MockPurger) {
	t.Helper()

	purger := NewMockPurger(gomock.NewController(t))

	return schedule.NewRetention(purger, keep, every, log), purger
}

func TestRetentionPurgesAtStartAndThenEveryPeriod(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, purger := newRetention(t, slog.New(slog.DiscardHandler))
		purger.EXPECT().Purge(gomock.Any(), keep).Return(int64(0), nil).Times(3)

		runFor(t, r, 2*every+every/2)
	})
}

func TestRetentionLogsPurgedChecks(t *testing.T) {
	tests := map[string]struct {
		deleted int64
		err     error
		want    string
	}{
		"nothing to purge": {
			deleted: 0,
			err:     nil,
			want:    "",
		},
		"purged": {
			deleted: 7,
			err:     nil,
			want:    "level=INFO msg=\"checks purged\"\n",
		},
		"failed": {
			deleted: 0,
			err:     errors.New("storage is down"),
			want:    "level=ERROR msg=\"purge checks\"\n",
		},
		"failed halfway": {
			deleted: 3,
			err:     errors.New("storage is down"),
			want:    "level=INFO msg=\"checks purged\"\nlevel=ERROR msg=\"purge checks\"\n",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var buf bytes.Buffer
				r, purger := newRetention(t, levelAndMessageLog(&buf))
				purger.EXPECT().Purge(gomock.Any(), keep).Return(test.deleted, test.err)

				runFor(t, r, every/2)

				assert.Equal(t, test.want, buf.String())
			})
		})
	}
}

func TestRetentionKeepsRunningAfterError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, purger := newRetention(t, slog.New(slog.DiscardHandler))
		errStorage := errors.New("storage is down")
		gomock.InOrder(
			purger.EXPECT().Purge(gomock.Any(), keep).Return(int64(0), errStorage),
			purger.EXPECT().Purge(gomock.Any(), keep).Return(int64(0), nil),
		)

		runFor(t, r, every+every/2)
	})
}

func TestRetentionStopsWithoutLoggingInterruptedPurge(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var buf bytes.Buffer
		r, purger := newRetention(t, levelAndMessageLog(&buf))
		purger.EXPECT().Purge(gomock.Any(), keep).DoAndReturn(
			func(ctx context.Context, _ time.Duration) (int64, error) {
				<-ctx.Done()

				return 0, ctx.Err()
			},
		)

		runFor(t, r, every/2)

		assert.Empty(t, buf.String())
	})
}
