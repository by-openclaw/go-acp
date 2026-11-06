package consumer

// The device's inputs and outputs as an operator reads them in the web
// UI — input 1 to the last, one row per stream with the fields a
// provisioning sheet writes — and every routable channel against the
// matrix index the router control (Probel SW-P-08) addresses it by,
// with the stream that owns it and its UUID.
//
// The index is CCM's own `matrixIndex`. The audio generator and
// analyser channels carry none (SHUFFLE 6.0.0), though the router
// addresses them: those rows take their index from the router's label
// file when one is given, matched by name, and every such channel is a
// `ccm_no_matrix_index` deviation either way.

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	dhsc "dhs/internal/consumer"
)

// MatrixLabel is one entry of a router's source or destination label
// file: the default label and every width the router keeps.
type MatrixLabel struct {
	Default string
	Labels  []string
}

// MatrixRow is one routable channel.
type MatrixRow struct {
	Index       int // -1: neither the device nor the label file gives one
	Kind        string
	OwnerNo     string
	OwnerName   string
	OwnerUUID   string
	Channel     string
	ChannelName string
	ChannelUUID string
	// IndexFrom is "ccm" when the device gave the index, "labels" when
	// it was matched by name in the router's label file, "" otherwise.
	IndexFrom    string
	DefaultLabel string
	ShortLabel   string
	// NamesAgree is "yes" when one of the router's labels at this index
	// names the same channel as CCM does, "no" when none does, "" when
	// the router has no label here.
	NamesAgree string
}

// IOTables is what BuildIOTables returns: the stream tables with their
// header rows, the two channel lists in index order, and the channels
// the device gave no index.
type IOTables struct {
	Inputs       [][]string
	Outputs      [][]string
	Sources      []MatrixRow
	Destinations []MatrixRow
	NoIndex      []string // "kind: channel name"
}

// MatrixHeader is the column row of the source and destination lists.
var MatrixHeader = []string{"matrix_index", "kind", "owner_no", "owner_name", "owner_uuid",
	"channel", "channel_name", "channel_uuid", "index_from", "default_label", "short_label", "names_agree"}

// Record is the row as CSV fields.
func (r MatrixRow) Record() []string {
	idx := ""
	if r.Index >= 0 {
		idx = strconv.Itoa(r.Index)
	}
	return []string{idx, r.Kind, r.OwnerNo, r.OwnerName, r.OwnerUUID,
		r.Channel, r.ChannelName, r.ChannelUUID, r.IndexFrom, r.DefaultLabel, r.ShortLabel, r.NamesAgree}
}

// family is one kind of routable thing in the CCM tree.
type family struct {
	path string
	kind string
	// perItem: each item under path is itself one channel (mute).
	perItem bool
	// flat: path is the channel list itself (generator, analyser).
	// Otherwise each item owns channels.<uuid>.
	flat bool
}

var (
	sourceFamilies = []family{
		{path: "io.ip.receivers.audio", kind: "IP input"},
		{path: "io.madi.inputs", kind: "MADI input"},
		{path: "processing.audio.delay", kind: "delay bank"},
		{path: "processing.audio.generator.channels", kind: "generator", flat: true},
		{path: "processing.audio.mute", kind: "mute", perItem: true},
	}
	destinationFamilies = []family{
		{path: "io.ip.senders.audio", kind: "IP output"},
		{path: "io.madi.outputs", kind: "MADI output"},
		{path: "processing.audio.delay", kind: "delay bank"},
		{path: "processing.audio.analyser.channels", kind: "analyser", flat: true},
	}
)

// tree is the walk as resource path -> field -> text.
type tree map[string]map[string]string

func newTree(objs []dhsc.Object) tree {
	t := tree{}
	for _, o := range objs {
		if len(o.Path) < 2 {
			continue
		}
		res := strings.Join(o.Path[:len(o.Path)-1], ".")
		if t[res] == nil {
			t[res] = map[string]string{}
		}
		t[res][o.Path[len(o.Path)-1]] = valueText(o.Value)
	}
	return t
}

// children are the item keys directly under prefix, sorted.
func (t tree) children(prefix string) []string {
	seen := map[string]bool{}
	for k := range t {
		rest, ok := strings.CutPrefix(k, prefix+".")
		if !ok {
			continue
		}
		seen[strings.SplitN(rest, ".", 2)[0]] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (t tree) get(res, field string) string { return t[res][field] }

type channel struct {
	index  int
	number int
	uuid   string
	name   string
}

// channels lists the channels of one owner in matrix-index order,
// numbered by CCM's `number` or, where it has none, by the name.
func (t tree) channels(base string) []channel {
	var cs []channel
	for _, u := range t.children(base + ".channels") {
		d := t[base+".channels."+u]
		if d == nil {
			continue
		}
		cs = append(cs, toChannel(u, d))
	}
	sortChannels(cs)
	return cs
}

func toChannel(uuid string, d map[string]string) channel {
	c := channel{index: -1, uuid: uuid, name: d["name"]}
	if v, err := strconv.Atoi(d["matrixIndex"]); err == nil {
		c.index = v
	}
	if v, err := strconv.Atoi(d["number"]); err == nil {
		c.number = v
	} else if _, n, ok := chanParts(c.name); ok {
		c.number = n
	}
	return c
}

func sortChannels(cs []channel) {
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if (a.index < 0) != (b.index < 0) {
			return a.index >= 0
		}
		if a.index != b.index {
			return a.index < b.index
		}
		return a.number < b.number
	})
}

