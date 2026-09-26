package httpcheck_test

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/iamroockie/pulsewatch/internal/adapter/httpcheck"
)

func TestBackoffDelay(t *testing.T) {
	b := httpcheck.Backoff{Base: 100 * time.Millisecond, Max: time.Second}
	tests := map[string]struct {
		retry int32
		want  time.Duration
	}{
		"first retry":    {retry: 1, want: 100 * time.Millisecond},
		"second retry":   {retry: 2, want: 200 * time.Millisecond},
		"fourth retry":   {retry: 4, want: 800 * time.Millisecond},
		"capped by max":  {retry: 5, want: time.Second},
		"far beyond max": {retry: 100, want: time.Second},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got := make([]time.Duration, 1000)

			for i := range got {
				got[i] = b.Delay(test.retry)
			}

			assert.GreaterOrEqual(t, slices.Min(got), test.want/2)
			assert.LessOrEqual(t, slices.Max(got), test.want)
			assert.Less(t, slices.Min(got), slices.Max(got))
		})
	}
}
