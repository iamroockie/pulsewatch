package domain

import (
	"errors"
	"net/url"
	"strconv"
	"time"
	"uuid"
)

const (
	MinInterval = 10 * time.Second
	MaxInterval = 86400 * time.Second
	MinTimeout  = 100 * time.Millisecond
	MaxTimeout  = 60000 * time.Millisecond
	MinRetries  = 0
	MaxRetries  = 5
)

type Monitor struct {
	ID          uuid.UUID
	IsActive    bool
	Settings    CheckSettings
	NextCheckAt time.Time
	LastCheckAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CheckSettings struct {
	URL        string
	Interval   time.Duration
	Timeout    time.Duration
	MaxRetries int32
}

type MonitorChanges struct {
	URL        *string
	Interval   *time.Duration
	Timeout    *time.Duration
	MaxRetries *int32
	IsActive   *bool
}

func NewMonitor(id uuid.UUID, s CheckSettings, now time.Time) (*Monitor, error) {
	s, err := s.normalize()
	if err != nil {
		return nil, err
	}

	monitor := &Monitor{
		ID:          id,
		IsActive:    true,
		Settings:    s,
		NextCheckAt: now,
		LastCheckAt: nil,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	return monitor, nil
}

func (m *Monitor) Apply(c MonitorChanges, now time.Time) error {
	s, err := c.applyTo(m.Settings).normalize()
	if err != nil {
		return err
	}

	reactivated := c.IsActive != nil && *c.IsActive && !m.IsActive
	if s.URL != m.Settings.URL || s.Interval != m.Settings.Interval || reactivated {
		m.NextCheckAt = now
	}

	m.Settings = s
	if c.IsActive != nil {
		m.IsActive = *c.IsActive
	}
	m.UpdatedAt = now

	return nil
}

func (c MonitorChanges) applyTo(s CheckSettings) CheckSettings {
	if c.URL != nil {
		s.URL = *c.URL
	}
	if c.Interval != nil {
		s.Interval = *c.Interval
	}
	if c.Timeout != nil {
		s.Timeout = *c.Timeout
	}
	if c.MaxRetries != nil {
		s.MaxRetries = *c.MaxRetries
	}

	return s
}

func (s CheckSettings) normalize() (CheckSettings, error) {
	normalizedURL, urlErr := normalizeURL(s.URL)
	intervalErr := validateInterval(s.Interval)
	timeoutErr := validateTimeout(s.Timeout)

	errs := []error{urlErr, intervalErr, timeoutErr}

	if intervalErr == nil && timeoutErr == nil && s.Timeout >= s.Interval {
		errs = append(errs, ErrTimeoutExceedsInterval)
	}

	if s.MaxRetries < MinRetries || s.MaxRetries > MaxRetries {
		errs = append(errs, ErrMaxRetriesOutOfRange)
	}

	if err := errors.Join(errs...); err != nil {
		return CheckSettings{}, err
	}

	s.URL = normalizedURL

	return s, nil
}

func normalizeURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	switch {
	case err != nil, u.Scheme == "", u.Hostname() == "", !isValidPort(u.Port()):
		return "", ErrInvalidURL
	case u.Scheme != "http" && u.Scheme != "https":
		return "", ErrUnsupportedSchemeURL
	case u.User != nil:
		return "", ErrCredentialsInURL
	}

	return u.String(), nil
}

func isValidPort(port string) bool {
	if port == "" {
		return true
	}

	n, err := strconv.ParseUint(port, 10, 16)

	return err == nil && n > 0
}

func validateInterval(d time.Duration) error {
	switch {
	case d < MinInterval || d > MaxInterval:
		return ErrIntervalOutOfRange
	case d%time.Second != 0:
		return ErrIntervalPrecision
	}

	return nil
}

func validateTimeout(d time.Duration) error {
	switch {
	case d < MinTimeout || d > MaxTimeout:
		return ErrTimeoutOutOfRange
	case d%time.Millisecond != 0:
		return ErrTimeoutPrecision
	}

	return nil
}