// ccmChannelName is CCM's channel name: family letters, owner number,
// dash, channel number ("IP001-01", "MA004-59", "AG001-12").
var ccmChannelName = regexp.MustCompile(`^([A-Za-z]+)(\d+)-(\d+)$`)

// flatLabel is the router's 8-character label of the same channel:
// family letters then owner and channel run together, two digits for
// the channel ("IP000101", "MA000459", "AG000112").
var flatLabel = regexp.MustCompile(`^([A-Za-z]+)(\d+)$`)

func chanParts(name string) (prefix string, ch int, ok bool) {
	m := ccmChannelName.FindStringSubmatch(name)
	if m == nil {
		return "", 0, false
	}
	n, _ := strconv.Atoi(m[3])
	return m[1], n, true
}

// nameKey reduces a CCM channel name to what the router's flat label
// of the same channel reduces to.
func nameKey(name string) string {
	m := ccmChannelName.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	owner, _ := strconv.Atoi(m[2])
	ch, _ := strconv.Atoi(m[3])
	if ch > 99 {
		return ""
	}
	return strings.ToUpper(m[1]) + ":" + strconv.Itoa(owner*100+ch)
}

func labelKey(label string) string {
	m := flatLabel.FindStringSubmatch(strings.TrimSpace(label))
	if m == nil {
		return ""
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return ""
	}
	return strings.ToUpper(m[1]) + ":" + strconv.Itoa(n)
}

// BuildIOTables reads a walked CCM tree into the stream tables and the
// two channel lists. srcLabels and dstLabels are the router's label
// files keyed by index; either may be nil.
func BuildIOTables(objs []dhsc.Object, srcLabels, dstLabels map[int]MatrixLabel) *IOTables {
	t := newTree(objs)
	out := &IOTables{}
	macs := map[string]string{}
	for _, u := range t.children("network.macs") {
		if n := t.get("network.macs."+u, "name"); n != "" {
			macs[u] = n
		} else {
			macs[u] = u
		}
	}
	iface := func(uuid string) string {
		if n, ok := macs[uuid]; ok {
			return n
		}
		return uuid
	}

	var srcRows, dstRows []MatrixRow
	out.Inputs = inputTable(t, iface, srcLabels, &srcRows)
	out.Outputs = outputTable(t, iface, dstLabels, &dstRows)
	for _, f := range sourceFamilies {
		if f.path == "io.ip.receivers.audio" {
			continue
		}
		srcRows = append(srcRows, familyRows(t, f)...)
	}
	for _, f := range destinationFamilies {
		if f.path == "io.ip.senders.audio" {
			continue
		}
		dstRows = append(dstRows, familyRows(t, f)...)
	}
	out.Sources, out.NoIndex = finish(srcRows, srcLabels, out.NoIndex)
	out.Destinations, out.NoIndex = finish(dstRows, dstLabels, out.NoIndex)
	return out
}

// owner is a stream, MADI port or delay bank and its channels.
type owner struct {
	uuid string
	cs   []channel
}

// owners lists what lives under path in the order of its first channel's
// index: input 1 is the one whose channels come first on the router.
func owners(t tree, path string) []owner {
	var list []owner
	for _, u := range t.children(path) {
		cs := t.channels(path + "." + u)
		if len(cs) == 0 {
			continue
		}
		list = append(list, owner{u, cs})
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i].cs[0].index, list[j].cs[0].index
		if (a < 0) != (b < 0) {
			return a >= 0
		}
		return a < b
	})
	return list
}

func familyRows(t tree, f family) []MatrixRow {
	var rows []MatrixRow
	if f.flat || f.perItem {
		var cs []channel
		for _, u := range t.children(f.path) {
			cs = append(cs, toChannel(u, t[f.path+"."+u]))
		}
		sortChannels(cs)
		for _, c := range cs {
			rows = append(rows, chanRow(f.kind, "", c.name, c.uuid, c))
		}
		return rows
	}
	for n, o := range owners(t, f.path) {
		name := t.get(f.path+"."+o.uuid, "name")
		for _, c := range o.cs {
			rows = append(rows, chanRow(f.kind, strconv.Itoa(n+1), name, o.uuid, c))
		}
	}
	return rows
}

