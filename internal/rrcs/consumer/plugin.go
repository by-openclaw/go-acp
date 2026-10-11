package consumer

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	dhsc "dhs/internal/consumer"
	"dhs/internal/plugin"
	"dhs/internal/rrcs/codec"
)

// The client in this package is RRCS's own shape: one method, one
// answer. This file is the other face: the same client behind the
// neutral consumer.Protocol, so an RRCS gateway answers like every other
// device here. The mapping is docs/mapping.md of this connector.
//
// What is here is the first part: the session, the device and its slots.
// Walk, GetValue, SetValue and Subscribe answer ErrNotImplemented until
// their logic has moved out of cmd/dhs; the rrcs verbs of the command
// line do not go through this file yet.

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
}

// Connect opens the session and asks the gateway its version: one
// request. Nothing else is read until a verb needs it.
func (p *Plugin) Connect(ctx context.Context, ip string, port int) error {
	if port <= 0 {
		port = DefaultPort
	}
	client, err := NewClient(Config{Addr: net.JoinHostPort(ip, strconv.Itoa(port))})
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
	cards, err := p.clientCards(ctx)
	if err != nil {
		return dhsc.DeviceInfo{}, err
	}
	p.mu.Lock()
	host, port, version := p.host, p.port, p.version
	p.mu.Unlock()

	last := 0
	if oneNode(cards) {
		for _, c := range cards {
			if c.Bay > last {
				last = c.Bay
			}
		}
	}
	return dhsc.DeviceInfo{IP: host, Port: port, NumSlots: last + 1, DtdVersion: version}, nil
}

// GetSlotInfo answers for the node (slot 0) or one bay. A bay with no
// client card configured is no_card.
//
// With client cards of several nodes behind one gateway the numbering of
// bays as slots is not defined (docs/mapping.md §8), so only slot 0
// answers present.
func (p *Plugin) GetSlotInfo(ctx context.Context, slot int) (dhsc.SlotInfo, error) {
	cards, err := p.clientCards(ctx)
	if err != nil {
		return dhsc.SlotInfo{}, err
	}
	present := dhsc.SlotInfo{Slot: slot, Status: dhsc.SlotPresent, State: dhsc.SlotStatePresent, IsOnline: true}
	if slot == 0 {
		return present, nil
	}
	if oneNode(cards) {
		for _, c := range cards {
			if c.Bay == slot {
				return present, nil
			}
		}
	}
	return dhsc.SlotInfo{Slot: slot, Status: dhsc.SlotNoCard, State: dhsc.SlotStateNoCard}, nil
}

// Walk is not moved into the library yet.
func (p *Plugin) Walk(ctx context.Context, slot int) ([]dhsc.Object, error) {
	return nil, dhsc.ErrNotImplemented
}

// GetValue is not moved into the library yet.
func (p *Plugin) GetValue(ctx context.Context, req dhsc.ValueRequest) (dhsc.Value, error) {
	return dhsc.Value{}, dhsc.ErrNotImplemented
}

// SetValue is not moved into the library yet.
func (p *Plugin) SetValue(ctx context.Context, req dhsc.ValueRequest, val dhsc.Value) (dhsc.Value, error) {
	return dhsc.Value{}, dhsc.ErrNotImplemented
}

// Subscribe is not moved into the library yet.
func (p *Plugin) Subscribe(req dhsc.ValueRequest, fn dhsc.EventFunc) error {
	return dhsc.ErrNotImplemented
}

// Unsubscribe is not moved into the library yet.
func (p *Plugin) Unsubscribe(req dhsc.ValueRequest) error {
	return dhsc.ErrNotImplemented
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
