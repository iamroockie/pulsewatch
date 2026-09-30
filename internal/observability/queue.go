package observability

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type queueCollector struct {
	queue Queue
	depth *prometheus.Desc
}

func NewQueueCollector(q Queue) prometheus.Collector {
	return &queueCollector{
		queue: q,
		depth: prometheus.NewDesc(
			"pulsewatch_queue_depth",
			"Active monitors that are due and not claimed by any worker.",
			nil, nil,
		),
	}
}

func (c *queueCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.depth
}

func (c *queueCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	due, err := c.queue.Backlog(ctx)
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.depth, err)
		return
	}

	ch <- prometheus.MustNewConstMetric(c.depth, prometheus.GaugeValue, float64(due))
}
