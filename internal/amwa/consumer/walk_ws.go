package consumer

import (
	"context"

	"dhs/internal/amwa/codec/is04"
)

// SendersViaSubscription enumerates Senders through a Query API
// WebSocket subscription — the enumeration path IS-04 v1.0 actually
// specifies for dynamic state. REST pagination arrived in v1.1; a
// v1.0 Query API whose collection exceeds its page size has no REST
// way to serve the remainder, so a controller that only ever GETs the
// collection silently sees a fraction of the plant at v1.0.
func (c *Controller) SendersViaSubscription(ctx context.Context) ([]is04.Sender, error) {
	raw, err := c.client.ListViaSubscription(ctx, "senders")
	if err != nil {
		return nil, err
	}
	out := make([]is04.Sender, 0, len(raw))
	for _, rb := range raw {
		v, err := c.client.Codec.DecodeSender(rb)
		if err != nil {
			continue // a non-decoding grain row costs one resource, not the listing
		}
		out = append(out, v)
	}
	return out, nil
}

// ReceiversViaSubscription is the Receiver twin of
// [Controller.SendersViaSubscription].
func (c *Controller) ReceiversViaSubscription(ctx context.Context) ([]is04.Receiver, error) {
	raw, err := c.client.ListViaSubscription(ctx, "receivers")
	if err != nil {
		return nil, err
	}
	out := make([]is04.Receiver, 0, len(raw))
	for _, rb := range raw {
		v, err := c.client.Codec.DecodeReceiver(rb)
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}
