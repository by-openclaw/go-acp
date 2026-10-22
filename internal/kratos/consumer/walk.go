package kratos

import (
	"context"
	"sort"
	"strings"

	dhsc "dhs/internal/consumer"
	"dhs/internal/kratos/codec"
)

// Walk reads the four tables of the client sequence (§2.5, §3.1) and
// joins them into one object per value:
//
//   - svtList: what each value is (name, type, range, units, scaling);
//   - cvtList: the current values, one row per instance of an array;
//   - enumerations: the display text of enumerated values;
//   - dependencies: UIDs with no meaning in this configuration (hidden).
//
// A value of the SVT never written to the CVT (volatility -1) is
// listed with no value: it is read from MQTT only (watch).
func (p *Plugin) Walk(ctx context.Context, slot int) ([]dhsc.Object, error) {
	r, err := p.session()
	if err != nil {
		return nil, err
	}
	var svt []codec.SVTRow
	var cvt []codec.CVTRow
	var enums []codec.Enumeration
	var deps []codec.Dependency
	for _, t := range []struct {
		query string
		dst   any
	}{
		{"consumer/svtList", &svt},
		{"consumer/cvtList", &cvt},
		{"consumer/enumerations", &enums},
		{"consumer/dependencies", &deps},
	} {
		env, err := r.get(ctx, t.query)
		if err != nil {
			return nil, err
		}
		if _, err := env.Rows(t.dst); err != nil {
			return nil, err
		}
	}
	objs := joinTables(svt, cvt, enums, deps)

	byUID := make(map[string]dhsc.Object, len(objs))
	root := ""
	for _, o := range objs {
		u := strings.Join(o.Path, "/")
		byUID[u] = o
		if root == "" && len(o.Path) > 0 {
			root = o.Path[0]
		}
	}
	p.mu.Lock()
	p.tree, p.byUID = objs, byUID
	if p.root == "" {
		p.root = root
	}
	p.mu.Unlock()
	return objs, nil
}

// joinTables builds the object list. One object per CVT row (array
// instances included) that the SVT describes, plus one per SVT value
// absent from the CVT; nodes and hidden UIDs are left out.
func joinTables(svt []codec.SVTRow, cvt []codec.CVTRow, enums []codec.Enumeration, deps []codec.Dependency) []dhsc.Object {
	hidden := map[string]bool{}
	for _, d := range deps {
		if d.Hidden() {
			hidden[d.UID] = true
		}
	}
	enumByUID := map[string]map[int64]string{}
	for _, e := range enums {
		n, ok := e.EnumIndex.Int()
		if !ok {
			continue
		}
		m := enumByUID[e.UID]
		if m == nil {
			m = map[int64]string{}
			enumByUID[e.UID] = m
		}
		m[n] = string(e.EnDisplayText)
	}
	exact := map[string]codec.SVTRow{}
	var arrays []codec.SVTRow
	for _, s := range svt {
		if s.IsNode() {
			continue
		}
		if s.IsArray() {
			arrays = append(arrays, s)
			continue
		}
		exact[s.UID] = s
	}
	describe := func(uid string) (codec.SVTRow, bool) {
		if s, ok := exact[uid]; ok {
			return s, true
		}
		for _, a := range arrays {
			if codec.MatchArray(a.UID, uid) {
				return a, true
			}
		}
		return codec.SVTRow{}, false
	}

	var out []dhsc.Object
	seen := map[string]bool{}
	for _, c := range cvt {
		if hidden[c.UID] || seen[c.UID] {
			continue
		}
		seen[c.UID] = true
		s, ok := describe(c.UID)
		if !ok {
			s = codec.SVTRow{UID: c.UID} // autoInserted: in the CVT, not in the SVT (§5.15.3)
		}
		if hidden[s.UID] {
			continue
		}
		o := object(c.UID, s, enumByUID)
		o.Value = dhsc.Value{Kind: dhsc.KindString, Str: string(c.Value), Raw: []byte(c.Value)}
		if c.ConfigValue != "" && c.ConfigValue != codec.Invalid {
			o.Meta["configValue"] = string(c.ConfigValue)
		}
		if c.LastUTS != "" && c.LastUTS != "0" {
			o.Meta["lastUTS"] = string(c.LastUTS)
		}
		out = append(out, o)
	}
	for uid, s := range exact {
		if seen[uid] || hidden[uid] {
			continue
		}
		o := object(uid, s, enumByUID)
		if s.MQTTOnly() {
			o.Meta["mqttOnly"] = true
		}
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return strings.Join(out[i].Path, "/") < strings.Join(out[j].Path, "/") })
	for i := range out {
		out[i].ID = i
	}
	return out
}

func object(uid string, s codec.SVTRow, enumByUID map[string]map[int64]string) dhsc.Object {
	path := strings.Split(uid, "/")
	o := dhsc.Object{
		Path:  path,
		Group: strings.Join(path[:len(path)-1], "/"),
		Label: path[len(path)-1],
		Kind:  dhsc.KindString,
		// The document gives AccessType and AccessLevel no meaning
		// (§5.57.3 lists the columns only). Every value is offered for
		// read and write; the controller decides, and the read-back
		// shows what it did.
		Access: 0x03,
		Meta:   map[string]any{"uid": uid},
	}
	if v := string(s.Units); v != "" && v != codec.NoUnits {
		o.Unit = v
	}
	if v := string(s.RangeMin); v != "" && v != codec.NoMin {
		o.Min = v
	}
	if v := string(s.RangeMax); v != "" && v != codec.NoMax {
		o.Max = v
	}
	if v := string(s.DefaultValue); v != "" && v != codec.NoDefault {
		o.Def = v
	}
	for k, v := range map[string]codec.Text{
		"name": s.EnName, "description": s.EnDescription, "valueType": s.ValueType,
		"formatType": s.FormatType, "accessType": s.AccessType, "accessLevel": s.AccessLevel,
		"unitScale": s.UnitScale, "precision": s.GUIDisplayPrecision, "volatility": s.Volatility,
		"dependency": s.Dependency, "snmpMibOid": s.SNMPMibOid,
	} {
		if v != "" && v != codec.NoDep {
			o.Meta[k] = string(v)
		}
	}
	if s.UID != "" && s.UID != uid {
		o.Meta["svtUID"] = s.UID // the array template this instance belongs to
	}
	if m := enumByUID[uid]; m == nil && s.UID != "" {
		enumByUID[uid] = enumByUID[s.UID]
	}
	if m := enumByUID[uid]; len(m) > 0 {
		o.Kind = dhsc.KindEnum
		labels := map[string]string{}
		var maxIdx int64 = -1
		for k, v := range m {
			labels[strconvI(k)] = v
			if k > maxIdx {
				maxIdx = k
			}
		}
		o.Meta["enum"] = labels
		if maxIdx >= 0 && maxIdx < 1024 && int64(len(m)) == maxIdx+1 {
			o.EnumItems = make([]string, maxIdx+1)
			for k, v := range m {
				o.EnumItems[k] = v
			}
		}
	}
	return o
}

func strconvI(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
		if n == 0 {
			break
		}
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
