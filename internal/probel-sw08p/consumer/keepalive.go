package probelsw08p

import (
	"dhs/internal/probel-sw08p/codec"
)

// installKeepaliveAutoResponder subscribes to the shared client's async
// event stream and auto-replies to any TxAppKeepaliveRequest (0x11) with
// an RxAppKeepaliveResponse (0x22). Kept as a passive listener — the
// response goes out via client.Write (bypass-retry, like the reader's
// own ACK/NAK emission), so it does not collide with any in-flight
// Send's pending waiter.
//
// Rationale: the TS matrix emulator pings controllers every ~30 s; a
// controller that never responds is dropped. Auto-response keeps the
// session alive without bothering application code.
//
// Reference: NOT in SW-P-08 §3.2/§3.3 — see CLAUDE.md "Application
// keepalive" for the byte-0x11/0x22 ping/pong pair the testbed
// (Lawo VSM + Commie) uses to keep TCP sessions warm.
func (p *Plugin) installKeepaliveAutoResponder(cli *codec.Client) {
	cli.Subscribe(func(f codec.Frame) {
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
	})
}
