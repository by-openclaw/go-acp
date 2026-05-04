package probelsw08p

import (
	"dhs/internal/probel-sw08p/codec"
)

// keepaliveAutoResponder returns a codec.ClientConfig.OnEvent listener
// that auto-replies to any TxAppKeepaliveRequest (0x11) with an
// RxAppKeepaliveResponse (0x22). Wired through ClientConfig.OnEvent so
// the listener is registered BEFORE the reader goroutine starts —
// guarantees the very first inbound keepalive is never lost to a
// post-Dial Subscribe race (refs #234).
//
// The response goes out via Client.Write (bypass-retry, like the
// reader's own ACK/NAK emission), so it does not collide with any
// in-flight Send's pending waiter.
//
// Reference: NOT in SW-P-08 §3.2/§3.3 — see CLAUDE.md "Application
// keepalive" for the byte-0x11/0x22 ping/pong pair the testbed uses
// to keep TCP sessions warm.
func (p *Plugin) keepaliveAutoResponder() func(*codec.Client, codec.Frame) {
	return func(cli *codec.Client, f codec.Frame) {
		if f.ID != codec.TxAppKeepaliveRequest {
			return
		}
		if err := codec.DecodeKeepaliveRequest(f); err != nil {
			return
		}
		if err := cli.Write(codec.Pack(codec.EncodeKeepaliveResponse())); err != nil {
			p.logger.Warn("probel keepalive response write failed",
				"err", err.Error())
		}
	}
}
