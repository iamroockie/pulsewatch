package rest

import (
	"time"
	"uuid"

	"github.com/iamroockie/pulsewatch/internal/domain"
	"github.com/iamroockie/pulsewatch/internal/service"
)

type createMonitorRequest struct {
	URL             string `json:"url"`
	IntervalSeconds int32  `json:"interval_seconds"`
	TimeoutMS       int32  `json:"timeout_ms"`
	MaxRetries      int32  `json:"max_retries"`
}

func (req createMonitorRequest) toDomain() domain.CheckSettings {
	return domain.CheckSettings{
		URL:        req.URL,
		Interval:   time.Duration(req.IntervalSeconds) * time.Second,
		Timeout:    time.Duration(req.TimeoutMS) * time.Millisecond,
		MaxRetries: req.MaxRetries,
	}
}

type updateMonitorRequest struct {
	URL             *string `json:"url"`
	IntervalSeconds *int32  `json:"interval_seconds"`
	TimeoutMS       *int32  `json:"timeout_ms"`
	MaxRetries      *int32  `json:"max_retries"`
	IsActive        *bool   `json:"is_active"`
}

func (req updateMonitorRequest) toDomain() domain.MonitorChanges {
	return domain.MonitorChanges{
		URL:        req.URL,
		Interval:   durationOf(req.IntervalSeconds, time.Second),
		Timeout:    durationOf(req.TimeoutMS, time.Millisecond),
		MaxRetries: req.MaxRetries,
		IsActive:   req.IsActive,
	}
}

func durationOf(n *int32, unit time.Duration) *time.Duration {
	if n == nil {
		return nil
	}

	return new(time.Duration(*n) * unit)
}

type monitorResponse struct {
	ID              uuid.UUID  `json:"id"`
	URL             string     `json:"url"`
	IntervalSeconds int64      `json:"interval_seconds"`
	TimeoutMS       int64      `json:"timeout_ms"`
	MaxRetries      int32      `json:"max_retries"`
	IsActive        bool       `json:"is_active"`
	NextCheckAt     time.Time  `json:"next_check_at"`
	LastCheckAt     *time.Time `json:"last_check_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func monitorFromDomain(m *domain.Monitor) monitorResponse {
	return monitorResponse{
		ID:              m.ID,
		URL:             m.Settings.URL,
		IntervalSeconds: seconds(m.Settings.Interval),
		TimeoutMS:       m.Settings.Timeout.Milliseconds(),
		MaxRetries:      m.Settings.MaxRetries,
		IsActive:        m.IsActive,
		NextCheckAt:     m.NextCheckAt,
		LastCheckAt:     m.LastCheckAt,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}
}

type monitorPageResponse struct {
	Items     []monitorResponse `json:"items"`
	NextAfter *uuid.UUID        `json:"next_after"`
}

func pageFromDomain(p service.MonitorPage) monitorPageResponse {
	items := make([]monitorResponse, 0, len(p.Items))
	for _, m := range p.Items {
		items = append(items, monitorFromDomain(m))
	}

	return monitorPageResponse{Items: items, NextAfter: p.NextAfter}
}

func seconds(d time.Duration) int64 {
	return int64(d / time.Second)
}

type checkResponse struct {
	CheckedAt  time.Time `json:"checked_at"`
	IsUp       bool      `json:"is_up"`
	StatusCode *int      `json:"status_code"`
	LatencyMS  int64     `json:"latency_ms"`
	Attempts   int32     `json:"attempts"`
	Error      *string   `json:"error"`
}

func checkFromDomain(c *domain.Check) checkResponse {
	return checkResponse{
		CheckedAt:  c.CheckedAt,
		IsUp:       c.Result.IsUp,
		StatusCode: nullIfZero(c.Result.StatusCode),
		LatencyMS:  c.Result.Latency.Milliseconds(),
		Attempts:   c.Result.Attempts,
		Error:      nullIfZero(c.Result.Error),
	}
}

type checkPageResponse struct {
	Items      []checkResponse `json:"items"`
	NextBefore *time.Time      `json:"next_before"`
}

func checkPageFromDomain(p service.CheckPage) checkPageResponse {
	items := make([]checkResponse, 0, len(p.Items))
	for _, c := range p.Items {
		items = append(items, checkFromDomain(c))
	}

	return checkPageResponse{Items: items, NextBefore: p.NextBefore}
}

type uptimeResponse struct {
	Checks int64    `json:"checks"`
	Up     int64    `json:"up"`
	Ratio  *float64 `json:"ratio"`
}

func uptimeFromDomain(u domain.Uptime) uptimeResponse {
	resp := uptimeResponse{Checks: u.Checks, Up: u.Up, Ratio: nil}
	if ratio, ok := u.Ratio(); ok {
		resp.Ratio = &ratio
	}

	return resp
}

type uptimeReportResponse struct {
	Hour uptimeResponse `json:"1h"`
	Day  uptimeResponse `json:"24h"`
	Week uptimeResponse `json:"7d"`
}

func uptimeReportFromDomain(r domain.UptimeReport) uptimeReportResponse {
	return uptimeReportResponse{
		Hour: uptimeFromDomain(r.Hour),
		Day:  uptimeFromDomain(r.Day),
		Week: uptimeFromDomain(r.Week),
	}
}

func nullIfZero[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}

	return &v
}
