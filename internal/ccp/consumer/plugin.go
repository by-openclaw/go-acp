// Package ccp is the consumer of the Axon Cortex Control Panel Protocol
// (CCPP 1.18): it connects to an Axon hardware panel (CCP-1601 and kin)
// as a second client next to Cerebrum, reads its status, draws button
// labels and colours, and reports button, rotary and T-bar actions.
package ccp

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"dhs/internal/ccp/codec"
	dhsc "dhs/internal/consumer"
	"dhs/internal/plugin"
)

// Name is the registry name.
const Name = "ccp"

func init() { dhsc.Register(&Factory{}) }

// Factory builds Plugin instances.
type Factory struct{}

// Meta describes the plugin to the registry.
func (f *Factory) Meta() dhsc.ProtocolMeta {
	return dhsc.ProtocolMeta{Name: Name, DefaultPort: codec.DefaultPort,
		Description: "Axon Cortex Control Panel Protocol (CCP-1601 …) — TCP 2071, binary"}
}

// New builds a plugin from the injected dependency set.
func (f *Factory) New(deps plugin.Deps) dhsc.Protocol {
	deps = deps.WithDefaults()
	p := &Plugin{deps: deps, timeout: 3 * time.Second}
	p.Init(deps, 30*time.Second)
	return p
}

// Plugin is one panel, slot 0.
type Plugin struct {
	dhsc.Base

	deps    plugin.Deps
	timeout time.Duration

	mu       sync.Mutex
	s        *session
	host     string
	port     int
	poll     codec.PollReply
	firmware string
	buttons  int
	labels   map[int]string
	colours  map[int]byte
	subs     []sub
	onFrame  []func(codec.Frame)
}

type sub struct {
	prefix string
	fn     dhsc.EventFunc
}

// buttonCount per model: the panels' own button IDs (§1.4.0); the rotary
// of the CCP-1601 reports as ID 17 (measured 2026-10-25).
var buttonCount = map[string]int{"CCP-1601": 16, "CCP-1601B": 16, "CCP-3201": 32, "CCP-3201B": 32,
	"CCP-4801": 48, "CCP-4801B": 48, "CCP-4200B": 42, "CCP-4800B": 48, "CCP-4800": 48,
	"CCP-2410": 24, "CCP-6002B": 60, "CCP-8002B": 80}

// Connect opens the TCP session, polls the panel and reads its firmware.
func (p *Plugin) Connect(ctx context.Context, ip string, port int) error {
	if port <= 0 {
		port = codec.DefaultPort
	}
	conn, err := p.deps.Net.Dial(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("ccp: %s:%d: %w", ip, port, err)
	}
	s := newSession(conn, p.Metrics(), p.dispatch, func(err error) {
		p.ComplianceProfile().Note("ccp_unknown_command_byte")
		p.deps.Logger.Warn("ccp: undecodable bytes dropped", "err", err)
	})
	p.mu.Lock()
	old := p.s
	p.s, p.host, p.port = s, ip, port
	if p.labels == nil {
		p.labels, p.colours = map[int]string{}, map[int]byte{}
	}
	p.mu.Unlock()
	if old != nil {
		old.close(fmt.Errorf("replaced"))
	}
	pr, err := p.Poll(ctx)
	if err != nil {
		s.close(err)
		return err
	}
	fw := ""
	if f, err := p.do(ctx, codec.ReportFirmware()); err == nil {
		fw = codec.Firmware(f)
	}
	p.mu.Lock()
	p.poll, p.firmware = pr, fw
	p.buttons = buttonCount[pr.Model]
	p.mu.Unlock()
	p.Opened("tcp", ip, port, dhsc.MetricsTimes{C: p.Metrics()})
	return nil
}

// Disconnect closes the session; within ~5 s the panel shows its off-line
// screen unless another client (Cerebrum) is connected (§1.0.0).
func (p *Plugin) Disconnect() error {
	p.mu.Lock()
	s := p.s
	p.s = nil
	p.mu.Unlock()
	if s != nil {
		s.close(fmt.Errorf("disconnect"))
	}
	p.Closed()
	return nil
}

// Done is closed when the session ends (panel closed it, network loss).
func (p *Plugin) Done() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.s == nil {
		c := make(chan struct{})
		close(c)
		return c
	}
	return p.s.done
}

