package rest_test

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/iamroockie/plinth"
	"github.com/iamroockie/plinth/plinthtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/iamroockie/pulsewatch/internal/domain"
	"github.com/iamroockie/pulsewatch/internal/service"
	"github.com/iamroockie/pulsewatch/internal/transport/rest"
)

const (
	upCheckJSON = `{
		"checked_at": "2026-09-26T11:59:00.123456Z",
		"is_up": true,
		"status_code": 200,
		"latency_ms": 120,
		"attempts": 1,
		"error": null
	}`
	downCheckJSON = `{
		"checked_at": "2026-09-26T11:58:00.123456Z",
		"is_up": false,
		"status_code": null,
		"latency_ms": 5000,
		"attempts": 3,
		"error": "timeout"
	}`
)

func newHistoryRouter(t *testing.T) (http.Handler, *MockHistoryService) {
	t.Helper()

	ctrl := gomock.NewController(t)
	svc := NewMockHistoryService(ctrl)
	h := rest.NewRouter(slog.New(slog.DiscardHandler), nil, NewMockMonitorService(ctrl), svc)

	return h, svc
}

func checksPath() string {
	return monitorPath() + "/checks"
}

func uptimePath() string {
	return monitorPath() + "/uptime"
}

func storedChecks() []*domain.Check {
	return []*domain.Check{
		{
			MonitorID: fixedID(),
			CheckedAt: fixedNow().Add(-time.Minute),
			Result: domain.CheckResult{
				IsUp:       true,
				StatusCode: 200,
				Latency:    120 * time.Millisecond,
				Attempts:   1,
			},
		},
		{
			MonitorID: fixedID(),
			CheckedAt: fixedNow().Add(-2 * time.Minute),
			Result: domain.CheckResult{
				Latency:  5 * time.Second,
				Attempts: 3,
				Error:    "timeout",
			},
		},
	}
}

func TestListChecks(t *testing.T) {
	before := fixedNow().Add(-time.Minute)
	tests := map[string]struct {
		query      string
		wantBefore time.Time
		wantLimit  int
	}{
		"defaults":  {query: "", wantBefore: time.Time{}, wantLimit: 50},
		"min limit": {query: "?limit=1", wantBefore: time.Time{}, wantLimit: 1},
		"max limit": {query: "?limit=100", wantBefore: time.Time{}, wantLimit: 100},
		"before": {
			query:      "?before=2026-09-26T11:59:00.123456Z",
			wantBefore: before,
			wantLimit:  50,
		},
		"before with offset": {
			query:      "?before=2026-09-26T14:59:00.123456%2B03:00",
			wantBefore: before,
			wantLimit:  50,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			h, svc := newHistoryRouter(t)
			svc.EXPECT().
				Checks(gomock.Any(), fixedID(), timeEqual(test.wantBefore), test.wantLimit).
				Return(service.CheckPage{}, nil)

			rec := serveJSON(t, h, http.MethodGet, checksPath()+test.query, nil)

			assert.Equal(t, http.StatusOK, rec.Code)
		})
	}
}

func TestListChecksResponse(t *testing.T) {
	checks := storedChecks()
	tests := map[string]struct {
		page service.CheckPage
		want string
	}{
		"empty": {
			page: service.CheckPage{Items: nil, NextBefore: nil},
			want: `{"items": [], "next_before": null}`,
		},
		"last page": {
			page: service.CheckPage{Items: checks, NextBefore: nil},
			want: fmt.Sprintf(`{"items": [%s, %s], "next_before": null}`,
				upCheckJSON, downCheckJSON),
		},
		"has next page": {
			page: service.CheckPage{Items: checks, NextBefore: &checks[1].CheckedAt},
			want: fmt.Sprintf(`{"items": [%s, %s], "next_before": "2026-09-26T11:58:00.123456Z"}`,
				upCheckJSON, downCheckJSON),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			h, svc := newHistoryRouter(t)
			svc.EXPECT().Checks(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(test.page, nil)

			rec := serveJSON(t, h, http.MethodGet, checksPath(), nil)

			require.Equal(t, http.StatusOK, rec.Code)
			assert.JSONEq(t, test.want, rec.Body.String())
		})
	}
}

func TestListChecksRejectsMalformedQuery(t *testing.T) {
	tests := map[string]struct {
		query string
		want  string
	}{
		"limit not a number": {
			query: "?limit=ten",
			want:  `invalid query parameter "limit"`,
		},
		"before not a time": {
			query: "?before=yesterday",
			want:  `invalid query parameter "before"`,
		},
		"before without zone": {
			query: "?before=2026-09-26T11:59:00",
			want:  `invalid query parameter "before"`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			h, _ := newHistoryRouter(t)

			rec := serveJSON(t, h, http.MethodGet, checksPath()+test.query, nil)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			want := plinthtest.Error{Code: plinth.CodeBadRequest, Message: test.want}
			assert.Equal(t, want, plinthtest.DecodeError(t, rec))
		})
	}
}

