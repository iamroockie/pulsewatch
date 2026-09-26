package domain

import "time"

type CheckResult struct {
	IsUp       bool
	StatusCode int
	Latency    time.Duration
	Attempts   int32
	Error      string
}