func (p *Plugin) do(ctx context.Context, cmd []byte) (codec.Frame, error) {
	p.mu.Lock()
	s := p.s
	p.mu.Unlock()
	if s == nil {
		return codec.Frame{}, fmt.Errorf("ccp: not connected")
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.timeout)
		defer cancel()
	}
	return s.do(ctx, cmd)
}

// Send sends any client command and returns its reply.
func (p *Plugin) Send(ctx context.Context, cmd []byte) (codec.Frame, error) { return p.do(ctx, cmd) }

// Poll asks the panel's status (§1.2.1).
func (p *Plugin) Poll(ctx context.Context) (codec.PollReply, error) {
	f, err := p.do(ctx, codec.Poll())
	if err != nil {
		return codec.PollReply{}, err
	}
	pr, err := codec.ParsePoll(f)
	if err == nil {
		p.mu.Lock()
		p.poll = pr
		p.mu.Unlock()
	}
	return pr, err
}

// Status returns the last poll reply and the firmware text.
func (p *Plugin) Status() (codec.PollReply, string, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.poll, p.firmware, p.buttons
}

// SetLabel draws text on a button (rendered to a 64×32 bitmap).
func (p *Plugin) SetLabel(ctx context.Context, id int, text string) error {
	return p.SetLabelOpts(ctx, id, text, false)
}

// SetLabelOpts draws text on a button, with a 1-pixel frame when frame is set.
func (p *Plugin) SetLabelOpts(ctx context.Context, id int, text string, frame bool) error {
	return p.SetLabelScaled(ctx, id, text, frame, 0)
}

// SetLabelScaled draws text at a fixed scale (1 or 2; 0 = largest that fits).
func (p *Plugin) SetLabelScaled(ctx context.Context, id int, text string, frame bool, scale int) error {
	b := codec.TextScaled(text, frame, scale)
	if _, err := p.do(ctx, codec.WriteBitmap(byte(id), &b)); err != nil {
		return err
	}
	p.mu.Lock()
	p.labels[id] = text
	p.mu.Unlock()
	return nil
}

// SetColour sets a button's colour index (§1.2.5).
func (p *Plugin) SetColour(ctx context.Context, id int, colour byte) error {
	if _, err := p.do(ctx, codec.SetColour(byte(id), colour)); err != nil {
		return err
	}
	p.mu.Lock()
	p.colours[id] = colour
	p.mu.Unlock()
	return nil
}

// OnFrame registers a callback for every unsolicited frame.
func (p *Plugin) OnFrame(fn func(codec.Frame)) {
	p.mu.Lock()
	p.onFrame = append(p.onFrame, fn)
	p.mu.Unlock()
}

// ParseColour accepts a colour name, a decimal or a 0x hex index.
func ParseColour(s string) (byte, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if c, ok := codec.NamedColours[s]; ok {
		return c, nil
	}
	n, err := strconv.ParseUint(s, 0, 8)
	if err != nil || n > 0x3F {
		return 0, fmt.Errorf("ccp: colour %q: a name (%s) or an index 0..0x3f", s, colourNames())
	}
	return byte(n), nil
}

func colourNames() string {
	var n []string
	for k := range codec.NamedColours {
		n = append(n, k)
	}
	sortStrings(n)
	return strings.Join(n, ", ")
}

func sortStrings(a []string) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

func (p *Plugin) dispatch(f codec.Frame) {
	p.mu.Lock()
	subs := append([]sub(nil), p.subs...)
	hooks := make([]func(codec.Frame), len(p.onFrame))
	copy(hooks, p.onFrame)
	p.mu.Unlock()
	for _, h := range hooks {
		h(f)
	}
	ev, ok := eventOf(f)
	if !ok {
		return
	}
	for _, s := range subs {
		if s.prefix == "" || strings.HasPrefix(ev.Path, s.prefix) {
			s.fn(ev)
		}
	}
}

