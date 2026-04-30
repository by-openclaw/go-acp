package probelsw02p

import (
	"context"
	"log/slog"
	"time"

	"acp/internal/probel-sw02p/codec"
)

// startKeepalive spawns the background goroutine that handles two
// jobs: a one-shot bootstrap rx 01 sweep over (0..Dsts-1) at session
// start, and a continuous rotating rx 01 ping that mirrors VSM's
// keep-alive behaviour. Both honour the MatrixConfig knobs.
//
// SW-P-02 has no in-protocol keep-alive command (§3.1 transparent
// framing, §3.2 has no APP_KEEPALIVE pair like SW-P-08's 0x11/0x22),
// so the rx 01 / tx 03 round-trip IS the keep-alive — same trick VSM
// uses against shipping matrices.
//
// The goroutine exits cleanly when ctx is cancelled (Disconnect).
//
// Holds no Plugin lock — it reads the codec.Client pointer once and
// fires through it. If the client is closed mid-sweep, the underlying
// Send returns net.ErrClosed and the goroutine exits.
func (p *Plugin) startKeepalive(ctx context.Context, cli *codec.Client) {
	cfg := p.matrixCfg
	if cfg.Dsts == 0 {
		// Nothing to poll — caller didn't set a matrix size. Leaving
		// the goroutine unstarted means SetMatrixConfig + Connect
		// later still arms the next session.
		return
	}

	bootstrapSpacing := cfg.BootstrapSpacing
	if bootstrapSpacing == 0 {
		bootstrapSpacing = DefaultBootstrapSpacing
	}
	keepaliveSpacing := cfg.AppKeepaliveSpacing
	if keepaliveSpacing == 0 {
		keepaliveSpacing = DefaultAppKeepaliveSpacing
	}

	go p.runKeepalive(ctx, cli, cfg, bootstrapSpacing, keepaliveSpacing)
}

// runKeepalive is the goroutine body. Split out for testability —
// callers can drive it with a stub client + cancellable ctx.
func (p *Plugin) runKeepalive(
	ctx context.Context,
	cli *codec.Client,
	cfg MatrixConfig,
	bootstrapSpacing time.Duration,
	keepaliveSpacing time.Duration,
) {
	if cfg.InitialPoll {
		p.bootstrapSweep(ctx, cli, cfg, bootstrapSpacing)
	}
	if keepaliveSpacing < 0 {
		// Caller explicitly disabled the keep-alive ping.
		return
	}
	p.keepalivePingLoop(ctx, cli, cfg, keepaliveSpacing)
}

// bootstrapSweep emits one rx 01 (or rx 65 for dst > 1023) per dst
// across 0..Dsts-1. Cancels promptly on ctx.Done.
func (p *Plugin) bootstrapSweep(
	ctx context.Context,
	cli *codec.Client,
	cfg MatrixConfig,
	spacing time.Duration,
) {
	p.logger.Debug("probel-sw02p bootstrap sweep starting",
		slog.Int("dsts", int(cfg.Dsts)),
		slog.Duration("spacing", spacing),
	)
	for dst := uint16(0); dst < cfg.Dsts; dst++ {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := sendInterrogate(ctx, cli, dst); err != nil {
			p.logger.Debug("probel-sw02p bootstrap sweep aborted",
				slog.Int("dst", int(dst)),
				slog.String("err", err.Error()),
			)
			return
		}
		if spacing > 0 && dst+1 < cfg.Dsts {
			select {
			case <-ctx.Done():
				return
			case <-time.After(spacing):
			}
		}
	}
	p.logger.Debug("probel-sw02p bootstrap sweep complete",
		slog.Int("dsts", int(cfg.Dsts)),
	)
}

// keepalivePingLoop fires one rx 01 every spacing on a rotating dst
// cursor. Mirrors VSM's continuous-poll keep-alive trick.
func (p *Plugin) keepalivePingLoop(
	ctx context.Context,
	cli *codec.Client,
	cfg MatrixConfig,
	spacing time.Duration,
) {
	t := time.NewTicker(spacing)
	defer t.Stop()
	cursor := uint16(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := sendInterrogate(ctx, cli, cursor); err != nil {
			p.logger.Debug("probel-sw02p keep-alive ping exiting",
				slog.Int("dst", int(cursor)),
				slog.String("err", err.Error()),
			)
			return
		}
		cursor = (cursor + 1) % cfg.Dsts
	}
}

// sendInterrogate emits rx 01 for dst <= 1023, rx 65 otherwise. Auto-
// escalation matches the spec address-range split (§3.2.3 vs §3.2.47).
// Uses Client.Write (raw bytes, bypasses single-flight) so the keep-
// alive ping doesn't compete with caller-driven Sends. Replies (tx 03)
// flow through the Subscribe path to whoever owns the tally cache.
// ctx is honoured at the outer goroutine; this call is sync.
func sendInterrogate(ctx context.Context, cli *codec.Client, dst uint16) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var f codec.Frame
	if dst <= 1023 {
		f = codec.EncodeInterrogate(codec.InterrogateParams{Destination: dst})
	} else {
		f = codec.EncodeExtendedInterrogate(codec.ExtendedInterrogateParams{Destination: dst})
	}
	return cli.Write(codec.Pack(f))
}
