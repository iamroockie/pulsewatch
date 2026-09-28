package rest_test

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"uuid"

	"github.com/iamroockie/plinth"
	"github.com/iamroockie/plinth/plinthtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/iamroockie/pulsewatch/internal/domain"
	"github.com/iamroockie/pulsewatch/internal/service"
	"github.com/iamroockie/pulsewatch/internal/transport/rest"
)

const storedMonitorJSON = `{
	"id": "01998a2e-6f00-7000-8000-000000000001",
	"url": "https://example.com/health",
	"interval_seconds": 60,
	"timeout_ms": 5000,
	"max_retries": 2,
	"is_active": true,
	"next_check_at": "2026-09-26T14:00:00.123456Z",
	"last_check_at": null,
	"created_at": "2026-09-26T12:00:00.123456Z",
	"updated_at": "2026-09-26T13:00:00.123456Z"
}`

func fixedNow() time.Time {
	return time.Date(2026, 9, 26, 12, 0, 0, 123456000, time.UTC)
}

func fixedID() uuid.UUID {
	return uuid.MustParse("01998a2e-6f00-7000-8000-000000000001")
}

func monitorPath() string {
	return "/monitors/" + fixedID().String()
}

func storedMonitor() *domain.Monitor {
	return &domain.Monitor{
		ID:       fixedID(),
		IsActive: true,
		Settings: domain.CheckSettings{
			URL:        "https://example.com/health",
			Interval:   time.Minute,
			Timeout:    5 * time.Second,
			MaxRetries: 2,
		},
		NextCheckAt: fixedNow().Add(2 * time.Hour),
		LastCheckAt: nil,
		CreatedAt:   fixedNow(),
		UpdatedAt:   fixedNow().Add(time.Hour),
	}
}

func newMonitorRouter(t *testing.T) (http.Handler, *MockMonitorService) {
	t.Helper()

	ctrl := gomock.NewController(t)
	svc := NewMockMonitorService(ctrl)
	h := rest.NewRouter(slog.New(slog.DiscardHandler), nil, svc, NewMockHistoryService(ctrl))

	return h, svc
}

func serveJSON(
	t *testing.T, h http.Handler, method, target string, body any,
) *httptest.ResponseRecorder {
	t.Helper()

	rec, _ := plinthtest.Serve(t, h, plinthtest.NewRequest(t, method, target, body))

	return rec
}

func TestCreateMonitor(t *testing.T) {
	h, svc := newMonitorRouter(t)
	stored := storedMonitor()
	body := plinth.Map{
		"url":              "https://example.com/health",
		"interval_seconds": 60,
		"timeout_ms":       5000,
		"max_retries":      2,
	}
	svc.EXPECT().Create(gomock.Any(), stored.Settings).Return(stored, nil)

	rec := serveJSON(t, h, http.MethodPost, "/monitors", body)

	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, monitorPath(), rec.Header().Get("Location"))
	assert.JSONEq(t, storedMonitorJSON, rec.Body.String())
}

func TestCreateMonitorRejectsInvalidSettings(t *testing.T) {
	tests := map[string]struct {
		err  error
		want []plinth.FieldViolation
	}{
		"invalid url": {
			err:  domain.ErrInvalidURL,
			want: []plinth.FieldViolation{{Field: "url", Code: "invalid"}},
		},
		"unsupported scheme": {
			err:  domain.ErrUnsupportedSchemeURL,
			want: []plinth.FieldViolation{{Field: "url", Code: "unsupported_scheme"}},
		},
		"credentials in url": {
			err:  domain.ErrCredentialsInURL,
			want: []plinth.FieldViolation{{Field: "url", Code: "credentials_not_allowed"}},
		},
		"interval out of range": {
			err: domain.ErrIntervalOutOfRange,
			want: []plinth.FieldViolation{{
				Field:  "interval_seconds",
				Code:   "out_of_range",
				Params: plinth.Map{"min": 10.0, "max": 86400.0},
			}},
		},
		"timeout out of range": {
			err: domain.ErrTimeoutOutOfRange,
			want: []plinth.FieldViolation{{
				Field:  "timeout_ms",
				Code:   "out_of_range",
				Params: plinth.Map{"min": 100.0, "max": 60000.0},
			}},
		},
		"timeout exceeds interval": {
			err:  domain.ErrTimeoutExceedsInterval,
			want: []plinth.FieldViolation{{Field: "timeout_ms", Code: "exceeds_interval"}},
		},
		"max retries out of range": {
			err: domain.ErrMaxRetriesOutOfRange,
			want: []plinth.FieldViolation{{
				Field:  "max_retries",
				Code:   "out_of_range",
				Params: plinth.Map{"min": 0.0, "max": 5.0},
			}},
		},
		"several errors": {
			err: errors.Join(domain.ErrMaxRetriesOutOfRange, domain.ErrInvalidURL),
			want: []plinth.FieldViolation{
				{Field: "url", Code: "invalid"},
				{
					Field:  "max_retries",
					Code:   "out_of_range",
					Params: plinth.Map{"min": 0.0, "max": 5.0},
				},
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			h, svc := newMonitorRouter(t)
			err := fmt.Errorf("create monitor: %w", test.err)
			svc.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil, err)

			rec := serveJSON(t, h, http.MethodPost, "/monitors", plinth.Map{})

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			got := plinthtest.DecodeError(t, rec)
			assert.Equal(t, plinth.CodeValidation, got.Code)
			assert.Equal(t, test.want, got.Details)
		})
	}
}

