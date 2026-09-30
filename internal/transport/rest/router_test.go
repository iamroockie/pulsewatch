package rest_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"uuid"

	"github.com/iamroockie/plinth"
	"github.com/iamroockie/plinth/plinthtest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/iamroockie/pulsewatch/internal/domain"
	"github.com/iamroockie/pulsewatch/internal/transport/rest"
)

const requestCount = "pulsewatch_http_request_duration_seconds_count"

func discardLog() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func newRouter(tb testing.TB, log *slog.Logger, checkErr error) http.Handler {
	tb.Helper()

	ctrl := gomock.NewController(tb)
	probes := map[string]plinth.CheckFunc{
		"postgres": func(context.Context) error { return checkErr },
	}

	monitors, history := NewMockMonitorService(ctrl), NewMockHistoryService(ctrl)

	return rest.NewRouter(log, prometheus.NewRegistry(), probes, monitors, history)
}

func testRouter(tb testing.TB, checkErr error) http.Handler {
	tb.Helper()

	return newRouter(tb, discardLog(), checkErr)
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

func TestRouterProbesAndMetricsStayOutOfRequestLog(t *testing.T) {
	var buf bytes.Buffer

	h := newRouter(t, slog.New(slog.NewTextHandler(&buf, nil)), nil)

	serve(t, h, http.MethodGet, "/healthz")
	serve(t, h, http.MethodGet, "/readyz")
	serve(t, h, http.MethodGet, "/metrics")
	require.Empty(t, buf.String())

	serve(t, h, http.MethodGet, "/nope")
	assert.Contains(t, buf.String(), "/nope")
}

func TestRouterMetricsLabelRequestsByRoute(t *testing.T) {
	tests := map[string]struct {
		prepare func(*MockMonitorService)
		targets []string
		want    string
	}{
		"route pattern instead of path": {
			prepare: func(svc *MockMonitorService) {
				svc.EXPECT().Get(gomock.Any(), gomock.Any()).Return(storedMonitor(), nil).Times(2)
			},
			targets: []string{"/monitors/" + uuid.NewV7().String(), monitorPath()},
			want:    requestCount + `{code="200",route="GET /monitors/{id}"} 2`,
		},
		"unknown paths share one series": {
			prepare: func(*MockMonitorService) {},
			targets: []string{"/nope", "/wp-admin/install.php"},
			want:    requestCount + `{code="404",route=""} 2`,
		},
		"panic is a server error": {
			prepare: func(svc *MockMonitorService) {
				svc.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(
					func(context.Context, uuid.UUID) (*domain.Monitor, error) {
						panic("broken handler")
					},
				)
			},
			targets: []string{monitorPath()},
			want:    requestCount + `{code="500",route="GET /monitors/{id}"} 1`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			monitors := NewMockMonitorService(ctrl)
			test.prepare(monitors)
			reg := prometheus.NewRegistry()
			h := rest.NewRouter(discardLog(), reg, nil, monitors, NewMockHistoryService(ctrl))
			for _, target := range test.targets {
				serve(t, h, http.MethodGet, target)
			}

			rec := serve(t, h, http.MethodGet, "/metrics")

			require.Equal(t, http.StatusOK, rec.Code)
			assert.Contains(t, rec.Body.String(), test.want+"\n")
		})
	}
}
