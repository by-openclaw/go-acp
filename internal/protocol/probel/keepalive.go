package probel

import (
	iprobel "acp/internal/probel"
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
// Reference: TS assets/probel/smh-probelsw08p/src/command/application-keep-alive/.
func (p *Plugin) installKeepaliveAutoResponder(cli *iprobel.Client) {
	cli.Subscribe(func(f iprobel.Frame) {
		if f.ID != iprobel.TxAppKeepaliveRequest {
			return
		}
		if err := iprobel.DecodeKeepaliveRequest(f); err != nil {
			return
		}
		if err := cli.Write(iprobel.Pack(iprobel.EncodeKeepaliveResponse())); err != nil {
			p.logger.Warn("probel keepalive response write failed",
				"err", err.Error())
		}
	})
}
