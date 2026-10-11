package main

import rrcs "dhs/internal/rrcs/consumer"

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
