package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"sort"
	"strconv"
	"strings"

	rrcs "dhs/internal/rrcs/consumer"
)

// ensure --file also takes the provisioning CSV of the stream blocks:
// one line per port, as an operator reads it.
//
//	card,port,rrcs_port,directions,role,protocol,channels,linked_to,linked_to_rrcs_port,channel
//	4,-7.41,1064,in+out,main,NMOS,8,,,1
//	4,-7.42,1065,in+out,linked,NMOS,,-7.41,1064,2
//
// card is the bay number of the client card; the tool reads the gateway
// once to learn which node its ports sit on. role is main or linked; a
// linked line names its main in linked_to_rrcs_port and its channel. The
// port, protocol and linked_to columns are for the reader and are not
// parsed. The directions and channels of the main line rule the block.

// rrcsStreamCSVRow is one parsed line.
type rrcsStreamCSVRow struct {
	Line       int
	Card       int
	Port       int
	Role       string
	Directions []string
	Channels   int
	LinkedTo   int
	Channel    int
}

// rrcsStreamRowsCSV reads the CSV by its header.
func rrcsStreamRowsCSV(raw []byte) ([]rrcsStreamCSVRow, error) {
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))))
	r.TrimLeadingSpace = true
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("want a header line and at least one port line")
	}
	col := map[string]int{}
	for i, name := range records[0] {
		col[strings.ToLower(strings.TrimSpace(name))] = i
	}
	for _, need := range []string{"card", "rrcs_port", "role", "channel"} {
		if _, ok := col[need]; !ok {
			return nil, fmt.Errorf("the header misses the column %q", need)
		}
	}
	field := func(rec []string, name string) string {
		i, ok := col[name]
		if !ok || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}
	number := func(line int, rec []string, name string, required bool) (int, error) {
		text := field(rec, name)
		if text == "" {
			if required {
				return 0, fmt.Errorf("line %d: the column %q is empty", line, name)
			}
			return 0, nil
		}
		n, err := strconv.Atoi(text)
		if err != nil {
			return 0, fmt.Errorf("line %d: %s %q is not a whole number", line, name, text)
		}
		return n, nil
	}
	var rows []rrcsStreamCSVRow
	for i, rec := range records[1:] {
		line := i + 2
		row := rrcsStreamCSVRow{Line: line, Role: strings.ToLower(field(rec, "role"))}
		var err error
		if row.Card, err = number(line, rec, "card", true); err != nil {
			return nil, err
		}
		if row.Port, err = number(line, rec, "rrcs_port", true); err != nil {
			return nil, err
		}
		if row.Channel, err = number(line, rec, "channel", true); err != nil {
			return nil, err
		}
		if row.Channels, err = number(line, rec, "channels", false); err != nil {
			return nil, err
		}
		if row.LinkedTo, err = number(line, rec, "linked_to_rrcs_port", false); err != nil {
			return nil, err
		}
		switch directions := field(rec, "directions"); directions {
		case "", "in+out", "out+in", "both":
			row.Directions = []string{"in", "out"}
		case "in", "out":
			row.Directions = []string{directions}
		default:
			return nil, fmt.Errorf("line %d: directions %q: want in, out or in+out", line, directions)
		}
		switch row.Role {
		case "main":
			if row.Channel != 1 {
				return nil, fmt.Errorf("line %d: a main port is channel 1", line)
			}
		case "linked":
			if row.LinkedTo == 0 {
				return nil, fmt.Errorf("line %d: a linked port names its main in linked_to_rrcs_port", line)
			}
			if row.Channel < 2 || row.Channel > 16 {
				return nil, fmt.Errorf("line %d: a linked port takes channel 2 to 16", line)
			}
		default:
			return nil, fmt.Errorf("line %d: role %q: want main or linked", line, row.Role)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// rrcsStreamsOfCSV turns the lines into stream blocks, reading the
// gateway once (three requests) to learn which node each card's ports
// sit on.
func rrcsStreamsOfCSV(ctx context.Context, cf *rrcsFlags, client *rrcs.Client, rows []rrcsStreamCSVRow) ([]rrcsDesiredStream, error) {
	snap, _, err := rrcsCollect(ctx, client, rrcsCollectOpts{Only: []string{"GetAllNodes", "GetAllClientCards", "GetAllPorts"}})
	if err != nil {
		return nil, err
	}
	m, err := rrcsModelOf(snap)
	if err != nil {
		return nil, err
	}
	nodeOfBay, netOfNode := map[int]int{}, map[int]int{}
	for node, card := range m.CardOfNode() {
		nodeOfBay[card.Bay] = node
	}
	for _, p := range m.Ports {
		netOfNode[p.Node] = p.Net
	}
	path := func(card, port int) (string, error) {
		node, ok := nodeOfBay[card]
		if !ok {
			return "", fmt.Errorf("the gateway lists no client card in bay %d", card)
		}
		net := netOfNode[node]
		if net == 0 {
			net = 1
		}
		return fmt.Sprintf("net.%d.node.%d.port.%d", net, node, port), nil
	}

	type key struct{ card, main int }
	mains := map[key]rrcsStreamCSVRow{}
	linked := map[key][]rrcsStreamCSVRow{}
	var order []key
	for _, row := range rows {
		switch row.Role {
		case "main":
			k := key{row.Card, row.Port}
			if _, dup := mains[k]; dup {
				return nil, fmt.Errorf("line %d: main port %d of card %d given twice", row.Line, row.Port, row.Card)
			}
			mains[k] = row
			order = append(order, k)
		case "linked":
			k := key{row.Card, row.LinkedTo}
			linked[k] = append(linked[k], row)
		}
	}
	for k := range linked {
		if _, ok := mains[k]; !ok {
			return nil, fmt.Errorf("card %d: linked ports name main port %d and no main line gives it", k.card, k.main)
		}
	}
	var streams []rrcsDesiredStream
	for _, k := range order {
		main := mains[k]
		ports := linked[k]
		if len(ports) == 0 {
			return nil, fmt.Errorf("line %d: main port %d of card %d has no linked line", main.Line, main.Port, main.Card)
		}
		sort.Slice(ports, func(i, j int) bool { return ports[i].Channel < ports[j].Channel })
		// An empty channels column stays 0: the linked ports plus one is
		// then the minimum and a larger main is left alone.
		st := rrcsDesiredStream{Directions: main.Directions, Channels: main.Channels}
		if st.Main, err = path(main.Card, main.Port); err != nil {
			return nil, err
		}
		for i, p := range ports {
			if p.Channel != i+2 {
				return nil, fmt.Errorf("line %d: card %d main %d: channels must run 2, 3, … without a gap; got channel %d in position %d", p.Line, p.Card, main.Port, p.Channel, i+2)
			}
			full, err := path(p.Card, p.Port)
			if err != nil {
				return nil, err
			}
			st.Linked = append(st.Linked, full)
		}
		if st.Channels != 0 && len(ports)+1 > st.Channels {
			return nil, fmt.Errorf("card %d main %d: %d linked ports do not fit %d channels", main.Card, main.Port, len(ports), st.Channels)
		}
		streams = append(streams, st)
	}
	cf.say("rrcs ensure: csv: %d stream block(s) on %d line(s)", len(streams), len(rows))
	return streams, nil
}
