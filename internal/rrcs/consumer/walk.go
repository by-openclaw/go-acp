package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	dhsc "dhs/internal/consumer"
)

// Access bits of an object, as the other connectors set them.
const (
	accessRead  uint8 = 1
	accessWrite uint8 = 2
)

// slotLists are the three requests a walk of one slot is made of: the
// nodes and the cards to know which node number the ports of a bay
// carry, and the ports.
var slotLists = []string{"GetAllNodes", "GetAllClientCards", "GetAllPorts"}

// slotZeroLists are the requests a walk of slot 0 is made of: the node
// and everything that belongs to no card. The ports are read too, so a
// member of a conference, group or IFB is named by the path and label
// of its port.
var slotZeroLists = []string{
	"GetAllNodes", "GetAllPorts", "GetAllConferences", "GetAllGroups",
	"GetAllIFBs", "GetAllLogicSources_v2",
}

// Walk reads one slot, each property as one object.
//
// Slot 0 is the node and what belongs to no card: conferences, groups,
// IFBs, logic sources (six requests). A bay gives its client card and
// the ports of that card (three requests), whatever the size of the
// system. The keys of the panels are not read here: they cost one
// request per panel and are asked for on their own.
func (p *Plugin) Walk(ctx context.Context, slot int) ([]dhsc.Object, error) {
	p.mu.Lock()
	client := p.client
	p.mu.Unlock()
	if client == nil {
		return nil, dhsc.ErrNotConnected
	}
	if slot == 0 {
		snap, _, err := Collect(ctx, client, CollectOpts{Only: slotZeroLists})
		if err != nil {
			return nil, err
		}
		m, err := modelOf(snap)
		if err != nil {
			return nil, err
		}
		target := hostOnly(m.Target)
		rows := append(nodeRows(target, m), objectRows(target, m)...)
		out := make([]dhsc.Object, 0, len(rows))
		for _, r := range rows {
			out = append(out, objectOf(0, r))
		}
		return out, nil
	}

	snap, _, err := Collect(ctx, client, CollectOpts{Only: slotLists})
	if err != nil {
		return nil, err
	}
	m, err := modelOf(snap)
	if err != nil {
		return nil, err
	}

	var card *Card
	for _, c := range m.Cards {
		if c.Bay == slot {
			if card != nil {
				// Two nodes with a card in this bay: which one a slot
				// names is not defined (docs/mapping.md §8).
				return nil, fmt.Errorf("rrcs: bay %d holds a card on several nodes", slot)
			}
			card = c
		}
	}
	if card == nil {
		return nil, nil
	}

	target := hostOnly(m.Target)
	rows := RowsOf(target, "client-card", card.Path, card.ObjectID, card.Raw)
	for node, c := range m.CardOfNode() {
		if c != card {
			continue
		}
		for _, port := range m.Ports {
			if port.Node == node {
				rows = append(rows, RowsOf(target, "portex", port.Path, port.ObjectID, port.Raw)...)
			}
		}
	}

	out := make([]dhsc.Object, 0, len(rows))
	for _, r := range rows {
		out = append(out, objectOf(slot, r))
	}
	return out, nil
}

// modelOf builds the model of a snapshot the way a snapshot file is
// read: through its JSON, so a live gateway and a file give the same.
func modelOf(snap *Snapshot) (*Model, error) {
	doc, err := json.Marshal(snap)
	if err != nil {
		return nil, err
	}
	var tree map[string]any
	if err := json.Unmarshal(doc, &tree); err != nil {
		return nil, err
	}
	return BuildModel(tree), nil
}

// objectOf turns a row into the object of the shared contract. The path
// is the address RRCS uses; the OID is the ObjectID RRCS gave the thing
// the property belongs to.
func objectOf(slot int, r Row) dhsc.Object {
	o := dhsc.Object{
		Slot:   slot,
		Path:   strings.Split(r.Path, "."),
		OID:    r.ID,
		Label:  r.Label,
		Unit:   r.Unit,
		Access: accessRead,
		Min:    numberOr(r.Min),
		Max:    numberOr(r.Max),
		Def:    numberOr(r.Default),
	}
	if id, err := strconv.Atoi(r.ID); err == nil {
		o.ID = id
	}
	if strings.Contains(r.Access, "W") {
		o.Access |= accessWrite
	}
	if r.EnumItems != "" {
		o.EnumItems = strings.Split(r.EnumItems, "|")
	}
	if n, err := strconv.Atoi(r.MaxLen); err == nil {
		o.MaxLen = n
	}
	switch r.Kind {
	case "bool":
		o.Kind = dhsc.KindBool
		o.Value = dhsc.Value{Kind: dhsc.KindBool, Bool: r.Value == "true"}
	case "int", "enum":
		// An enum of RRCS is a number with names; the number is kept as
		// it is, since it does not have to start at 0 nor fit a byte.
		o.Kind = dhsc.KindInt
		n, _ := strconv.ParseInt(r.Value, 10, 64)
		o.Value = dhsc.Value{Kind: dhsc.KindInt, Int: n}
	default:
		o.Kind = dhsc.KindString
		o.Value = dhsc.Value{Kind: dhsc.KindString, Str: r.Value}
	}
	return o
}

// numberOr gives a printed bound as a number where it is one, as text
// where it is not, and nil where there is none.
func numberOr(s string) any {
	if s == "" {
		return nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	return s
}
