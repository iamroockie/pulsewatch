package observability_test

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/iamroockie/pulsewatch/internal/observability"
)

func TestQueueCollectorQueriesBacklogOnEveryScrape(t *testing.T) {
	reg := prometheus.NewRegistry()
	queue := NewMockQueue(gomock.NewController(t))
	gomock.InOrder(
		queue.EXPECT().Backlog(gomock.Any()).Return(int64(7), nil),
		queue.EXPECT().Backlog(gomock.Any()).Return(int64(0), nil),
	)
	reg.MustRegister(observability.NewQueueCollector(queue))

	first := samples(t, reg)
	second := samples(t, reg)

	assert.Equal(t, map[string]float64{"pulsewatch_queue_depth": 7}, first)
	assert.Equal(t, map[string]float64{"pulsewatch_queue_depth": 0}, second)
}

func TestQueueCollectorBoundsBacklogQuery(t *testing.T) {
	reg := prometheus.NewRegistry()
	var bounded bool
	queue := NewMockQueue(gomock.NewController(t))
	queue.EXPECT().Backlog(gomock.Any()).DoAndReturn(
		func(ctx context.Context) (int64, error) {
			_, bounded = ctx.Deadline()

			return 0, nil
		},
	)
	reg.MustRegister(observability.NewQueueCollector(queue))

	_, err := reg.Gather()

	require.NoError(t, err)
	assert.True(t, bounded)
}

func TestQueueCollectorReportsBacklogError(t *testing.T) {
	reg := prometheus.NewRegistry()
	errStorage := errors.New("storage is down")
	queue := NewMockQueue(gomock.NewController(t))
	queue.EXPECT().Backlog(gomock.Any()).Return(int64(0), errStorage)
	reg.MustRegister(observability.NewQueueCollector(queue))

	families, err := reg.Gather()

	require.ErrorIs(t, err, errStorage)
	assert.Empty(t, families)
}
