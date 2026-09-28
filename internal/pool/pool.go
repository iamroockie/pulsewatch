package pool

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"sync"

	"github.com/iamroockie/pulsewatch/internal/domain"
)

type Pool struct {
	runner Runner
	log    *slog.Logger
	tokens chan struct{}
	freed  chan struct{}
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

func NewPool(size uint, runner Runner, log *slog.Logger) *Pool {
	ctx, cancel := context.WithCancel(context.Background())

	return &Pool{
		runner: runner,
		log:    log,
		tokens: make(chan struct{}, size),
		freed:  make(chan struct{}, 1),
		ctx:    ctx,
		cancel: cancel,
	}
}

func (p *Pool) Free() int {
	return cap(p.tokens) - len(p.tokens)
}

func (p *Pool) Freed() <-chan struct{} {
	return p.freed
}

func (p *Pool) Go(c domain.Claim) {
	p.tokens <- struct{}{}
	p.wg.Go(func() {
		defer p.release()
		p.run(c)
	})
}

func (p *Pool) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.cancel()
		return nil
	case <-ctx.Done():
		p.cancel()
		<-done
		return ctx.Err()
	}
}

func (p *Pool) release() {
	<-p.tokens
	select {
	case p.freed <- struct{}{}:
	default:
	}
}

func (p *Pool) run(c domain.Claim) {
	defer func() {
		if r := recover(); r != nil {
			p.log.Error("check panicked",
				"monitor_id", c.Monitor.ID,
				"panic", r,
				"stack", string(debug.Stack()),
			)
		}
	}()

	err := p.runner.Run(p.ctx, c)
	if err == nil {
		return
	}

	if errors.Is(err, context.Canceled) {
		p.log.Warn("check interrupted", "monitor_id", c.Monitor.ID, "error", err)
		return
	}

	p.log.Error("check failed", "monitor_id", c.Monitor.ID, "error", err)
}