func eventOf(f codec.Frame) (dhsc.Event, bool) {
	now := time.Now()
	switch f.Cmd {
	case codec.RspButton:
		b, err := codec.ParseButton(f)
		if err != nil {
			return dhsc.Event{}, false
		}
		v := b.ActionName()
		switch b.Action {
		case codec.ActionIncrement:
			v = "+" + strconv.Itoa(int(b.Value))
		case codec.ActionDecrement:
			v = "-" + strconv.Itoa(int(b.Value))
		case codec.ActionTBar:
			v = "t-bar " + strconv.Itoa(int(b.Value))
		}
		path := fmt.Sprintf("buttons.%d", b.ID)
		return dhsc.Event{Path: path, Group: "buttons", ID: int(b.ID), Label: strconv.Itoa(int(b.ID)),
			Value: dhsc.Value{Kind: dhsc.KindString, Str: v, Raw: []byte(v)}, Timestamp: now, Access: 1}, true
	case codec.RspGPI:
		return dhsc.Event{Path: "gpi", Label: "gpi", Value: dhsc.Value{Kind: dhsc.KindString, Str: fmt.Sprintf("% x", f.Data[1:])}, Timestamp: now, Access: 1}, true
	case codec.RspError:
		pe, _ := codec.ParseError(f)
		return dhsc.Event{Path: "status.error", Label: "error", Value: dhsc.Value{Kind: dhsc.KindString, Str: pe.Error()}, Timestamp: now, Access: 1}, true
	}
	return dhsc.Event{}, false
}

// ---- consumer.Protocol ---------------------------------------------------

// GetDeviceInfo: one panel, one slot.
func (p *Plugin) GetDeviceInfo(ctx context.Context) (dhsc.DeviceInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.s == nil {
		return dhsc.DeviceInfo{}, fmt.Errorf("ccp: not connected")
	}
	return dhsc.DeviceInfo{IP: p.host, Port: p.port, NumSlots: 1, ProtocolVersion: int(p.poll.ProtocolVersion)}, nil
}

// GetSlotInfo reports the poll reply as identity.
func (p *Plugin) GetSlotInfo(ctx context.Context, slot int) (dhsc.SlotInfo, error) {
	if slot > 0 {
		return dhsc.SlotInfo{}, fmt.Errorf("ccp: a panel has slot 0 only")
	}
	pr, fw, n := p.Status()
	return dhsc.SlotInfo{Slot: 0, Status: dhsc.SlotPresent, State: dhsc.SlotStatePresent, IsOnline: true,
		Identity: map[string]string{"model": pr.Model, "protocol": strconv.Itoa(int(pr.ProtocolVersion)),
			"firmware": fw, "buttons": strconv.Itoa(n)}}, nil
}

// PathNative: every path is answered from the panel, no walk needed.
func (p *Plugin) PathNative() bool { return true }

// Walk lists the panel's status, network settings and one label + colour
// per button. A panel cannot report what it shows, so button values are
// the last written by this session.
func (p *Plugin) Walk(ctx context.Context, slot int) ([]dhsc.Object, error) {
	var out []dhsc.Object
	add := func(path, unit string, access uint8, kind dhsc.ValueKind) {
		parts := strings.Split(path, ".")
		v, _ := p.GetValue(ctx, dhsc.ValueRequest{Path: path})
		out = append(out, dhsc.Object{Path: parts, Group: parts[0], Label: parts[len(parts)-1], ID: len(out),
			Kind: kind, Access: access, Unit: unit, Value: v})
	}
	for _, s := range []string{"status.model", "status.protocol", "status.firmware", "status.psu_a", "status.psu_b",
		"status.refresh_required", "network.dhcp", "network.subnet", "network.gateway"} {
		add(s, "", 1, dhsc.KindString)
	}
	_, _, n := p.Status()
	for i := 1; i <= n; i++ {
		add(fmt.Sprintf("buttons.%d.label", i), "", 2, dhsc.KindString)
		add(fmt.Sprintf("buttons.%d.colour", i), "", 2, dhsc.KindString)
	}
	return out, nil
}

