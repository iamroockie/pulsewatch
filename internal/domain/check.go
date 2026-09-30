package domain

import (
	"time"
	"uuid"
)

type Claim struct {
	Monitor *Monitor
	DueAt   time.Time
	Until   time.Time
}

type Check struct {
	MonitorID uuid.UUID
	CheckedAt time.Time
	Result    CheckResult
}

type CheckResult struct {
	IsUp       bool
	StatusCode int
	Latency    time.Duration
	Attempts   int32
	Error      string
}
