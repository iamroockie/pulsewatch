package domain_test

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/iamroockie/pulsewatch/internal/domain"
	"github.com/iamroockie/pulsewatch/internal/domain/domaintest"
)

func fixedNow() time.Time {
	return time.Date(2026, 9, 25, 12, 30, 0, 123456000, time.UTC)
}

func joinedErrors(t *testing.T, err error) []error {
	t.Helper()

	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) {
		t.Fatalf("error %v is not a joined error", err)
	}

	return joined.Unwrap()
}

func TestNewMonitor(t *testing.T) {
	id := uuid.NewV7()
	now := fixedNow()
	settings := domaintest.ValidSettings()
	want := &domain.Monitor{
		ID:          id,
		IsActive:    true,
		Settings:    settings,
		NextCheckAt: now,
		LastCheckAt: nil,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	got, err := domain.NewMonitor(id, settings, now)

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestNewMonitorNormalizesURL(t *testing.T) {
	s := domaintest.ValidSettings()
	s.URL = "HTTPS://Example.com/health"

	got, err := domain.NewMonitor(uuid.NewV7(), s, fixedNow())

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "https://Example.com/health", got.Settings.URL)
}

func TestNewMonitorAcceptsBoundaries(t *testing.T) {
	tests := map[string]struct {
		modify func(*domain.CheckSettings)
	}{
		"min interval": {
			modify: func(s *domain.CheckSettings) {
				s.Interval, s.Timeout = domain.MinInterval, domain.MinTimeout
			},
		},
		"max interval": {
			modify: func(s *domain.CheckSettings) {
				s.Interval = domain.MaxInterval
			},
		},
		"max timeout": {
			modify: func(s *domain.CheckSettings) {
				s.Interval, s.Timeout = domain.MaxInterval, domain.MaxTimeout
			},
		},
		"timeout just below interval": {
			modify: func(s *domain.CheckSettings) {
				s.Interval, s.Timeout = 10*time.Second, 9999*time.Millisecond
			},
		},
		"min retries": {
			modify: func(s *domain.CheckSettings) {
				s.MaxRetries = domain.MinRetries
			},
		},
		"max retries": {
			modify: func(s *domain.CheckSettings) {
				s.MaxRetries = domain.MaxRetries
			},
		},
		"http scheme": {
			modify: func(s *domain.CheckSettings) {
				s.URL = "http://example.com"
			},
		},
		"explicit port": {
			modify: func(s *domain.CheckSettings) {
				s.URL = "https://example.com:65535"
			},
		},
		"ipv6 host": {
			modify: func(s *domain.CheckSettings) {
				s.URL = "http://[::1]:8080/health"
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			s := domaintest.ValidSettings()
			test.modify(&s)

			got, err := domain.NewMonitor(uuid.NewV7(), s, fixedNow())

			require.NoError(t, err)
			assert.NotNil(t, got)
		})
	}
}

func TestNewMonitorRejects(t *testing.T) {
	tests := map[string]struct {
		modify func(*domain.CheckSettings)
		want   []error
	}{
		"unparsable url": {
			modify: func(s *domain.CheckSettings) {
				s.URL = "http://[::1"
			},
			want: []error{domain.ErrInvalidURL},
		},
		"url without scheme": {
			modify: func(s *domain.CheckSettings) {
				s.URL = "example.com"
			},
			want: []error{domain.ErrInvalidURL},
		},
		"url without host": {
			modify: func(s *domain.CheckSettings) {
				s.URL = "https://"
			},
			want: []error{domain.ErrInvalidURL},
		},
		"port out of range": {
			modify: func(s *domain.CheckSettings) {
				s.URL = "https://example.com:99999"
			},
			want: []error{domain.ErrInvalidURL},
		},
		"zero port": {
			modify: func(s *domain.CheckSettings) {
				s.URL = "https://example.com:0"
			},
			want: []error{domain.ErrInvalidURL},
		},
		"unsupported scheme": {
			modify: func(s *domain.CheckSettings) {
				s.URL = "ftp://example.com"
			},
			want: []error{domain.ErrUnsupportedSchemeURL},
		},
		"credentials in url": {
			modify: func(s *domain.CheckSettings) {
				s.URL = "https://user:secret@example.com"
			},
			want: []error{domain.ErrCredentialsInURL},
		},
		"interval below min": {
			modify: func(s *domain.CheckSettings) {
				s.Interval, s.Timeout = 9*time.Second, time.Second
			},
			want: []error{domain.ErrIntervalOutOfRange},
		},
		"interval above max": {
			modify: func(s *domain.CheckSettings) {
				s.Interval = domain.MaxInterval + time.Second
			},
			want: []error{domain.ErrIntervalOutOfRange},
		},
		"interval not whole seconds": {
			modify: func(s *domain.CheckSettings) {
				s.Interval = 10500 * time.Millisecond
			},
			want: []error{domain.ErrIntervalPrecision},
		},
		"timeout below min": {
			modify: func(s *domain.CheckSettings) {
				s.Timeout = 99 * time.Millisecond
			},
			want: []error{domain.ErrTimeoutOutOfRange},
		},
		"timeout above max": {
			modify: func(s *domain.CheckSettings) {
				s.Interval, s.Timeout = domain.MaxInterval, domain.MaxTimeout+time.Millisecond
			},
			want: []error{domain.ErrTimeoutOutOfRange},
		},
		"timeout not whole milliseconds": {
			modify: func(s *domain.CheckSettings) {
				s.Timeout = 1500500 * time.Microsecond
			},
			want: []error{domain.ErrTimeoutPrecision},
		},
		"timeout equals interval": {
			modify: func(s *domain.CheckSettings) {
				s.Interval, s.Timeout = 10*time.Second, 10*time.Second
			},
			want: []error{domain.ErrTimeoutExceedsInterval},
		},
		"timeout above interval": {
			modify: func(s *domain.CheckSettings) {
				s.Interval, s.Timeout = 10*time.Second, 15*time.Second
			},
			want: []error{domain.ErrTimeoutExceedsInterval},
		},
		"invalid timeout is not compared with interval": {
			modify: func(s *domain.CheckSettings) {
				s.Interval, s.Timeout = 10*time.Second, domain.MaxTimeout+time.Second
			},
			want: []error{domain.ErrTimeoutOutOfRange},
		},
		"invalid interval is not compared with timeout": {
			modify: func(s *domain.CheckSettings) {
				s.Interval, s.Timeout = 5*time.Second, 6*time.Second
			},
			want: []error{domain.ErrIntervalOutOfRange},
		},
		"negative retries": {
			modify: func(s *domain.CheckSettings) {
				s.MaxRetries = domain.MinRetries - 1
			},
			want: []error{domain.ErrMaxRetriesOutOfRange},
		},
		"too many retries": {
			modify: func(s *domain.CheckSettings) {
				s.MaxRetries = domain.MaxRetries + 1
			},
			want: []error{domain.ErrMaxRetriesOutOfRange},
		},
		"all fields at once": {
			modify: func(s *domain.CheckSettings) {
				*s = domain.CheckSettings{
					URL:        "ftp://example.com",
					Interval:   5 * time.Second,
					Timeout:    99 * time.Millisecond,
					MaxRetries: -1,
				}
			},
			want: []error{
				domain.ErrUnsupportedSchemeURL,
				domain.ErrIntervalOutOfRange,
				domain.ErrTimeoutOutOfRange,
				domain.ErrMaxRetriesOutOfRange,
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			s := domaintest.ValidSettings()
			test.modify(&s)

			got, err := domain.NewMonitor(uuid.NewV7(), s, fixedNow())

			require.Error(t, err)
			assert.Nil(t, got)
			assert.ElementsMatch(t, test.want, joinedErrors(t, err))
		})
	}
}

func TestMonitorApply(t *testing.T) {
	now := fixedNow().Add(time.Hour)
	tests := map[string]struct {
		changes domain.MonitorChanges
		want    func(*domain.Monitor)
	}{
		"max retries": {
			changes: domain.MonitorChanges{MaxRetries: new(int32(4))},
			want: func(m *domain.Monitor) {
				m.Settings.MaxRetries = 4
			},
		},
		"timeout": {
			changes: domain.MonitorChanges{Timeout: new(30 * time.Second)},
			want: func(m *domain.Monitor) {
				m.Settings.Timeout = 30 * time.Second
			},
		},
		"url is normalized": {
			changes: domain.MonitorChanges{URL: new("HTTPS://Example.org/ping")},
			want: func(m *domain.Monitor) {
				m.Settings.URL = "https://Example.org/ping"
				m.NextCheckAt = now
			},
		},
		"pause": {
			changes: domain.MonitorChanges{IsActive: new(false)},
			want: func(m *domain.Monitor) {
				m.IsActive = false
			},
		},
		"timeout valid only with new interval": {
			changes: domain.MonitorChanges{
				Interval: new(2 * time.Minute),
				Timeout:  new(time.Minute),
			},
			want: func(m *domain.Monitor) {
				m.Settings.Interval = 2 * time.Minute
				m.Settings.Timeout = time.Minute
				m.NextCheckAt = now
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got := domaintest.NewMonitor(t, fixedNow())
			want := *got
			want.UpdatedAt = now
			test.want(&want)

			err := got.Apply(test.changes, now)

			require.NoError(t, err)
			assert.Equal(t, &want, got)
		})
	}
}

func TestMonitorApplyWithoutChanges(t *testing.T) {
	now := fixedNow().Add(time.Hour)
	got := domaintest.NewMonitor(t, fixedNow())
	want := *got
	want.UpdatedAt = now

	err := got.Apply(domain.MonitorChanges{}, now)

	require.NoError(t, err)
	assert.Equal(t, &want, got)
}

func TestMonitorApplySchedulesNextCheck(t *testing.T) {
	created := fixedNow()
	now := created.Add(time.Hour)
	tests := map[string]struct {
		changes domain.MonitorChanges
		want    time.Time
	}{
		"new url": {
			changes: domain.MonitorChanges{URL: new("https://example.org")},
			want:    now,
		},
		"same url in another case": {
			changes: domain.MonitorChanges{URL: new("HTTPS://example.com/health")},
			want:    created,
		},
		"new interval": {
			changes: domain.MonitorChanges{Interval: new(2 * time.Minute)},
			want:    now,
		},
		"same interval": {
			changes: domain.MonitorChanges{Interval: new(time.Minute)},
			want:    created,
		},
		"new timeout": {
			changes: domain.MonitorChanges{Timeout: new(10 * time.Second)},
			want:    created,
		},
		"new max retries": {
			changes: domain.MonitorChanges{MaxRetries: new(int32(0))},
			want:    created,
		},
		"pause": {
			changes: domain.MonitorChanges{IsActive: new(false)},
			want:    created,
		},
		"activation of active monitor": {
			changes: domain.MonitorChanges{IsActive: new(true)},
			want:    created,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			m := domaintest.NewMonitor(t, fixedNow())

			err := m.Apply(test.changes, now)

			require.NoError(t, err)
			assert.Equal(t, test.want, m.NextCheckAt)
		})
	}
}

