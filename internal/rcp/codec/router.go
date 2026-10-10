package codec

import (
	"fmt"
	"strconv"
	"strings"
)

// RouteSource is the source routed to a destination on one level. ID 0
// means nothing is routed there.
type RouteSource struct {
	ID         int64  `json:"id"`
	Name       string `json:"name,omitempty"`
	IOMnemonic string `json:"IOMnemonic,omitempty"`
}

// RouteLevel is one level of a destination in the routes table.
type RouteLevel struct {
	Name   string      `json:"name,omitempty"`
	Source RouteSource `json:"source"`
}

// RouteDest is one destination of the routes table. Levels is keyed
// "destLevel_<id>".
type RouteDest struct {
	Name   string                `json:"name"`
	Levels map[string]RouteLevel `json:"levels"`
}

// Routes is the routes table, keyed "dest_<id>".
type Routes map[string]RouteDest

// DestKey and LevelKey are the map keys of the routes table.
func DestKey(id int64) string  { return "dest_" + strconv.FormatInt(id, 10) }
func LevelKey(id int) string   { return "destLevel_" + strconv.Itoa(id) }
func IOLevelKey(id int) string { return "level_" + strconv.Itoa(id) }

// KeyID is the number at the end of a map key such as "dest_6",
// "destLevel_2", "src_7" or "level_1".
func KeyID(key string) (int64, error) {
	i := strings.LastIndexByte(key, '_')
	if i < 0 {
		return 0, fmt.Errorf("rcp: %q is not a <name>_<id> key", key)
	}
	n, err := strconv.ParseInt(key[i+1:], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("rcp: %q is not a <name>_<id> key", key)
	}
	return n, nil
}

// routeTake is the body of a route update: one destination, all its
// levels or one.
type routeTake struct {
	Source *RouteSource          `json:"source,omitempty"`
	Levels map[string]RouteLevel `json:"levels,omitempty"`
}

// TakeBody is the PATCH body that routes src to dest — on every level
// when level is 0, on that level only otherwise.
func TakeBody(dest, src int64, level int) map[string]any {
	t := routeTake{}
	if level == 0 {
		t.Source = &RouteSource{ID: src}
	} else {
		t.Levels = map[string]RouteLevel{LevelKey(level): {Source: RouteSource{ID: src}}}
	}
	return map[string]any{DestKey(dest): t}
}

// MnemonicKind is which of a router's three mnemonic tables is meant.
type MnemonicKind string

const (
	SourceMnemonics      MnemonicKind = "source"
	DestinationMnemonics MnemonicKind = "destination"
	LevelMnemonics       MnemonicKind = "level"
)

// ParseMnemonicKind accepts a kind as an operator types it.
func ParseMnemonicKind(s string) (MnemonicKind, error) {
	switch MnemonicKind(s) {
	case SourceMnemonics, DestinationMnemonics, LevelMnemonics:
		return MnemonicKind(s), nil
	}
	return "", fmt.Errorf("rcp: unknown mnemonic kind %q (want source, destination or level)", s)
}

// Path is the last segment of the table's URL; EnvelopeKey the member
// of the answer holding it; KeyPrefix the prefix of its map keys.
func (k MnemonicKind) Path() string        { return string(k) + "-mnemonics" }
func (k MnemonicKind) EnvelopeKey() string { return string(k) + "Mnemonics" }
func (k MnemonicKind) KeyPrefix() string {
	switch k {
	case SourceMnemonics:
		return "src_"
	case DestinationMnemonics:
		return "dest_"
	}
	return "level_"
}

// IOMnemonic is the name of the device IO behind a RouteMaster IO on
// one level.
type IOMnemonic struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// MnemonicEntry is one row of a mnemonic table.
type MnemonicEntry struct {
	Original    string            `json:"originalMnemonic"`
	Alternates  map[string]string `json:"alternateMnemonics,omitempty"`
	IOMnemonics []IOMnemonic      `json:"IOMnemonics,omitempty"`
}

// MnemonicUpdate is what is written on one row. Every field is optional.
type MnemonicUpdate struct {
	Original   *string           `json:"originalMnemonic,omitempty"`
	Alternates map[string]string `json:"alternateMnemonics,omitempty"`
}
