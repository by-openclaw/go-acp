// Package hyperdeck implements the consumer side of the Blackmagic
// HyperDeck Ethernet Protocol.
package hyperdeck

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"

	"acp/internal/blackmagic-hyperdeck/codec"
	"acp/internal/protocol"
)

const DefaultPort = codec.DefaultPort

func init() {
	protocol.Register(&Factory{})
}

type Factory struct{}

func (f *Factory) Meta() protocol.ProtocolMeta {
	return protocol.ProtocolMeta{
		Name:        "blackmagic-hyperdeck",
		DefaultPort: DefaultPort,
		Description: "Blackmagic HyperDeck Ethernet Protocol (TCP text control)",
	}
}

func (f *Factory) New(logger *slog.Logger) protocol.Protocol {
	return &Plugin{logger: logger}
}

func NewPlugin(logger *slog.Logger) *Plugin {
	return &Plugin{logger: logger}
}

type Plugin struct {
	logger *slog.Logger

	mu    sync.Mutex
	conn  net.Conn
	r     *bufio.Reader
	host  string
	port  int
	cache map[string]protocol.Object
}

func (p *Plugin) Connect(ctx context.Context, host string, port int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != nil {
		if p.host == host && (port == 0 || p.port == port) {
			return nil
		}
		return fmt.Errorf("blackmagic-hyperdeck: already connected to %s:%d", p.host, p.port)
	}
	if port == 0 {
		port = DefaultPort
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return err
	}
	p.conn = conn
	p.r = bufio.NewReader(conn)
	p.host = host
	p.port = port
	p.cache = map[string]protocol.Object{}

	// PDF p.11, "Connection response": on connection an async
	// "500 connection info" message is delivered.
	resp, err := codec.ReadResponse(p.r)
	if err != nil {
		_ = p.disconnectLocked()
		return fmt.Errorf("read connection info: %w", err)
	}
	if resp.Code == 120 {
		_ = p.disconnectLocked()
		return &codec.FailureError{Code: resp.Code, Text: resp.Text}
	}
	if resp.Code != 500 {
		_ = p.disconnectLocked()
		return fmt.Errorf("blackmagic-hyperdeck: expected 500 connection info, got %03d %s", resp.Code, resp.Text)
	}
	return nil
}

func (p *Plugin) Disconnect() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.disconnectLocked()
}

func (p *Plugin) disconnectLocked() error {
	if p.conn == nil {
		return nil
	}
	err := p.conn.Close()
	p.conn = nil
	p.r = nil
	return err
}

func (p *Plugin) GetDeviceInfo(ctx context.Context) (protocol.DeviceInfo, error) {
	resp, err := p.command(ctx, "device info", nil)
	if err != nil {
		return protocol.DeviceInfo{}, err
	}
	return protocol.DeviceInfo{
		IP:              p.host,
		Port:            p.port,
		NumSlots:        atoi(resp.Params["slot count"]),
		ProtocolVersion: atoiLeading(resp.Params["protocol version"]),
	}, nil
}

func (p *Plugin) GetSlotInfo(ctx context.Context, slot int) (protocol.SlotInfo, error) {
	params := map[string]string{}
	if slot > 0 {
		params["slot id"] = strconv.Itoa(slot)
	}
	resp, err := p.command(ctx, "slot info", params)
	if err != nil {
		return protocol.SlotInfo{}, err
	}
	if slot == 0 {
		slot = atoi(resp.Params["slot id"])
	}
	return protocol.SlotInfo{
		Slot:   slot,
		Status: slotStatus(resp.Params["status"]),
		Identity: map[string]string{
			"slot name":    resp.Params["slot name"],
			"device name":  resp.Params["device name"],
			"volume name":  resp.Params["volume name"],
			"video format": resp.Params["video format"],
		},
	}, nil
}

func (p *Plugin) Walk(ctx context.Context, slot int) ([]protocol.Object, error) {
	var out []protocol.Object

	if resp, err := p.command(ctx, "device info", nil); err == nil {
		addString(&out, p.cache, 0, "device.model", "Device Model", resp.Params["model"], false)
		addString(&out, p.cache, 1, "device.protocol_version", "Protocol Version", resp.Params["protocol version"], false)
		addString(&out, p.cache, 2, "device.software_version", "Software Version", resp.Params["software version"], false)
		addInt(&out, p.cache, 3, "device.slot_count", "Slot Count", int64(atoi(resp.Params["slot count"])), false)
	}
	if resp, err := p.command(ctx, "remote", nil); err == nil {
		addBool(&out, p.cache, 10, "remote.enabled", "Remote Enabled", parseBool(resp.Params["enabled"]), true)
		addBool(&out, p.cache, 11, "remote.override", "Remote Override", parseBool(resp.Params["override"]), true)
	}
	if resp, err := p.command(ctx, "transport info", nil); err == nil {
		addString(&out, p.cache, 20, "transport.status", "Transport Status", resp.Params["status"], true)
		addInt(&out, p.cache, 21, "transport.speed", "Transport Speed", int64(atoi(resp.Params["speed"])), false)
		addString(&out, p.cache, 22, "transport.timecode", "Transport Timecode", resp.Params["timecode"], false)
		addString(&out, p.cache, 23, "transport.video_format", "Transport Video Format", resp.Params["video format"], false)
	}
	if resp, err := p.command(ctx, "slot info", slotParam(slot)); err == nil {
		addString(&out, p.cache, 30, "slot.status", "Slot Status", resp.Params["status"], false)
		addString(&out, p.cache, 31, "slot.volume_name", "Slot Volume Name", resp.Params["volume name"], false)
		addInt(&out, p.cache, 32, "slot.remaining_size", "Slot Remaining Size", int64(atoi(resp.Params["remaining size"])), false)
	}
	if resp, err := p.command(ctx, "notify", nil); err == nil {
		i := 40
		for _, k := range []string{"transport", "slot", "remote", "configuration", "clips", "disk", "device info"} {
			addBool(&out, p.cache, i, "notify."+strings.ReplaceAll(k, " ", "_"), "Notify "+title(k), parseBool(resp.Params[k]), true)
			i++
		}
	}
	return out, nil
}

