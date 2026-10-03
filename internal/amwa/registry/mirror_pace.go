package registry

// Pacing the mirror's pushes into the target registry.
//
// A grain row is forwarded as one POST, and the source hands the mirror
// its whole catalogue at every (re)subscription — six topics, four
// minors, up to twenty-four watchers at once. Unpaced, a restart replays
// the plant into the target in one burst, and a target like Cerebrum
// dies on about a thousand resources at a time (#1283). The pace is a
// token bucket shared by every watcher: at most TargetPace requests
// per second, with a burst of one second's worth, so a quiet mirror
// still forwards a change at once and a storm is spread out.

import (
	"context"
	"sync"
	"time"

	"dhs/internal/clock"
)

// DefaultTargetPace is the requests per second the mirror sends the
// target when the operator sets nothing: a plant of five thousand
// resources in under a minute, and nothing a registry has to buffer.
const DefaultTargetPace = 100

// pacer is a token bucket on the injected clock.
type pacer struct {
	clk    clock.Clock
	rate   float64 // tokens per second
	burst  float64
	mu     sync.Mutex
	tokens float64
	last   time.Time
}

func newPacer(clk clock.Clock, perSecond int) *pacer {
	if perSecond <= 0 {
		perSecond = DefaultTargetPace
	}
	return &pacer{clk: clk, rate: float64(perSecond), burst: float64(perSecond), tokens: float64(perSecond), last: clk.Now()}
}

// wait blocks until a token is available or ctx ends. Tokens refill at
// the rate, capped at the burst; a request never waits more than one
// token's worth once the bucket has drained.
func (p *pacer) wait(ctx context.Context) error {
	for {
		p.mu.Lock()
		now := p.clk.Now()
		p.tokens += now.Sub(p.last).Seconds() * p.rate
		if p.tokens > p.burst {
			p.tokens = p.burst
		}
		p.last = now
		if p.tokens >= 1 {
			p.tokens--
			p.mu.Unlock()
			return nil
		}
		need := time.Duration((1 - p.tokens) / p.rate * float64(time.Second))
		p.mu.Unlock()
		if err := p.clk.Sleep(ctx, need); err != nil {
			return err
		}
	}
}