func TestMonitorsRejectMalformedBody(t *testing.T) {
	tests := map[string]struct {
		method string
		target string
		body   string
	}{
		"create with unknown field": {
			method: http.MethodPost,
			target: "/monitors",
			body:   `{"url": "https://example.com", "intreval_seconds": 60}`,
		},
		"update with unknown field": {
			method: http.MethodPatch,
			target: monitorPath(),
			body:   `{"is_actve": false}`,
		},
		"create with overflowing interval": {
			method: http.MethodPost,
			target: "/monitors",
			body:   `{"interval_seconds": 2147483648}`,
		},
		"update with fractional retries": {
			method: http.MethodPatch,
			target: monitorPath(),
			body:   `{"max_retries": 1.5}`,
		},
		"update with duplicate field": {
			method: http.MethodPatch,
			target: monitorPath(),
			body:   `{"is_active": true, "is_active": false}`,
		},
		"create with broken json": {
			method: http.MethodPost,
			target: "/monitors",
			body:   `{"url":`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			h, _ := newMonitorRouter(t)

			rec := serveJSON(t, h, test.method, test.target, test.body)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, plinth.CodeBadRequest, plinthtest.DecodeError(t, rec).Code)
		})
	}
}

func TestMonitorsRejectInvalidID(t *testing.T) {
	tests := map[string]struct {
		method string
	}{
		"get":    {method: http.MethodGet},
		"update": {method: http.MethodPatch},
		"delete": {method: http.MethodDelete},
	}
	want := plinthtest.Error{Code: plinth.CodeBadRequest, Message: `invalid path parameter "id"`}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			h, _ := newMonitorRouter(t)

			rec := serveJSON(t, h, test.method, "/monitors/not-a-uuid", nil)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, want, plinthtest.DecodeError(t, rec))
		})
	}
}

func TestMonitorsTranslateServiceErrors(t *testing.T) {
	notFound := fmt.Errorf("monitor: %w", domain.ErrMonitorNotFound)
	invalid := fmt.Errorf("update monitor: %w", domain.ErrTimeoutExceedsInterval)
	failure := errors.New("connection refused")
	tests := map[string]struct {
		method     string
		target     string
		expect     func(svc *MockMonitorService)
		wantStatus int
		wantCode   plinth.ErrorCode
	}{
		"get not found": {
			method: http.MethodGet,
			target: monitorPath(),
			expect: func(svc *MockMonitorService) {
				svc.EXPECT().Get(gomock.Any(), fixedID()).Return(nil, notFound)
			},
			wantStatus: http.StatusNotFound,
			wantCode:   plinth.CodeNotFound,
		},
		"update not found": {
			method: http.MethodPatch,
			target: monitorPath(),
			expect: func(svc *MockMonitorService) {
				svc.EXPECT().Update(gomock.Any(), fixedID(), gomock.Any()).Return(nil, notFound)
			},
			wantStatus: http.StatusNotFound,
			wantCode:   plinth.CodeNotFound,
		},
		"delete not found": {
			method: http.MethodDelete,
			target: monitorPath(),
			expect: func(svc *MockMonitorService) {
				svc.EXPECT().Delete(gomock.Any(), fixedID()).Return(notFound)
			},
			wantStatus: http.StatusNotFound,
			wantCode:   plinth.CodeNotFound,
		},
		"update invalid settings": {
			method: http.MethodPatch,
			target: monitorPath(),
			expect: func(svc *MockMonitorService) {
				svc.EXPECT().Update(gomock.Any(), fixedID(), gomock.Any()).Return(nil, invalid)
			},
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   plinth.CodeValidation,
		},
		"create failure": {
			method: http.MethodPost,
			target: "/monitors",
			expect: func(svc *MockMonitorService) {
				svc.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil, failure)
			},
			wantStatus: http.StatusInternalServerError,
			wantCode:   plinth.CodeInternal,
		},
		"list failure": {
			method: http.MethodGet,
			target: "/monitors",
			expect: func(svc *MockMonitorService) {
				svc.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(service.MonitorPage{}, failure)
			},
			wantStatus: http.StatusInternalServerError,
			wantCode:   plinth.CodeInternal,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			h, svc := newMonitorRouter(t)
			test.expect(svc)

			rec := serveJSON(t, h, test.method, test.target, plinth.Map{})

			require.Equal(t, test.wantStatus, rec.Code)
			assert.Equal(t, test.wantCode, plinthtest.DecodeError(t, rec).Code)
			assert.NotContains(t, rec.Body.String(), failure.Error())
		})
	}
}

func TestGetMonitor(t *testing.T) {
	h, svc := newMonitorRouter(t)
	svc.EXPECT().Get(gomock.Any(), fixedID()).Return(storedMonitor(), nil)

	rec := serveJSON(t, h, http.MethodGet, monitorPath(), nil)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, storedMonitorJSON, rec.Body.String())
}

