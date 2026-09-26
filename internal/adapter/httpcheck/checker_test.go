package httpcheck_test

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/iamroockie/pulsewatch/internal/adapter/httpcheck"
	"github.com/iamroockie/pulsewatch/internal/domain"
	"github.com/iamroockie/pulsewatch/internal/domain/domaintest"
)

func newChecker() *httpcheck.Checker {
	return httpcheck.NewChecker(httpcheck.Backoff{Base: time.Millisecond, Max: time.Millisecond})
}

func settingsFor(url string) domain.CheckSettings {
	s := domaintest.ValidSettings()
	s.URL = url

	return s
}

func withoutLatency(r domain.CheckResult) domain.CheckResult {
	r.Latency = 0

	return r
}

func statusServer(t *testing.T, statuses ...int) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := int(hits.Add(1))
		w.WriteHeader(statuses[min(n, len(statuses))-1])
	}))
	t.Cleanup(srv.Close)

	return srv, &hits
}

func TestCheckerCheck(t *testing.T) {
	attempts := domaintest.ValidSettings().MaxRetries + 1
	tests := map[string]struct {
		statuses []int
		want     domain.CheckResult
	}{
		"ok": {
			statuses: []int{http.StatusOK},
			want: domain.CheckResult{
				IsUp:       true,
				StatusCode: http.StatusOK,
				Attempts:   1,
			},
		},
		"not modified is up": {
			statuses: []int{http.StatusNotModified},
			want: domain.CheckResult{
				IsUp:       true,
				StatusCode: http.StatusNotModified,
				Attempts:   1,
			},
		},
		"client error is not retried": {
			statuses: []int{http.StatusNotFound},
			want: domain.CheckResult{
				StatusCode: http.StatusNotFound,
				Attempts:   1,
			},
		},
		"server error is retried": {
			statuses: []int{http.StatusInternalServerError},
			want: domain.CheckResult{
				StatusCode: http.StatusInternalServerError,
				Attempts:   attempts,
			},
		},
		"request timeout is retried": {
			statuses: []int{http.StatusRequestTimeout},
			want: domain.CheckResult{
				StatusCode: http.StatusRequestTimeout,
				Attempts:   attempts,
			},
		},
		"too many requests is retried": {
			statuses: []int{http.StatusTooManyRequests},
			want: domain.CheckResult{
				StatusCode: http.StatusTooManyRequests,
				Attempts:   attempts,
			},
		},
		"recovers on retry": {
			statuses: []int{http.StatusServiceUnavailable, http.StatusOK},
			want: domain.CheckResult{
				IsUp:       true,
				StatusCode: http.StatusOK,
				Attempts:   2,
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			srv, hits := statusServer(t, test.statuses...)

			got, err := newChecker().Check(t.Context(), settingsFor(srv.URL))

			require.NoError(t, err)
			assert.Positive(t, got.Latency)
			assert.Equal(t, test.want, withoutLatency(got))
			assert.Equal(t, test.want.Attempts, hits.Load())
		})
	}
}

func TestCheckerCheckFollowsRedirects(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("/old", http.RedirectHandler("/new", http.StatusFound))
	mux.HandleFunc("/new", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	want := domain.CheckResult{IsUp: true, StatusCode: http.StatusNoContent, Attempts: 1}

	got, err := newChecker().Check(t.Context(), settingsFor(srv.URL+"/old"))

	require.NoError(t, err)
	assert.Equal(t, want, withoutLatency(got))
}

func TestCheckerCheckSendsGetWithUserAgent(t *testing.T) {
	requests := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		requests <- r.Method + " " + r.UserAgent()
	}))
	t.Cleanup(srv.Close)

	_, err := newChecker().Check(t.Context(), settingsFor(srv.URL))

	require.NoError(t, err)
	assert.Equal(t, "GET pulsewatch", <-requests)
}

func TestCheckerCheckTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	s := settingsFor(srv.URL)
	s.Timeout = 100 * time.Millisecond
	want := domain.CheckResult{Attempts: s.MaxRetries + 1, Error: "timeout"}

	got, err := newChecker().Check(t.Context(), s)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, got.Latency, s.Timeout)
	assert.Equal(t, want, withoutLatency(got))
}

func TestCheckerCheckConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	s := settingsFor(srv.URL)

	got, err := newChecker().Check(t.Context(), s)

	require.NoError(t, err)
	assert.False(t, got.IsUp)
	assert.Zero(t, got.StatusCode)
	assert.Equal(t, s.MaxRetries+1, got.Attempts)
	assert.Contains(t, got.Error, "connection refused")
	assert.NotContains(t, got.Error, srv.URL)
}

func TestCheckerCheckRejectsUnknownCertificate(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.NotFoundHandler())
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)

	got, err := newChecker().Check(t.Context(), settingsFor(srv.URL))

	require.NoError(t, err)
	assert.False(t, got.IsUp)
	assert.Zero(t, got.StatusCode)
	assert.Contains(t, got.Error, "certificate")
}

func TestCheckerCheckCanceledDuringAttempt(t *testing.T) {
	entered := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		<-entered
		cancel()
	}()
	s := settingsFor(srv.URL)
	s.MaxRetries = 0
	start := time.Now()

	got, err := newChecker().Check(ctx, s)

	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(start), s.Timeout)
	assert.Zero(t, got)
}

func TestCheckerCheckCanceledDuringBackoff(t *testing.T) {
	srv, hits := statusServer(t, http.StatusServiceUnavailable)
	checker := httpcheck.NewChecker(httpcheck.Backoff{Base: time.Hour, Max: time.Hour})
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	t.Cleanup(cancel)

	got, err := checker.Check(ctx, settingsFor(srv.URL))

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Zero(t, got)
	assert.Equal(t, int32(1), hits.Load())
}

func TestCheckerCheckOpensNewConnectionEachTime(t *testing.T) {
	var conns atomic.Int32
	ok := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	srv := httptest.NewUnstartedServer(ok)
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)
	checker := newChecker()
	s := settingsFor(srv.URL)

	for range 3 {
		_, err := checker.Check(t.Context(), s)
		require.NoError(t, err)
	}

	assert.Equal(t, int32(3), conns.Load())
}

func TestCheckerCheckConcurrentCalls(t *testing.T) {
	srv, hits := statusServer(t, http.StatusServiceUnavailable)
	checker := newChecker()
	s := settingsFor(srv.URL)
	errs := make([]error, 10)
	var wg sync.WaitGroup

	for i := range errs {
		wg.Go(func() {
			_, errs[i] = checker.Check(t.Context(), s)
		})
	}
	wg.Wait()

	require.NoError(t, errors.Join(errs...))
	assert.Equal(t, int32(len(errs))*(s.MaxRetries+1), hits.Load())
}
