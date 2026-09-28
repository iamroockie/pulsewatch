package redis_test

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/iamroockie/pulsewatch/internal/adapter/redis"
	"github.com/iamroockie/pulsewatch/internal/domain"
	"github.com/iamroockie/pulsewatch/internal/domain/domaintest"
)

func newHost() string {
	return uuid.NewV4().String() + ".test"
}

func newClaim(t *testing.T, rawURL string, until time.Time) domain.Claim {
	t.Helper()

	m := domaintest.NewMonitor(t, time.Now())
	m.Settings.URL = rawURL

	return domain.Claim{Monitor: m, Until: until}
}

func acquire(t *testing.T, l *redis.HostLimiter, c domain.Claim, now time.Time) bool {
	t.Helper()

	acquired, err := l.Acquire(t.Context(), c, now)
	require.NoError(t, err)

	return acquired
}

func TestHostLimiterAcquireRejectsBusyHost(t *testing.T) {
	limiter := redis.NewHostLimiter(newClient(t), 2)
	now := time.Now()
	until := now.Add(time.Minute)
	rawURL := "https://" + newHost() + "/health"
	require.True(t, acquire(t, limiter, newClaim(t, rawURL, until), now))
	require.True(t, acquire(t, limiter, newClaim(t, rawURL, until), now))

	got, err := limiter.Acquire(t.Context(), newClaim(t, rawURL, until), now)

	require.NoError(t, err)
	assert.False(t, got)
}

func TestHostLimiterGroupsClaimsByHostname(t *testing.T) {
	tests := map[string]struct {
		url  func(host string) string
		want bool
	}{
		"other path": {
			url:  func(host string) string { return "https://" + host + "/other" },
			want: false,
		},
		"other port": {
			url:  func(host string) string { return "https://" + host + ":8443/health" },
			want: false,
		},
		"other scheme": {
			url:  func(host string) string { return "http://" + host + "/health" },
			want: false,
		},
		"upper case": {
			url:  func(host string) string { return "https://" + strings.ToUpper(host) },
			want: false,
		},
		"subdomain": {
			url:  func(host string) string { return "https://api." + host },
			want: true,
		},
		"other host": {
			url:  func(string) string { return "https://" + newHost() },
			want: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			limiter := redis.NewHostLimiter(newClient(t), 1)
			now := time.Now()
			until := now.Add(time.Minute)
			host := newHost()
			require.True(t, acquire(t, limiter, newClaim(t, "https://"+host+"/health", until), now))

			got, err := limiter.Acquire(t.Context(), newClaim(t, test.url(host), until), now)

			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestHostLimiterReleaseFreesSlot(t *testing.T) {
	limiter := redis.NewHostLimiter(newClient(t), 1)
	now := time.Now()
	until := now.Add(time.Minute)
	rawURL := "https://" + newHost() + "/health"
	held := newClaim(t, rawURL, until)
	require.True(t, acquire(t, limiter, held, now))

	err := limiter.Release(t.Context(), held)

	require.NoError(t, err)
	assert.True(t, acquire(t, limiter, newClaim(t, rawURL, until), now))
}

func TestHostLimiterAcquireReclaimsExpiredSlot(t *testing.T) {
	tests := map[string]struct {
		after time.Duration
		want  bool
	}{
		"slot expired":         {after: time.Minute, want: true},
		"slot about to expire": {after: time.Minute - time.Millisecond, want: false},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			limiter := redis.NewHostLimiter(newClient(t), 1)
			now := time.Now()
			rawURL := "https://" + newHost() + "/health"
			require.True(t, acquire(t, limiter, newClaim(t, rawURL, now.Add(time.Minute)), now))
			later := now.Add(test.after)
			next := newClaim(t, rawURL, later.Add(time.Minute))

			got, err := limiter.Acquire(t.Context(), next, later)

			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestHostLimiterReleaseKeepsSlotOfNewerClaim(t *testing.T) {
	limiter := redis.NewHostLimiter(newClient(t), 1)
	now := time.Now()
	rawURL := "https://" + newHost() + "/health"
	stale := newClaim(t, rawURL, now.Add(time.Minute))
	require.True(t, acquire(t, limiter, stale, now))
	later := stale.Until
	fresh := domain.Claim{Monitor: stale.Monitor, Until: later.Add(time.Minute)}
	require.True(t, acquire(t, limiter, fresh, later))

	err := limiter.Release(t.Context(), stale)

	require.NoError(t, err)
	assert.False(t, acquire(t, limiter, newClaim(t, rawURL, later.Add(time.Minute)), later))
}

func TestHostLimiterAcquireIsIdempotent(t *testing.T) {
	limiter := redis.NewHostLimiter(newClient(t), 1)
	now := time.Now()
	c := newClaim(t, "https://"+newHost()+"/health", now.Add(time.Minute))
	require.True(t, acquire(t, limiter, c, now))

	got, err := limiter.Acquire(t.Context(), c, now)

	require.NoError(t, err)
	assert.True(t, got)
}

func TestHostLimiterKeyLivesUntilLatestSlot(t *testing.T) {
	tests := map[string]struct {
		first  time.Duration
		second time.Duration
	}{
		"later slot extends key":    {first: time.Minute, second: 2 * time.Minute},
		"earlier slot keeps expiry": {first: 2 * time.Minute, second: time.Minute},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			client := newClient(t)
			limiter := redis.NewHostLimiter(client, 2)
			now := time.Now()
			host := newHost()
			rawURL := "https://" + host + "/health"
			require.True(t, acquire(t, limiter, newClaim(t, rawURL, now.Add(test.first)), now))
			latest := now.Add(max(test.first, test.second))
			want := time.Duration(latest.UnixMilli()) * time.Millisecond

			got, err := limiter.Acquire(t.Context(), newClaim(t, rawURL, now.Add(test.second)), now)

			require.NoError(t, err)
			assert.True(t, got)
			assert.Equal(t, want, client.PExpireTime(t.Context(), "pulsewatch:host:"+host).Val())
		})
	}
}

func TestHostLimiterConcurrentAcquireRespectsLimit(t *testing.T) {
	const limit = 5
	limiter := redis.NewHostLimiter(newClient(t), limit)
	now := time.Now()
	rawURL := "https://" + newHost() + "/health"
	claims := make([]domain.Claim, 200)
	for i := range claims {
		claims[i] = newClaim(t, rawURL, now.Add(time.Minute))
	}
	var acquired atomic.Int32
	var wg sync.WaitGroup

	for _, c := range claims {
		wg.Go(func() {
			ok, err := limiter.Acquire(t.Context(), c, now)
			if ok {
				acquired.Add(1)
			}
			assert.NoError(t, err)
		})
	}
	wg.Wait()

	assert.Equal(t, int32(limit), acquired.Load())
}

func TestHostLimiterAcquireRejectsInvalidURL(t *testing.T) {
	limiter := redis.NewHostLimiter(newClient(t), 1)
	now := time.Now()

	got, err := limiter.Acquire(t.Context(), newClaim(t, "https://[::1/health", now), now)

	require.Error(t, err)
	assert.False(t, got)
}