func TestMonitorApplyReactivates(t *testing.T) {
	now := fixedNow().Add(time.Hour)
	got := domaintest.NewMonitor(t, fixedNow())
	got.IsActive = false
	want := *got
	want.IsActive = true
	want.NextCheckAt = now
	want.UpdatedAt = now

	err := got.Apply(domain.MonitorChanges{IsActive: new(true)}, now)

	require.NoError(t, err)
	assert.Equal(t, &want, got)
}

func TestMonitorApplyRejects(t *testing.T) {
	tests := map[string]struct {
		changes domain.MonitorChanges
		want    []error
	}{
		"timeout reaches current interval": {
			changes: domain.MonitorChanges{Timeout: new(time.Minute)},
			want:    []error{domain.ErrTimeoutExceedsInterval},
		},
		"unsupported scheme": {
			changes: domain.MonitorChanges{URL: new("ftp://example.com")},
			want:    []error{domain.ErrUnsupportedSchemeURL},
		},
		"too many retries": {
			changes: domain.MonitorChanges{MaxRetries: new(int32(domain.MaxRetries + 1))},
			want:    []error{domain.ErrMaxRetriesOutOfRange},
		},
		"valid change is not applied with invalid one": {
			changes: domain.MonitorChanges{
				MaxRetries: new(int32(4)),
				Timeout:    new(time.Minute),
			},
			want: []error{domain.ErrTimeoutExceedsInterval},
		},
		"several fields at once": {
			changes: domain.MonitorChanges{
				URL:        new("example.com"),
				Interval:   new(5 * time.Second),
				MaxRetries: new(int32(-1)),
			},
			want: []error{
				domain.ErrInvalidURL,
				domain.ErrIntervalOutOfRange,
				domain.ErrMaxRetriesOutOfRange,
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got := domaintest.NewMonitor(t, fixedNow())
			want := *got

			err := got.Apply(test.changes, fixedNow().Add(time.Hour))

			require.Error(t, err)
			assert.ElementsMatch(t, test.want, joinedErrors(t, err))
			assert.Equal(t, &want, got)
		})
	}
}
