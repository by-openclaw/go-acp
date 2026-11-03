package consumer

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	dhsc "dhs/internal/consumer"
	"dhs/internal/plugin"
	"dhs/internal/rrcs/codec"
	"dhs/internal/transport"
	"dhs/internal/wiretrace"
)

// The client in this package is RRCS's own shape: one method, one
// answer. This file is the other face: the same client behind the
// neutral consumer.Protocol, so an RRCS gateway answers like every other
// device here. The mapping is docs/mapping.md of this connector.
//
// This file holds the session, the device and its slots. Walk is in
// walk.go, GetValue and SetValue in value.go (the writes in the forms
// ADR-0035 proved, through change.go), Subscribe in subscribe.go. The
// generic verbs of the command line reach the gateway through this
// plugin (ADR-0037); the rrcs-own verbs that remain are listed there.

// Name is the registry name: dhs consumer rrcs <verb>.
const Name = "rrcs"

// staleAfter is how long a session with no traffic is treated as alive.
const staleAfter = 30 * time.Second

func init() {
	dhsc.Register(&Factory{})
}

// Factory builds Plugin instances.
type Factory struct{}

// Meta describes the plugin to the registry.
func (f *Factory) Meta() dhsc.ProtocolMeta {
	return dhsc.ProtocolMeta{
		Name:        Name,
		DefaultPort: DefaultPort,
		Description: "Riedel RRCS (XML-RPC over HTTP) — Artist intercom: ports, panels, keys, crosspoints",
	}
}

// New builds a plugin from the injected dependency set.
func (f *Factory) New(deps plugin.Deps) dhsc.Protocol {
	deps = deps.WithDefaults()
	p := &Plugin{deps: deps}
	p.Init(deps, staleAfter)
	return p
}

// Plugin is one RRCS gateway with its Artist node. The node is slot 0;
// bay N of the node is slot N.
type Plugin struct {
	dhsc.Base

	deps plugin.Deps

	mu      sync.Mutex
	client  *Client
	host    string
	port    int
	version string
	// cards is read once per session, by the first call that needs it.
	cards    []Card
	cardsSet bool

	// events holds the subscriptions and the listener they feed from.
	events subscriptions

	// recorder, when set before Connect, receives every XML document
	// sent and received (ADR-0028), as the rrcs-own verbs record them.
	recorder *transport.Recorder

	// node is the Artist node whose bays are the slots, when the
	// gateway fronts several nodes (ADR-0037 P9); 0 = the only node.
	node int
}

// SelectNode names the node whose bays are the slots of this session
// (--node N, ADR-0037 P9). A gateway that fronts an Artist ring of
// several nodes refuses to number bays until one is named.
// Zero means the only node.
func (p *Plugin) SelectNode(node int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.node = node
}

// nodeCards are the client cards of the node this session is about:
// the selected one, or the only one. With several nodes and none
// selected the bays cannot be numbered, and the refusal lists them.
func (p *Plugin) nodeCards(cards []Card) ([]Card, error) {
	p.mu.Lock()
	node := p.node
	p.mu.Unlock()
	if node != 0 {
		var out []Card
		for _, c := range cards {
			if c.Node == node {
				out = append(out, c)
			}
		}
		if len(out) == 0 && len(cards) > 0 {
			return nil, fmt.Errorf("rrcs: no client card on node %d; the gateway fronts %s", node, nodeList(cards))
		}
		return out, nil
	}
	if !oneNode(cards) {
		return nil, fmt.Errorf("rrcs: the gateway fronts several nodes (%s): name one with --node", nodeList(cards))
	}
	return cards, nil
}

// nodeList prints the distinct node numbers of a card list, in order.
func nodeList(cards []Card) string {
	var nodes []string
	seen := map[int]bool{}
	for _, c := range cards {
		if !seen[c.Node] {
			seen[c.Node] = true
			nodes = append(nodes, strconv.Itoa(c.Node))
		}
	}
	return strings.Join(nodes, ", ")
}

// SetRecorder attaches a capture to the session Connect opens. It is the
// hook the generic verbs look for when --capture was given; it must be
// set before Connect, so the capture starts with the first request.
func (p *Plugin) SetRecorder(rec *transport.Recorder) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.recorder = rec
}

// PathNative says that a path is the gateway's own address of a thing
// (net.1.node.61.port.1024.in.PortAes67Input.Channels, conference.555.Label):
// GetValue and SetValue resolve it on the device, no walk is needed
// (consumer.PathNative, ADR-0037 P2).
func (p *Plugin) PathNative() bool { return true }

// Connect opens the session and asks the gateway its version: one
// request. Nothing else is read until a verb needs it.
func (p *Plugin) Connect(ctx context.Context, ip string, port int) error {
	if port <= 0 {
		port = DefaultPort
	}
	cfg := Config{Addr: net.JoinHostPort(ip, strconv.Itoa(port))}
	p.mu.Lock()
	rec := p.recorder
	p.mu.Unlock()
	if rec != nil {
		cfg.Tap = func(dir wiretrace.Direction, _ string, doc []byte) { rec.Record(Name, string(dir), doc) }
	}
	client, err := NewClient(cfg)
	if err != nil {
		return err
	}
	reply, err := client.Call(ctx, "GetVersion")
	if err != nil {
		return fmt.Errorf("rrcs: %s: %w", ip, err)
	}
	version := firstString(reply.Payload())

	p.mu.Lock()
	p.client, p.host, p.port, p.version = client, ip, port, version
	p.cards, p.cardsSet = nil, false
	p.mu.Unlock()

	p.Opened("tcp", ip, port, dhsc.MetricsTimes{C: p.Metrics()})
	return nil
}