func chanRow(kind, ownerNo, ownerName, ownerUUID string, c channel) MatrixRow {
	r := MatrixRow{Index: c.index, Kind: kind, OwnerNo: ownerNo, OwnerName: ownerName, OwnerUUID: ownerUUID,
		ChannelName: c.name, ChannelUUID: c.uuid}
	if c.number > 0 {
		r.Channel = strconv.Itoa(c.number)
	}
	if c.index >= 0 {
		r.IndexFrom = "ccm"
	}
	return r
}

// finish fills an index the device did not give from the label file,
// adds the router's labels, and sorts by index.
func finish(rows []MatrixRow, labels map[int]MatrixLabel, noIndex []string) ([]MatrixRow, []string) {
	byKey := map[string]int{}
	dup := map[string]bool{}
	for idx, l := range labels {
		for _, s := range append([]string{l.Default}, l.Labels...) {
			k := labelKey(s)
			if k == "" {
				continue
			}
			if prev, ok := byKey[k]; ok && prev != idx {
				dup[k] = true
			}
			byKey[k] = idx
		}
	}
	for i := range rows {
		r := &rows[i]
		if r.Index < 0 {
			noIndex = append(noIndex, r.Kind+": "+r.ChannelName)
			if k := nameKey(r.ChannelName); k != "" && !dup[k] {
				if idx, ok := byKey[k]; ok {
					r.Index, r.IndexFrom = idx, "labels"
				}
			}
		}
		if r.Index < 0 {
			continue
		}
		l, ok := labels[r.Index]
		if !ok {
			continue
		}
		r.DefaultLabel, r.ShortLabel = l.Default, shortest(l)
		r.NamesAgree = "no"
		want := nameKey(r.ChannelName)
		for _, s := range append([]string{l.Default}, l.Labels...) {
			if want != "" && labelKey(s) == want {
				r.NamesAgree = "yes"
				break
			}
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i].Index, rows[j].Index
		if (a < 0) != (b < 0) {
			return a >= 0
		}
		return a < b
	})
	return rows, noIndex
}

func shortest(l MatrixLabel) string {
	s := ""
	for _, v := range l.Labels {
		if v != "" && (s == "" || len(v) < len(s)) {
			s = v
		}
	}
	return s
}

func labelAt(labels map[int]MatrixLabel, idx int) string {
	if l, ok := labels[idx]; ok {
		if s := shortest(l); s != "" {
			return s
		}
		return l.Default
	}
	return ""
}

var inputHeader = []string{"input", "name", "uuid", "enable", "channel_slots", "channels", "payload_id", "sample_rate",
	"bit_depth", "packet_time", "rx_skew", "stream_delay_ms",
	"primary_interface", "primary_multicast", "primary_port", "primary_source_ip",
	"secondary_interface", "secondary_multicast", "secondary_port", "secondary_source_ip",
	"nmos_label", "nmos_group_hint", "matrix_first", "matrix_last", "label_first", "label_last"}

func inputTable(t tree, iface func(string) string, labels map[int]MatrixLabel, rows *[]MatrixRow) [][]string {
	const path = "io.ip.receivers.audio"
	table := [][]string{inputHeader}
	for n, o := range owners(t, path) {
		b := path + "." + o.uuid
		s := parseSDP(t.get(b, "sdp"))
		first, last := o.cs[0].index, o.cs[len(o.cs)-1].index
		table = append(table, []string{
			strconv.Itoa(n + 1), t.get(b, "name"), o.uuid, t.get(b, "enable"), strconv.Itoa(len(o.cs)),
			s.channels, s.payload, s.rate, s.depth, s.ptime, t.get(b, "skew"), t.get(b, "streamDelayMs"),
			iface(t.get(b+".primaryLeg", "mac")), s.leg(0).mcast, s.leg(0).port, s.leg(0).source,
			iface(t.get(b+".secondaryLeg", "mac")), s.leg(1).mcast, s.leg(1).port, s.leg(1).source,
			t.get(b+".nmos", "label"), t.get(b+".nmos", "groupHint"),
			indexText(first), indexText(last), labelAt(labels, first), labelAt(labels, last),
		})
		for _, c := range o.cs {
			*rows = append(*rows, chanRow("IP input", strconv.Itoa(n+1), t.get(b, "name"), o.uuid, c))
		}
	}
	return table
}

var outputHeader = []string{"output", "name", "uuid", "enable", "channel_slots", "channels", "sub_type", "payload_id",
	"bit_depth", "packet_time", "input_selection",
	"primary_interface", "primary_multicast", "primary_port",
	"secondary_interface", "secondary_multicast", "secondary_port",
	"nmos_label", "nmos_group_hint", "matrix_first", "matrix_last", "label_first", "label_last"}

