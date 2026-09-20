// Package mnset is the outbound REST client for Riedel MuoN eMSFP and
// FusioN modules — the devices MN SET manages — issue #1110.
//
// It talks to the module DIRECTLY, http://<module>/emsfp/node/v1/…, not
// through MN SET: every resource the MN SET "Rest" page shows answered
// on the module itself (verified on a FusioN6, fw 0x68cd783f). The
// resources carry values and structure only — no min/max/description;
// that metadata exists only in the SNMP MIB and is a later layer. MN SET
// is reached by one function, Inventory, as a device list.
//
// The wire is HTTP/JSON, so there is no codec package: a resource is a
// generic JSON document flattened into consumer.Objects. There is no
// producer.
package mnset

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	stdhttp "net/http"
	"strconv"
	"time"

	"dhs/internal/metrics"
	transporthttp "dhs/internal/transport/http"
)

// apiPrefix is the emSFP node API root on every module.
const apiPrefix = "/emsfp/node/v1/"

// MaxBody caps one resource document. flows on a 2R6T is ~100 KiB;
// 8 MiB leaves room for a much larger program while refusing nonsense.
const MaxBody = 8 << 20

// defaultTimeout bounds one request when the operator sets nothing.
const defaultTimeout = 8 * time.Second

// client is one module's REST endpoint.
type client struct {
	base string
	http *transporthttp.Client
}

// newClient builds a client for host:port. rt (nil = default transport)
// is the injected RoundTripper; met, when non-nil, receives every
// request (tx/rx/latency) so the plugin reports like any other.
func newClient(host string, port int, timeout time.Duration, rt stdhttp.RoundTripper, met *metrics.Connector) *client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &client{
		base: "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + apiPrefix,
		http: &transporthttp.Client{
			HTTP:    &stdhttp.Client{Timeout: timeout, Transport: rt},
			MaxBody: MaxBody,
			Metrics: met,
		},
	}
}

// get fetches one resource, addressed relative to the node root ("",
// "flows", "self/ipconfig", "flows/<id>"), as a generic document. A body
// that is not JSON is the module's text form — the sdp/<id> items are
// raw SDP — and comes back as a string.
func (c *client) get(ctx context.Context, rel string) (any, error) {
	raw, err := c.http.GetBytes(ctx, c.base+rel)
	if err != nil {
		return nil, fmt.Errorf("mnset GET %s: %w", rel, err)
	}
	if doc, err := decodeDoc(raw); err == nil {
		return doc, nil
	}
	return string(raw), nil
}

// decodeDoc decodes with UseNumber so an integer stays an integer all
// the way to the PUT: a port must go back as 20000, never 20000.0.
func decodeDoc(raw []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var doc any
	if err := d.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return doc, nil
}

// put writes one whole resource document back. A non-2xx is an error
// carrying the module's own message (Riedel answers
// {"code":N,"message":"…"}), so a refused write reads as the device
// said it, not as a bare status.
func (c *client) put(ctx context.Context, rel string, doc any) error {
	var v any
	status, err := c.http.PutJSON(ctx, c.base+rel, doc, &v)
	if err != nil {
		return fmt.Errorf("mnset PUT %s: %w", rel, err)
	}
	if status/100 != 2 {
		return fmt.Errorf("mnset PUT %s: module answered %d %s", rel, status, deviceMessage(v))
	}
	return nil
}

// deviceMessage extracts the module's own error text when it sent one.
func deviceMessage(v any) string {
	if m, ok := v.(map[string]any); ok {
		if s, ok := m["message"].(string); ok && s != "" {
			return s
		}
	}
	return fmt.Sprintf("%.120v", v)
}
