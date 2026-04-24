package probelsw02p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw02p/codec"
)

// SendExtendedProtectTallyDumpRequest emits rx 105 Extended PROTECT
// TALLY DUMP REQUEST (§3.2.69) asking the peer for up to count
// protect entries starting at startDest. Unlike most Send* helpers
// in this plugin, rx 105 can trigger MULTIPLE tx 100 broadcasts
// (§3.2.64 caps each at 32 entries), so the caller subscribes
// separately to tx 100 via SubscribeExtendedProtectTallyDump to
// collect the fan-out. This helper just writes the request frame
// and returns immediately.
func (p *Plugin) SendExtendedProtectTallyDumpRequest(ctx context.Context, startDest uint16, count uint8) error {
	cli, err := p.getClient()
	if err != nil {
		return err
	}
	req := codec.EncodeExtendedProtectTallyDumpRequest(codec.ExtendedProtectTallyDumpRequestParams{
		Count:            count,
		StartDestination: startDest,
	})
	if _, err := cli.Send(ctx, req, nil); err != nil {
		return fmt.Errorf("probel-sw02p: SendExtendedProtectTallyDumpRequest: %w", err)
	}
	return nil
}