func TestListMonitors(t *testing.T) {
	after := fixedID()
	tests := map[string]struct {
		query     string
		wantAfter uuid.UUID
		wantLimit int
	}{
		"defaults":  {query: "", wantAfter: uuid.Nil(), wantLimit: 50},
		"min limit": {query: "?limit=1", wantAfter: uuid.Nil(), wantLimit: 1},
		"max limit": {query: "?limit=100", wantAfter: uuid.Nil(), wantLimit: 100},
		"after":     {query: "?after=" + after.String(), wantAfter: after, wantLimit: 50},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			h, svc := newMonitorRouter(t)
			svc.EXPECT().List(gomock.Any(), test.wantAfter, test.wantLimit).
				Return(service.MonitorPage{}, nil)

			rec := serveJSON(t, h, http.MethodGet, "/monitors"+test.query, nil)

			assert.Equal(t, http.StatusOK, rec.Code)
		})
	}
}

func TestListMonitorsResponse(t *testing.T) {
	stored := storedMonitor()
	tests := map[string]struct {
		page service.MonitorPage
		want string
	}{
		"empty": {
			page: service.MonitorPage{Items: nil, NextAfter: nil},
			want: `{"items": [], "next_after": null}`,
		},
		"last page": {
			page: service.MonitorPage{Items: []*domain.Monitor{stored}, NextAfter: nil},
			want: fmt.Sprintf(`{"items": [%s], "next_after": null}`, storedMonitorJSON),
		},
		"has next page": {
			page: service.MonitorPage{Items: []*domain.Monitor{stored}, NextAfter: &stored.ID},
			want: fmt.Sprintf(`{"items": [%s], "next_after": %q}`, storedMonitorJSON, stored.ID),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			h, svc := newMonitorRouter(t)
			svc.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).Return(test.page, nil)

			rec := serveJSON(t, h, http.MethodGet, "/monitors", nil)

			require.Equal(t, http.StatusOK, rec.Code)
			assert.JSONEq(t, test.want, rec.Body.String())
		})
	}
}

func TestListMonitorsRejectsMalformedQuery(t *testing.T) {
	tests := map[string]struct {
		query string
		want  string
	}{
		"limit not a number": {query: "?limit=ten", want: `invalid query parameter "limit"`},
		"after not a uuid":   {query: "?after=42", want: `invalid query parameter "after"`},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			h, _ := newMonitorRouter(t)

			rec := serveJSON(t, h, http.MethodGet, "/monitors"+test.query, nil)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			want := plinthtest.Error{Code: plinth.CodeBadRequest, Message: test.want}
			assert.Equal(t, want, plinthtest.DecodeError(t, rec))
		})
	}
}

func TestListMonitorsRejectsLimitOutOfRange(t *testing.T) {
	tests := map[string]struct {
		query string
	}{
		"zero":      {query: "?limit=0"},
		"negative":  {query: "?limit=-1"},
		"above max": {query: "?limit=101"},
	}
	want := []plinth.FieldViolation{{
		Field:  "limit",
		Code:   "out_of_range",
		Params: plinth.Map{"min": 1.0, "max": 100.0},
	}}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			h, _ := newMonitorRouter(t)

			rec := serveJSON(t, h, http.MethodGet, "/monitors"+test.query, nil)

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			got := plinthtest.DecodeError(t, rec)
			assert.Equal(t, plinth.CodeValidation, got.Code)
			assert.Equal(t, want, got.Details)
		})
	}
}

func TestUpdateMonitor(t *testing.T) {
	tests := map[string]struct {
		body string
		want domain.MonitorChanges
	}{
		"all fields": {
			body: `{"url": "https://example.org/ping", "interval_seconds": 120,
				"timeout_ms": 1500, "max_retries": 4, "is_active": false}`,
			want: domain.MonitorChanges{
				URL:        new("https://example.org/ping"),
				Interval:   new(2 * time.Minute),
				Timeout:    new(1500 * time.Millisecond),
				MaxRetries: new(int32(4)),
				IsActive:   new(false),
			},
		},
		"one field": {
			body: `{"is_active": true}`,
			want: domain.MonitorChanges{IsActive: new(true)},
		},
		"null field": {
			body: `{"url": null}`,
			want: domain.MonitorChanges{},
		},
		"no fields": {
			body: `{}`,
			want: domain.MonitorChanges{},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			h, svc := newMonitorRouter(t)
			svc.EXPECT().Update(gomock.Any(), fixedID(), test.want).Return(storedMonitor(), nil)

			rec := serveJSON(t, h, http.MethodPatch, monitorPath(), test.body)

			require.Equal(t, http.StatusOK, rec.Code)
			assert.JSONEq(t, storedMonitorJSON, rec.Body.String())
		})
	}
}

func TestDeleteMonitor(t *testing.T) {
	h, svc := newMonitorRouter(t)
	svc.EXPECT().Delete(gomock.Any(), fixedID()).Return(nil)

	rec := serveJSON(t, h, http.MethodDelete, monitorPath(), nil)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
}