func outputTable(t tree, iface func(string) string, labels map[int]MatrixLabel, rows *[]MatrixRow) [][]string {
	const path = "io.ip.senders.audio"
	payload := t["io.ip.senders.rtp-payload-types"]
	table := [][]string{outputHeader}
	for n, o := range owners(t, path) {
		b := path + "." + o.uuid
		mt := t.get(b, "mediaType")
		first, last := o.cs[0].index, o.cs[len(o.cs)-1].index
		table = append(table, []string{
			strconv.Itoa(n + 1), t.get(b, "name"), o.uuid, t.get(b, "enable"), strconv.Itoa(len(o.cs)),
			t.get(b, "nrChannels"), mt, payload[strings.ReplaceAll(mt, "-", "_")],
			t.get(b, "bitDepth"), t.get(b, "packetTime"), t.get(b, "inputSelection"),
			iface(t.get(b+".primaryLeg", "mac")), t.get(b+".primaryLeg", "ip"), t.get(b+".primaryLeg", "port"),
			iface(t.get(b+".secondaryLeg", "mac")), t.get(b+".secondaryLeg", "ip"), t.get(b+".secondaryLeg", "port"),
			t.get(b+".nmos", "label"), t.get(b+".nmos", "groupHint"),
			indexText(first), indexText(last), labelAt(labels, first), labelAt(labels, last),
		})
		for _, c := range o.cs {
			*rows = append(*rows, chanRow("IP output", strconv.Itoa(n+1), t.get(b, "name"), o.uuid, c))
		}
	}
	return table
}

func indexText(i int) string {
	if i < 0 {
		return ""
	}
	return strconv.Itoa(i)
}

// sdpFacts is what an input's SDP says about the stream it expects.
type sdpFacts struct {
	payload, depth, rate, channels, ptime string
	legs                                  []sdpLeg
}

type sdpLeg struct{ mcast, port, source string }

func (s sdpFacts) leg(i int) sdpLeg {
	if i < len(s.legs) {
		return s.legs[i]
	}
	return sdpLeg{}
}

var (
	sdpRtpmap = regexp.MustCompile(`^a=rtpmap:(\d+) L(\d+)/(\d+)(?:/(\d+))?`)
	sdpPtime  = regexp.MustCompile(`^a=ptime:([0-9.]+)`)
	sdpConn   = regexp.MustCompile(`^c=IN IP4 ([0-9.]+)`)
	sdpMedia  = regexp.MustCompile(`^m=audio (\d+)`)
	sdpFilter = regexp.MustCompile(`^a=source-filter: *incl IN IP4 [0-9.]+ ([0-9.]+)`)
)

// parseSDP reads an SDP (RFC 8866) one media section per leg: ST 2022-7
// sends the second leg as a second m= section.
func parseSDP(sdp string) sdpFacts {
	var s sdpFacts
	var cur *sdpLeg
	var sessConn, sessSource string
	for _, line := range strings.Split(strings.ReplaceAll(sdp, "\r", ""), "\n") {
		if m := sdpMedia.FindStringSubmatch(line); m != nil {
			s.legs = append(s.legs, sdpLeg{port: m[1]})
			cur = &s.legs[len(s.legs)-1]
			continue
		}
		if m := sdpConn.FindStringSubmatch(line); m != nil {
			switch {
			case cur == nil:
				sessConn = m[1]
			case cur.mcast == "":
				cur.mcast = m[1]
			}
		}
		if m := sdpFilter.FindStringSubmatch(line); m != nil {
			switch {
			case cur == nil:
				sessSource = m[1]
			case cur.source == "":
				cur.source = m[1]
			}
		}
		if m := sdpRtpmap.FindStringSubmatch(line); m != nil && s.payload == "" {
			s.payload, s.depth, s.rate, s.channels = m[1], m[2], m[3], m[4]
			if s.channels == "" {
				s.channels = "1"
			}
		}
		if m := sdpPtime.FindStringSubmatch(line); m != nil && s.ptime == "" {
			if v, err := strconv.ParseFloat(m[1], 64); err == nil {
				if v < 1 {
					s.ptime = strconv.FormatFloat(v*1000, 'f', -1, 64) + "us"
				} else {
					s.ptime = strconv.FormatFloat(v, 'f', -1, 64) + "ms"
				}
			}
		}
	}
	// A connection or filter line above the first m= applies to every
	// media section that has none of its own (RFC 8866 §5.7, RFC 4570 §3).
	for i := range s.legs {
		if s.legs[i].mcast == "" {
			s.legs[i].mcast = sessConn
		}
		if s.legs[i].source == "" {
			s.legs[i].source = sessSource
		}
	}
	return s
}