// Disconnect drops the client. RRCS keeps no session for requests, so
// there is nothing to tell the gateway.
func (p *Plugin) Disconnect() error {
	p.mu.Lock()
	p.client = nil
	p.cards, p.cardsSet = nil, false
	p.mu.Unlock()
	p.Closed()
	return nil
}

// GetDeviceInfo answers with the version read at Connect and the number
// of slots: the node and its bays up to the last one that holds a card.
func (p *Plugin) GetDeviceInfo(ctx context.Context) (dhsc.DeviceInfo, error) {
	all, err := p.clientCards(ctx)
	if err != nil {
		return dhsc.DeviceInfo{}, err
	}
	cards, err := p.nodeCards(all)
	if err != nil {
		return dhsc.DeviceInfo{}, err
	}
	p.mu.Lock()
	host, port, version := p.host, p.port, p.version
	p.mu.Unlock()

	last := 0
	for _, c := range cards {
		if c.Bay > last {
			last = c.Bay
		}
	}
	return dhsc.DeviceInfo{IP: host, Port: port, NumSlots: last + 1, DtdVersion: version}, nil
}

// GetSlotInfo answers for the node (slot 0) or one bay. A bay with no
// client card configured is no_card.
//
// The bays are those of the selected node, or of the only node; with
// several nodes and none selected the numbering is not defined and
// the call refuses, naming the nodes (ADR-0037 P9).
func (p *Plugin) GetSlotInfo(ctx context.Context, slot int) (dhsc.SlotInfo, error) {
	all, err := p.clientCards(ctx)
	if err != nil {
		return dhsc.SlotInfo{}, err
	}
	cards, err := p.nodeCards(all)
	if err != nil {
		return dhsc.SlotInfo{}, err
	}
	present := dhsc.SlotInfo{Slot: slot, Status: dhsc.SlotPresent, State: dhsc.SlotStatePresent, IsOnline: true}
	if slot == 0 {
		return present, nil
	}
	for _, c := range cards {
		if c.Bay == slot {
			return present, nil
		}
	}
	return dhsc.SlotInfo{Slot: slot, Status: dhsc.SlotNoCard, State: dhsc.SlotStateNoCard}, nil
}

// WriteGuardReason declares that every write through this plugin must be
// confirmed with --write-to (consumer.WriteGuard, ADR-0037 P5).
func (p *Plugin) WriteGuardReason() string {
	return "a configuration write stopped a production RRCS 9.0 on 2026-10-06, ADR-0035; this connector names its target twice"
}

// Cards returns the client cards of the gateway: one request the first
// time in a session, none after.
func (p *Plugin) Cards(ctx context.Context) ([]Card, error) {
	return p.clientCards(ctx)
}

func (p *Plugin) clientCards(ctx context.Context) ([]Card, error) {
	p.mu.Lock()
	client, cards, set := p.client, p.cards, p.cardsSet
	p.mu.Unlock()
	if client == nil {
		return nil, dhsc.ErrNotConnected
	}
	if set {
		return cards, nil
	}
	reply, err := client.Call(ctx, "GetAllClientCards")
	if err != nil {
		return nil, fmt.Errorf("rrcs: GetAllClientCards: %w", err)
	}
	cards = parseCards(reply.Payload())

	p.mu.Lock()
	if p.client == client {
		p.cards, p.cardsSet = cards, true
	}
	p.mu.Unlock()
	return cards, nil
}

// parseCards reads the "[list]" a GetAllClientCards answer holds beside
// its key.
func parseCards(payload codec.Value) []Card {
	if payload.Kind != codec.KindArray || len(payload.Items) != 1 || payload.Items[0].Kind != codec.KindArray {
		return nil
	}
	var out []Card
	for _, v := range payload.Items[0].Items {
		if v.Kind != codec.KindStruct {
			continue
		}
		out = append(out, Card{
			Node:     structInt(v, "Node"),
			Bay:      structInt(v, "Bay"),
			Type:     structString(v, "ClientCardTypeString"),
			LongName: structString(v, "LongName"),
			ObjectID: structInt(v, "ObjectID"),
		})
	}
	return out
}

// oneNode reports whether every card belongs to the same node.
func oneNode(cards []Card) bool {
	for _, c := range cards {
		if c.Node != cards[0].Node {
			return false
		}
	}
	return true
}

func structInt(v codec.Value, name string) int {
	f, ok := v.Field(name)
	if !ok || f.Kind != codec.KindInt {
		return 0
	}
	return int(f.Int)
}

func structString(v codec.Value, name string) string {
	f, ok := v.Field(name)
	if !ok || f.Kind != codec.KindString {
		return ""
	}
	return f.Str
}

// firstString is the one string an answer like GetVersion holds.
func firstString(payload codec.Value) string {
	if payload.Kind == codec.KindString {
		return payload.Str
	}
	if payload.Kind == codec.KindArray && len(payload.Items) > 0 && payload.Items[0].Kind == codec.KindString {
		return payload.Items[0].Str
	}
	return ""
}
