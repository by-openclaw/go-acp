package main

import (
	"context"
	"dhs/internal/rrcs/codec"
	rrcs "dhs/internal/rrcs/consumer"
	"net"
)

// The names below are what the rrcs verbs of this command used before
// their logic moved into the rrcs library. Each one stands for the
// library's own, so the verbs and their tests read as they did while the
// move goes on part by part. A name leaves this file when its last user
// here is gone.

type rrcsSpecProp = rrcs.SpecProp

func rrcsSpecFor(object, block, field string) rrcsSpecProp {
	return rrcs.SpecFor(object, block, field)
}

type (
	rrcsNode   = rrcs.Node
	rrcsCard   = rrcs.Card
	rrcsPort   = rrcs.Port
	rrcsKey    = rrcs.Key
	rrcsObject = rrcs.Object
	rrcsXp     = rrcs.Xp
	rrcsModel  = rrcs.Model
)

func rrcsBuildModel(snap map[string]any) *rrcsModel { return rrcs.BuildModel(snap) }

// The readers of a document that went through encoding/json. The library
// has its own; these four serve the verbs that are still here.

func jMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func jInt(m map[string]any, key string) int {
	f, _ := m[key].(float64)
	return int(f)
}

func jStr(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func jBool(m map[string]any, key string) bool {
	b, _ := m[key].(bool)
	return b
}

var (
	rrcsInfoMethods     = rrcs.InfoMethods
	rrcsDiscoverMethods = rrcs.DiscoverMethods
	rrcsObjectTypes     = rrcs.ObjectTypes
	rrcsWalkLists       = rrcs.WalkLists
)

type (
	rrcsWalkSnapshot = rrcs.Snapshot
	rrcsCollectOpts  = rrcs.CollectOpts
)

func rrcsCollect(ctx context.Context, client *rrcs.Client, opts rrcsCollectOpts) (*rrcsWalkSnapshot, int, error) {
	if opts.Singles && opts.LocalIP == "" {
		opts.LocalIP = rrcsLocalIP("", client.Peer())
	}
	return rrcs.Collect(ctx, client, opts)
}

func rrcsListOf(reply rrcs.Reply) []codec.Value { return rrcs.ListOf(reply) }

func rrcsFieldInt(v codec.Value, name string) (int32, bool) { return rrcs.FieldInt(v, name) }

func rrcsFieldBool(v codec.Value, name string) bool { return rrcs.FieldBool(v, name) }

func rrcsJSON(v codec.Value) any { return rrcs.JSON(v) }

// rrcsLocalIP is the address RRCS has to send to: the one given with
// --listen, or the local end of the route towards the gateway.
func rrcsLocalIP(listen, peer string) string {
	if host, _, err := net.SplitHostPort(listen); err == nil && host != "" && host != "0.0.0.0" && host != "::" {
		return host
	}
	conn, err := net.Dial("udp", peer) // no packet is sent: UDP only picks the route
	if err != nil {
		return ""
	}
	defer func() { _ = conn.Close() }()
	host, _, _ := net.SplitHostPort(conn.LocalAddr().String())
	return host
}

type (
	rrcsMeta = rrcs.Meta
	rrcsRow  = rrcs.Row
)

var (
	rrcsCSVHeader = rrcs.CSVHeader
	rrcsEnums     = rrcs.Enums
	rrcsWritable  = rrcs.Writable
	rrcsMetaOf    = rrcs.MetaOf
)

func rrcsMetaFor(block, field string) rrcsMeta { return rrcs.MetaFor(block, field) }

func rrcsRows(m *rrcsModel) []rrcsRow { return rrcs.Rows(m) }

func rrcsRowsOf(target, object, path string, id int, props map[string]any) []rrcsRow {
	return rrcs.RowsOf(target, object, path, id, props)
}

func rrcsScalar(field string, v any) (value, kind, name, items string, ok bool) {
	return rrcs.Scalar(field, v)
}

type rrcsOp = rrcs.Op

var rrcsOps = rrcs.Ops

func rrcsPortOfPath(path string) (node, port int, isInput, ok bool) {
	_, node, port, isInput, ok = rrcs.PortOfPath(path)
	return node, port, isInput, ok
}

func rrcsKeyOfPath(path string) (node, port int, isInput bool, expansion, page, key int, ok bool) {
	return rrcs.KeyOfPath(path)
}

func rrcsNetOfPath(path string) int32 { return rrcs.NetOfPath(path) }

func rrcsValueOfJSON(text string) (codec.Value, error) { return rrcs.ValueOfJSON(text) }

type rrcsChangeLine = rrcs.ChangeLine

const rrcsListenerWait = rrcs.ListenerWait

func rrcsDecode(m *rrcsModel, e rrcs.Event) []rrcsChangeLine { return rrcs.Decode(m, e) }

func rrcsWatchCSVHeader() []string { return rrcs.WatchCSVHeader() }

func rrcsParamInt(p []codec.Value, i int) int { return rrcs.ParamInt(p, i) }

func rrcsMemberInt(v codec.Value, name string) int { return rrcs.MemberInt(v, name) }

func rrcsVolume(x int) (value, unit string) { return rrcs.VolumeText(x) }

func rrcsCompact(v codec.Value) string { return rrcs.Compact(v) }

func rrcsTarget(k *rrcsKey) string { return rrcs.Target(k) }

func rrcsCurrent(ctx context.Context, client *rrcs.Client, path string, values bool) (map[string]any, error) {
	return rrcs.Current(ctx, client, path, values)
}

func rrcsCurrentOne(ctx context.Context, client *rrcs.Client, path string) (map[string]any, bool) {
	return rrcs.CurrentOne(ctx, client, path)
}

func rrcsSplitPath(path string) (object, block, field string, ok bool) { return rrcs.SplitPath(path) }

// The configuration change and its proven forms live in the library;
// the verbs here keep the names they used.
type rrcsChange = rrcs.Change

const rrcsNMOSRefusal = rrcs.NMOSRefusal

func rrcsChangeFor(path string) (*rrcsChange, error) { return rrcs.ChangeFor(path) }
func rrcsStreamSteps(object string, c *rrcsChange, props map[string]any) ([]*rrcsChange, bool, string) {
	return rrcs.StreamSteps(object, c, props)
}
func rrcsPortNameEdits(m *rrcsModel, object string) []*rrcsChange {
	return rrcs.PortNameEdits(m, object)
}
