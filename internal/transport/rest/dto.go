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
