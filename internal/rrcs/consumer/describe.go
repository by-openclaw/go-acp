package consumer

import (
	"context"
	"strconv"
	"strings"

	dhsc "dhs/internal/consumer"
)

// Describe answers the schema of one point without a read
// (consumer.Describer, ADR-0037 P4): what the specification's tables and
// the hand-kept ranges say about the property — the same metadata the
// export rows carry — and where the thing sits in the manifest extract
// wrote, so the command line completes the model and the template from
// it. RRCS describes nothing over the wire, so this costs no request.
func (p *Plugin) Describe(_ context.Context, req dhsc.ValueRequest) (dhsc.Object, dhsc.DMRef, error) {
	object, block, field, ok := SplitPath(req.Path)
	if !ok {
		return dhsc.Object{}, dhsc.DMRef{}, &dhsc.ValidationError{Field: req.Path, Reason: "does not name an object and a property"}
	}
	kind, _, _ := strings.Cut(object, ".")
	isCard := strings.Contains(object, ".card.")
	node, port, _, isPort := portOfPath(object)
	specObject := kind
	switch {
	case isCard:
		specObject = "client-card"
	case isPort:
		specObject = "portex"
	}
	// A zero value of the property's type is what the row builder needs
	// to type the property as the export does.
	var zero any
	switch SpecFor(specObject, block, field).Type {
	case "bool":
		zero = false
	case "int":
		zero = float64(0)
	default:
		zero = ""
	}
	props := map[string]any{field: zero}
	if block != "" {
		props = map[string]any{block: map[string]any{field: zero}}
	}
	rows := RowsOf("", specObject, object, 0, props)
	if len(rows) != 1 {
		return dhsc.Object{}, dhsc.DMRef{}, &dhsc.ValidationError{Field: req.Path, Reason: "is not a property the tables describe"}
	}
	o := objectOf(req.Slot, rows[0])
	o.Value = dhsc.Value{}
	// What a method of its own writes (§8.3 to §8.7) is writable too.
	if (isPort && block == "" && (field == "Label" || field == "Alias" || field == "InputGain" || field == "OutputGain")) ||
		(kind == "logic" && block == "" && field == "State") {
		o.Access |= accessWrite
	}

	p.mu.Lock()
	version, cards, cardsSet := p.version, p.cards, p.cardsSet
	p.mu.Unlock()
	ref := dhsc.DMRef{SwRev: version}
	prop := strings.TrimPrefix(block+"."+field, ".")
	switch {
	case isPort:
		ref.Addr = map[string]any{"node": node, "port": port}
		ref.Path = prop
		if half := object[strings.LastIndex(object, ".")+1:]; half == "in" || half == "out" {
			ref.Path = half + "." + prop
		}
	case isCard:
		parts := strings.Split(object, ".")
		bay, _ := strconv.Atoi(parts[len(parts)-1])
		cardNode, _ := strconv.Atoi(parts[3])
		ref.Addr = map[string]any{"card_node": cardNode, "bay": bay}
		ref.Path = prop
		if cardsSet {
			for _, c := range cards {
				if c.Bay == bay && c.Node == cardNode {
					ref.Model = c.Type
				}
			}
		}
	default:
		// The node, and the objects that belong to no card: their
		// templates live in the node's model.
		ref.Addr = map[string]any{"bay": 0}
		ref.Template, ref.Path = kind, prop
		if kind == "net" {
			ref.Template, ref.Path = "", prop
		}
	}
	return o, ref, nil
}