func (p *Plugin) GetValue(ctx context.Context, req protocol.ValueRequest) (protocol.Value, error) {
	path := requestPath(req)
	switch path {
	case "remote.enabled", "remote.override":
		resp, err := p.command(ctx, "remote", nil)
		if err != nil {
			return protocol.Value{}, err
		}
		key := strings.TrimPrefix(path, "remote.")
		return boolValue(parseBool(resp.Params[key])), nil
	case "transport.status", "transport.speed", "transport.timecode", "transport.video_format":
		resp, err := p.command(ctx, "transport info", nil)
		if err != nil {
			return protocol.Value{}, err
		}
		return valueFromParam(path, resp.Params), nil
	case "slot.status", "slot.volume_name", "slot.remaining_size":
		resp, err := p.command(ctx, "slot info", slotParam(req.Slot))
		if err != nil {
			return protocol.Value{}, err
		}
		return valueFromParam(path, resp.Params), nil
	default:
		if strings.HasPrefix(path, "notify.") {
			resp, err := p.command(ctx, "notify", nil)
			if err != nil {
				return protocol.Value{}, err
			}
			key := strings.ReplaceAll(strings.TrimPrefix(path, "notify."), "_", " ")
			return boolValue(parseBool(resp.Params[key])), nil
		}
		return protocol.Value{}, fmt.Errorf("blackmagic-hyperdeck: unsupported get path %q", path)
	}
}

func (p *Plugin) SetValue(ctx context.Context, req protocol.ValueRequest, val protocol.Value) (protocol.Value, error) {
	path := requestPath(req)
	switch path {
	case "remote.enabled":
		_, err := p.command(ctx, "remote", map[string]string{"enable": boolString(val)})
		return boolValue(parseValueBool(val)), err
	case "remote.override":
		_, err := p.command(ctx, "remote", map[string]string{"override": boolString(val)})
		return boolValue(parseValueBool(val)), err
	case "transport.status":
		status := strings.ToLower(val.Str)
		switch status {
		case "play":
			_, err := p.command(ctx, "play", nil)
			return stringValue("play"), err
		case "record":
			_, err := p.command(ctx, "record", nil)
			return stringValue("record"), err
		case "stopped", "stop":
			_, err := p.command(ctx, "stop", nil)
			return stringValue("stopped"), err
		default:
			return protocol.Value{}, fmt.Errorf("blackmagic-hyperdeck: unsupported transport status %q", val.Str)
		}
	default:
		if strings.HasPrefix(path, "notify.") {
			key := strings.ReplaceAll(strings.TrimPrefix(path, "notify."), "_", " ")
			_, err := p.command(ctx, "notify", map[string]string{key: boolString(val)})
			return boolValue(parseValueBool(val)), err
		}
		return protocol.Value{}, fmt.Errorf("blackmagic-hyperdeck: unsupported set path %q", path)
	}
}

func (p *Plugin) Subscribe(req protocol.ValueRequest, fn protocol.EventFunc) error {
	return protocol.ErrNotImplemented
}

func (p *Plugin) Unsubscribe(req protocol.ValueRequest) error { return nil }

func (p *Plugin) command(ctx context.Context, name string, params map[string]string) (codec.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn == nil {
		return codec.Response{}, fmt.Errorf("blackmagic-hyperdeck: not connected")
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = p.conn.SetDeadline(deadline)
		defer func() { _ = p.conn.SetDeadline(timeZero()) }()
	}
	if _, err := p.conn.Write([]byte(codec.CommandLine(name, params))); err != nil {
		return codec.Response{}, err
	}
	for {
		resp, err := codec.ReadResponse(p.r)
		if err != nil {
			return codec.Response{}, err
		}
		if resp.Async() {
			continue
		}
		if !resp.OK() {
			return resp, &codec.FailureError{Code: resp.Code, Text: resp.Text}
		}
		return resp, nil
	}
}
