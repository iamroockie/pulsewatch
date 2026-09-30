package observability_test

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/iamroockie/pulsewatch/internal/domain"
	"github.com/iamroockie/pulsewatch/internal/domain/domaintest"
	"github.com/iamroockie/pulsewatch/internal/observability"
)

func samples(t *testing.T, g prometheus.Gatherer) map[string]float64 {
	t.Helper()

	families, err := g.Gather()
	require.NoError(t, err)

	got := make(map[string]float64)
	for _, f := range families {
		for _, m := range f.GetMetric() {
			labels := seriesLabels(m)
			switch {
			case m.GetHistogram() != nil:
				got[f.GetName()+"_count"+labels] = float64(m.GetHistogram().GetSampleCount())
				got[f.GetName()+"_sum"+labels] = m.GetHistogram().GetSampleSum()
			case m.GetCounter() != nil:
				got[f.GetName()+labels] = m.GetCounter().GetValue()
			case m.GetGauge() != nil:
				got[f.GetName()+labels] = m.GetGauge().GetValue()
			}
		}
	}

	return got
}

func seriesLabels(m *dto.Metric) string {
	if len(m.GetLabel()) == 0 {
		return ""
	}

	pairs := make([]string, 0, len(m.GetLabel()))
	for _, l := range m.GetLabel() {
		pairs = append(pairs, l.GetName()+`="`+l.GetValue()+`"`)
	}

	return "{" + strings.Join(pairs, ",") + "}"
}

func newClaim(t *testing.T, dueAt time.Time) domain.Claim {
	t.Helper()

	return domain.Claim{
		Monitor: domaintest.NewMonitor(t, dueAt),
		DueAt:   dueAt,
		Until:   dueAt.Add(time.Minute),
	}
}

func TestNewRegistryCollectsRuntimeMetrics(t *testing.T) {
	got := samples(t, observability.NewRegistry())

	assert.Contains(t, got, "go_goroutines")
	assert.Contains(t, got, "process_resident_memory_bytes")
}