// GetValue answers status.* (fresh poll), network.* (read commands) and
// buttons.N.* (last written).
func (p *Plugin) GetValue(ctx context.Context, req dhsc.ValueRequest) (dhsc.Value, error) {
	str := func(s string) dhsc.Value { return dhsc.Value{Kind: dhsc.KindString, Str: s, Raw: []byte(s)} }
	onoff := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	path := strings.Trim(req.Path, ".")
	if path == "" {
		path = req.Label
	}
	switch {
	case strings.HasPrefix(path, "status."):
		pr, err := p.Poll(ctx)
		if err != nil {
			return dhsc.Value{}, err
		}
		_, fw, _ := p.Status()
		switch path {
		case "status.model":
			return str(pr.Model), nil
		case "status.protocol":
			return str(strconv.Itoa(int(pr.ProtocolVersion))), nil
		case "status.firmware":
			return str(fw), nil
		case "status.psu_a":
			return str(onoff(pr.PSUA)), nil
		case "status.psu_b":
			return str(onoff(pr.PSUB)), nil
		case "status.refresh_required":
			return str(onoff(pr.RefreshRequired)), nil
		}
	case path == "network.dhcp":
		f, err := p.do(ctx, codec.ReadDHCP())
		if err != nil {
			return dhsc.Value{}, err
		}
		return str(onoff(len(f.Data) == 1 && f.Data[0] == 1)), nil
	case path == "network.subnet":
		f, err := p.do(ctx, codec.ReadSubnet())
		if err != nil {
			return dhsc.Value{}, err
		}
		return str(codec.IPv4(f.Data)), nil
	case path == "network.gateway":
		f, err := p.do(ctx, codec.ReadGateway())
		if err != nil {
			return dhsc.Value{}, err
		}
		return str(codec.IPv4(f.Data)), nil
	case strings.HasPrefix(path, "buttons."):
		id, field, err := buttonPath(path)
		if err != nil {
			return dhsc.Value{}, err
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if field == "label" {
			return str(p.labels[id]), nil
		}
		if c, ok := p.colours[id]; ok {
			return str(fmt.Sprintf("0x%02x", c)), nil
		}
		return str(""), nil
	}
	return dhsc.Value{}, fmt.Errorf("%w: ccp: %q (status.*, network.*, buttons.N.label|colour)", dhsc.ErrUnknownLabel, path)
}

// SetValue writes buttons.N.label (text → bitmap) and buttons.N.colour.
func (p *Plugin) SetValue(ctx context.Context, req dhsc.ValueRequest, val dhsc.Value) (dhsc.Value, error) {
	id, field, err := buttonPath(strings.Trim(req.Path, "."))
	if err != nil {
		return dhsc.Value{}, err
	}
	text := val.Str
	if text == "" {
		text = string(val.Raw)
	}
	if text == "" && val.Kind == dhsc.KindInt {
		text = strconv.FormatInt(val.Int, 10)
	}
	switch field {
	case "label":
		err = p.SetLabel(ctx, id, text)
	case "colour", "color":
		var c byte
		if c, err = ParseColour(text); err == nil {
			err = p.SetColour(ctx, id, c)
		}
	default:
		err = fmt.Errorf("ccp: buttons.%d.%s is not writable (label, colour)", id, field)
	}
	if err != nil {
		return dhsc.Value{}, err
	}
	return dhsc.Value{Kind: dhsc.KindString, Str: text, Raw: []byte(text)}, nil
}

func buttonPath(path string) (int, string, error) {
	parts := strings.Split(path, ".")
	if len(parts) != 3 || parts[0] != "buttons" {
		return 0, "", fmt.Errorf("ccp: %q: expected buttons.N.label or buttons.N.colour", path)
	}
	id, err := strconv.Atoi(parts[1])
	if err != nil || id < 0 || id > 255 {
		return 0, "", fmt.Errorf("ccp: %q: button id 0..255 (0 = all)", path)
	}
	return id, parts[2], nil
}

// Subscribe reports button, rotary, T-bar, GPI and error frames whose
// path starts with req.Path ("" = all, "buttons.5" = one button).
func (p *Plugin) Subscribe(req dhsc.ValueRequest, fn dhsc.EventFunc) error {
	p.mu.Lock()
	p.subs = append(p.subs, sub{prefix: strings.Trim(req.Path, "."), fn: fn})
	p.mu.Unlock()
	return nil
}

// Unsubscribe removes the subscriptions on req.Path.
func (p *Plugin) Unsubscribe(req dhsc.ValueRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	pre := strings.Trim(req.Path, ".")
	kept := p.subs[:0]
	for _, s := range p.subs {
		if s.prefix != pre {
			kept = append(kept, s)
		}
	}
	p.subs = kept
	return nil
}
