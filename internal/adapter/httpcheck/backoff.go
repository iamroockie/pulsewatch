package httpcheck

import (
	"math/rand/v2"
	"time"
)

type Backoff struct {
	Base time.Duration
	Max  time.Duration
}

func (b Backoff) Delay(retry int32) time.Duration {
	d := min(b.Base, b.Max)
	for range retry - 1 {
		if d > b.Max/2 {
			d = b.Max
			break
		}
		d *= 2
	}

	return d/2 + rand.N(d/2+1)
}
