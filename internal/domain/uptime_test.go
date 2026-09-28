package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/iamroockie/pulsewatch/internal/domain"
)

func TestUptimeRatio(t *testing.T) {
	tests := map[string]struct {
		uptime domain.Uptime
		want   float64
		wantOK bool
	}{
		"no checks":   {uptime: domain.Uptime{Checks: 0, Up: 0}, want: 0, wantOK: false},
		"all up":      {uptime: domain.Uptime{Checks: 4, Up: 4}, want: 1, wantOK: true},
		"all down":    {uptime: domain.Uptime{Checks: 4, Up: 0}, want: 0, wantOK: true},
		"partly down": {uptime: domain.Uptime{Checks: 4, Up: 3}, want: 0.75, wantOK: true},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, ok := test.uptime.Ratio()

			assert.Equal(t, test.wantOK, ok)
			assert.InDelta(t, test.want, got, 1e-9)
		})
	}
}