func TestListChecksRejectsLimitOutOfRange(t *testing.T) {
	tests := map[string]struct {
		query string
	}{
		"zero":      {query: "?limit=0"},
		"above max": {query: "?limit=101"},
	}
	want := []plinth.FieldViolation{{
		Field:  "limit",
		Code:   "out_of_range",
		Params: plinth.Map{"min": 1.0, "max": 100.0},
	}}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			h, _ := newHistoryRouter(t)

			rec := serveJSON(t, h, http.MethodGet, checksPath()+test.query, nil)

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			got := plinthtest.DecodeError(t, rec)
			assert.Equal(t, plinth.CodeValidation, got.Code)
			assert.Equal(t, want, got.Details)
		})
	}
}

func TestGetUptime(t *testing.T) {
	h, svc := newHistoryRouter(t)
	report := domain.UptimeReport{
		Hour: domain.Uptime{Checks: 0, Up: 0},
		Day:  domain.Uptime{Checks: 4, Up: 3},
		Week: domain.Uptime{Checks: 8, Up: 8},
	}
	want := `{
		"1h": {"checks": 0, "up": 0, "ratio": null},
		"24h": {"checks": 4, "up": 3, "ratio": 0.75},
		"7d": {"checks": 8, "up": 8, "ratio": 1}
	}`
	svc.EXPECT().Uptime(gomock.Any(), fixedID()).Return(report, nil)

	rec := serveJSON(t, h, http.MethodGet, uptimePath(), nil)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, want, rec.Body.String())
}

func TestHistoryRejectsInvalidID(t *testing.T) {
	tests := map[string]struct {
		target string
	}{
		"checks": {target: "/monitors/not-a-uuid/checks"},
		"uptime": {target: "/monitors/not-a-uuid/uptime"},
	}
	want := plinthtest.Error{Code: plinth.CodeBadRequest, Message: `invalid path parameter "id"`}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			h, _ := newHistoryRouter(t)

			rec := serveJSON(t, h, http.MethodGet, test.target, nil)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, want, plinthtest.DecodeError(t, rec))
		})
	}
}

func TestHistoryTranslateServiceErrors(t *testing.T) {
	notFound := fmt.Errorf("list checks: %w", domain.ErrMonitorNotFound)
	failure := errors.New("connection refused")
	tests := map[string]struct {
		target     string
		expect     func(svc *MockHistoryService)
		wantStatus int
		wantCode   plinth.ErrorCode
	}{
		"checks not found": {
			target: checksPath(),
			expect: func(svc *MockHistoryService) {
				svc.EXPECT().Checks(gomock.Any(), fixedID(), gomock.Any(), gomock.Any()).
					Return(service.CheckPage{}, notFound)
			},
			wantStatus: http.StatusNotFound,
			wantCode:   plinth.CodeNotFound,
		},
		"uptime not found": {
			target: uptimePath(),
			expect: func(svc *MockHistoryService) {
				svc.EXPECT().Uptime(gomock.Any(), fixedID()).
					Return(domain.UptimeReport{}, notFound)
			},
			wantStatus: http.StatusNotFound,
			wantCode:   plinth.CodeNotFound,
		},
		"checks failure": {
			target: checksPath(),
			expect: func(svc *MockHistoryService) {
				svc.EXPECT().Checks(gomock.Any(), fixedID(), gomock.Any(), gomock.Any()).
					Return(service.CheckPage{}, failure)
			},
			wantStatus: http.StatusInternalServerError,
			wantCode:   plinth.CodeInternal,
		},
		"uptime failure": {
			target: uptimePath(),
			expect: func(svc *MockHistoryService) {
				svc.EXPECT().Uptime(gomock.Any(), fixedID()).
					Return(domain.UptimeReport{}, failure)
			},
			wantStatus: http.StatusInternalServerError,
			wantCode:   plinth.CodeInternal,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			h, svc := newHistoryRouter(t)
			test.expect(svc)

			rec := serveJSON(t, h, http.MethodGet, test.target, nil)

			require.Equal(t, test.wantStatus, rec.Code)
			assert.Equal(t, test.wantCode, plinthtest.DecodeError(t, rec).Code)
			assert.NotContains(t, rec.Body.String(), failure.Error())
		})
	}
}

func timeEqual(want time.Time) gomock.Matcher {
	return gomock.Cond(func(got time.Time) bool {
		return got.Equal(want)
	})
}
