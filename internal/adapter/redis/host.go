package redis

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/iamroockie/pulsewatch/internal/domain"
)

type HostLimiter struct {
	client *goredis.Client
	limit  uint
}

func NewHostLimiter(client *goredis.Client, limit uint) *HostLimiter {
	return &HostLimiter{client: client, limit: limit}
}

func (l *HostLimiter) Acquire(ctx context.Context, c domain.Claim, now time.Time) (bool, error) {
	script := `
		local key = KEYS[1]
		local now, expires, limit, slot = ARGV[1], ARGV[2], tonumber(ARGV[3]), ARGV[4]
		redis.call('ZREMRANGEBYSCORE', key, '-inf', now)
		if redis.call('ZSCORE', key, slot) then
			return 1
		end
		if redis.call('ZCARD', key) >= limit then
			return 0
		end
		redis.call('ZADD', key, expires, slot)
		redis.call('PEXPIREAT', key, expires, 'NX')
		redis.call('PEXPIREAT', key, expires, 'GT')
		return 1
	`

	key, err := hostKey(c.Monitor.Settings.URL)
	if err != nil {
		return false, err
	}

	acquired, err := goredis.NewScript(script).
		Run(ctx, l.client, []string{key}, now.UnixMilli(), c.Until.UnixMilli(), l.limit, slot(c)).
		Bool()
	if err != nil {
		return false, fmt.Errorf("acquire host slot: %w", err)
	}

	return acquired, nil
}

func (l *HostLimiter) Release(ctx context.Context, c domain.Claim) error {
	key, err := hostKey(c.Monitor.Settings.URL)
	if err != nil {
		return err
	}

	if err := l.client.ZRem(ctx, key, slot(c)).Err(); err != nil {
		return fmt.Errorf("release host slot: %w", err)
	}

	return nil
}

func hostKey(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse monitor url: %w", err)
	}

	return "pulsewatch:host:" + strings.ToLower(u.Hostname()), nil
}

func slot(c domain.Claim) string {
	return c.Monitor.ID.String() + ":" + strconv.FormatInt(c.Until.UnixMicro(), 10)
}
