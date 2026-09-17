package main

// --leg red|blue|both → the positional per-leg lists SetSender speaks.
//
// The vocabulary is the plant's: RED is leg 1 of an ST 2022-7 pair,
// BLUE is leg 2. Naming a leg expands to a two-slot list with an
// EMPTY slot for the untouched leg, which IS-05's merge semantics
// read as "leave that leg exactly as it is" — the property that makes
// a single-network retune safe on a live pair.

import (
	"fmt"
	"strings"
)

// expandLeg maps one leg name + single values onto the positional
// lists. destRaw/portRaw are the raw flag strings (to refuse comma
// lists loudly); ports is the already-parsed list.
func expandLeg(leg, destRaw, portRaw string, ports []int) ([]string, []int, error) {
	if strings.Contains(destRaw, ",") || strings.Contains(portRaw, ",") {
		return nil, nil, fmt.Errorf("nmos set: with --leg, --destination and --port take ONE value")
	}
	var portVal int
	if len(ports) == 1 {
		portVal = ports[0]
	}
	var ips []string
	var outPorts []int
	switch leg {
	case "red":
		if destRaw != "" {
			ips = []string{destRaw, ""}
		}
		if portVal != 0 {
			outPorts = []int{portVal, 0}
		}
	case "blue":
		if destRaw != "" {
			ips = []string{"", destRaw}
		}
		if portVal != 0 {
			outPorts = []int{0, portVal}
		}
	case "both":
		if destRaw != "" {
			return nil, nil, fmt.Errorf("nmos set: --leg both cannot take one --destination — " +
				"ST 2022-7 legs must not share a group; give --destination a,b instead")
		}
		if portVal != 0 {
			outPorts = []int{portVal, portVal}
		}
	default:
		return nil, nil, fmt.Errorf("nmos set: --leg %q (want red | blue | both)", leg)
	}
	return ips, outPorts, nil
}
