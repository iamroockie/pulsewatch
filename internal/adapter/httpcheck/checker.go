package httpcheck

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/iamroockie/pulsewatch/internal/domain"
)

type Checker struct {
	client  *http.Client
	backoff Backoff
}

func NewChecker(b Backoff) *Checker {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true

	return &Checker{
		client:  &http.Client{Transport: transport},
		backoff: b,
	}
}

func (c *Checker) Check(ctx context.Context, s domain.CheckSettings) (domain.CheckResult, error) {
	for attempt := int32(1); ; attempt++ {
		result, retryable := c.attempt(ctx, s)
		if err := ctx.Err(); err != nil {
			return domain.CheckResult{}, err
		}

		result.Attempts = attempt
		if result.IsUp || !retryable || attempt > s.MaxRetries {
			return result, nil
		}

		if err := c.wait(ctx, attempt); err != nil {
			return domain.CheckResult{}, err
		}
	}
}

func (c *Checker) attempt(ctx context.Context, s domain.CheckSettings) (domain.CheckResult, bool) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, http.NoBody)
	if err != nil {
		return domain.CheckResult{Error: err.Error()}, false
	}
	req.Header.Set("User-Agent", "pulsewatch")

	resp, err := c.client.Do(req)
	latency := time.Since(start)
	if err != nil {
		return domain.CheckResult{Latency: latency, Error: describe(ctx, err)}, true
	}
	defer resp.Body.Close()

	result := domain.CheckResult{
		IsUp:       resp.StatusCode < http.StatusBadRequest,
		StatusCode: resp.StatusCode,
		Latency:    latency,
	}

	return result, isRetryable(resp.StatusCode)
}

func (c *Checker) wait(ctx context.Context, attempt int32) error {
	timer := time.NewTimer(c.backoff.Delay(attempt))
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isRetryable(code int) bool {
	return code >= http.StatusInternalServerError ||
		code == http.StatusRequestTimeout ||
		code == http.StatusTooManyRequests
}

func describe(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timeout"
	}
	if urlErr, ok := errors.AsType[*url.Error](err); ok {
		return urlErr.Err.Error()
	}

	return err.Error()
}
