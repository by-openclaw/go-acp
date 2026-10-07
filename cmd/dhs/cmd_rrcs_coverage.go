package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

// rrcsMethod is one method or notification of the specification, and
// where this connector stands on it.
type rrcsMethod struct {
	Name    string `json:"name"`
	Section string `json:"section"`
	// Kind: read | write | register | notify.
	Kind string `json:"kind"`
	// Verb is the dhs verb that uses it; "call" means it is reachable
	// through the generic call verb only, "" that a notification is not
	// decoded.
	Verb string `json:"verb"`
	// Real is what a real RRCS has shown: "yes" answered or received as
	// expected, "no" never tried, or a short note.
	Real string `json:"real"`
}

// rrcsCatalog lists every method and notification of "RRCS Interface
// Specification" 9.0.1 Rev 1 (§8, §9, §10). It is the single place that
// says what is covered; the coverage verb prints it and a test keeps the
// verbs honest against it.
var rrcsCatalog = []rrcsMethod{
	// §8.1 Crosspoints
	{"SetXp", "8.1", "write", "xp, ensure", "no"},
	{"SetXpPrio", "8.1", "write", "set-xp-prio", "no"},
	{"SetXpDestructive", "8.1", "write", "set-xp-destructive", "no"},
	{"KillXp", "8.1", "write", "xp, ensure", "no"},
	{"GetXpStatus", "8.1", "read", "xp, ensure", "yes"},
	{"GetAllActiveXps", "8.1", "read", "walk, list xp", "yes, empty list only"},
	{"GetActiveXpsRange", "8.1", "read", "walk", "yes, empty list only"},
	// §8.2 Volume
	{"SetXpVolume", "8.2", "write", "set-xp-volume", "yes"},
	{"GetXpVolume", "8.2", "read", "walk; xp --level yes instead", "refused on an Artist-1024 (Node address invalid)"},
	// §8.3 – §8.5 Alias, label, gain
	{"SetPortAlias", "8.3", "write", "set-port-alias", "no"},
	{"GetPortAlias", "8.3", "read", "walk", "yes"},
	{"SetPortLabel", "8.4", "write", "set-port-label", "no"},
	{"GetPortLabel", "8.4", "read", "walk", "yes"},
	{"SetInputGain", "8.5", "write", "set-input-gain", "no"},
	{"GetInputGain", "8.5", "read", "walk", "yes"},
	{"SetOutputGain", "8.5", "write", "set-output-gain", "no"},
	{"GetOutputGain", "8.5", "read", "walk", "yes"},
	{"GetLevelMeterValues", "8.5", "read", "walk", "yes"},
	// §8.6 GPIOs
	{"SetGpOutput", "8.6", "write", "set-gp-output", "no"},
	{"GetGpInputState", "8.6", "read", "get-gp-input-state", "no"},
	{"GetGpOutputState", "8.6", "read", "get-gp-output-state", "no"},
	{"GetAllGpIns", "8.6", "read", "walk", "yes, empty list only"},
	{"GetAllGpOuts", "8.6", "read", "walk", "yes, empty list only"},
	// §8.7 Logic sources
	{"SetLogicSourceState", "8.7", "write", "set-logic-source-state", "no"},
	{"GetAllLogicSources", "8.7", "read", "walk", "yes"},
	{"GetAllLogicSources_v2", "8.7", "read", "walk, list logic", "yes"},
	// §8.8 Status
	{"GetAllCaps", "8.8", "read", "walk", "yes"},
	{"GetAllPorts", "8.8", "read", "walk, list ports", "yes"},
	{"GetPort", "8.8", "read", "walk", "yes"},
	{"GetPortExTypeList", "8.8", "read", "walk", "yes"},
	{"GetAllIFBs", "8.8", "read", "walk, list ifbs", "yes"},
	{"GetErrorCodeList", "8.8", "read", "walk", "yes"},
	{"GetState", "8.8", "read", "info", "yes"},
	{"SetStateWorking", "8.8", "write", "set-state-working", "no"},
	{"SetStateStandby", "8.8", "write", "set-state-standby", "no"},
	{"GetAlive", "8.8", "read", "walk", "yes"},
	{"GetVersion", "8.8", "read", "info", "yes"},
	{"IsRegisteredForEvents", "8.8", "read", "walk", "yes"},
	{"IsConnectedToArtist", "8.8", "read", "info", "yes"},
	{"IsRegisteredForAllEvents", "8.8", "read", "watch", "yes"},
	{"GetAllConferences", "8.8", "read", "walk, list conferences", "yes"},
	{"GetAllGroups", "8.8", "read", "walk, list groups", "yes"},
	{"GetAllKeyConfiguration", "8.8", "read", "walk", "yes"},
	{"GetAllDevices", "8.8", "read", "walk", "yes, empty list only"},
	{"GetNetName", "8.8", "read", "walk", "yes"},
	{"SetNetName", "8.8", "write", "set-net-name", "no"},
	{"GetAllNodes", "8.8", "read", "walk, list nodes", "yes"},
	{"GetNode", "8.8", "read", "walk", "yes"},
	{"GetNodeTypes", "8.8", "read", "walk", "yes"},
	{"GetAllClientCards", "8.8", "read", "walk, list cards", "yes"},
	{"GetClientCard", "8.8", "read", "walk", "yes"},
	{"GetClientCardTypes", "8.8", "read", "walk", "yes"},
	{"GetConfigurationID", "8.8", "read", "info", "yes"},
	// §8.9 Object lists
	{"GetObjectList", "8.9.1", "read", "walk", "yes"},
	{"GetObjectProperty", "8.9.1", "read", "walk, get --id", "yes"},
	{"GetObjectPropertyNames", "8.9.1", "read", "get --id --names", "no"},
	{"GetCommandList", "8.9.2", "read", "walk", "yes"},
	{"GetPortsCommandLists", "8.9.3", "read", "walk, list keys", "yes"},
	// §8.10 Configuration changes
	{"ConfigurationChange", "8.10.1", "write", "configuration-change", "no"},
	{"ConfigurationChangeEx", "8.10.1", "write", "configuration-change-ex", "stopped RRCS on an AES67 stream edit; no longer used by set, import, ensure"},
	{"BufferConfigurationChange", "8.10.2", "write", "buffer-configuration-change", "no"},
	{"BufferConfigurationChangeEx", "8.10.2", "write", "buffer-configuration-change-ex", "no"},
	{"ApplyConfigurationChange", "8.10.3", "write", "apply-configuration-change", "no"},
	{"ApplyConfigurationChangeEx", "8.10.3", "write", "apply-configuration-change-ex", "no"},
	// §8.11 Key and marker manipulation
	{"ClearKeyLabel", "8.11", "write", "clear-key-label", "no"},
	{"ClearKeyLabelAndMarker", "8.11", "write", "clear-key-label-and-marker", "no"},
	{"ClearKeyMarker", "8.11", "write", "clear-key-marker", "no"},
	{"GetAllRemoteKeys", "8.11", "read", "walk", "yes, empty list only"},
	{"GetRemoteKey", "8.11", "read", "walk", "yes"},
	{"LockKey", "8.11", "write", "lock-key", "no"},
	{"PressKey", "8.11", "write", "press-key", "no"},
	{"PressKeyEx", "8.11", "write", "press-key-ex", "no"},
	{"SetKeyLabel", "8.11", "write", "set-key-label", "no"},
	{"SetKeyLabelAndMarker", "8.11", "write", "set-key-label-and-marker", "no"},
	{"SetKeyMarker", "8.11", "write", "set-key-marker", "no"},
	// §8.12 Panel spy
	{"ChangePanelSpyRegistry", "8.12", "register", "watch --spy", "accepted; refused by the system (no panel spy licence)"},
	// §8.13 Port cloning
	{"StartPortCloning", "8.13", "write", "start-port-cloning", "no"},
	{"StopPortCloning", "8.13", "write", "stop-port-cloning", "no"},
	{"GetAllActivePortClones", "8.13", "read", "walk", "yes, empty list only"},
	// §8.14 IFB volume
	{"SetIFBVolumeMixMinus", "8.14", "write", "set-ifb-volume-mix-minus", "no"},
	{"GetIFBVolumeMixMinus", "8.14", "read", "walk", "no"},
	{"RemoveIFBVolumeMixMinus", "8.14", "write", "remove-ifb-volume-mix-minus", "no"},
	// §8.15 Registration for notifications
	{"XpVolumeChangeRegistryReset", "8.15", "register", "xp-volume-change-registry-reset", "no"},
	{"XpVolumeChangeRegistryAdd", "8.15", "register", "watch --volume", "yes"},
	{"XpVolumeChangeRegistryRemove", "8.15", "register", "xp-volume-change-registry-remove", "no"},
	{"RegisterForEvents", "8.15", "register", "register-for-events", "no"},
	{"UnregisterForEvents", "8.15", "register", "unregister-for-events", "no"},
	{"RegisterForGpInputChange", "8.15", "register", "register-for-gp-input-change", "no"},
	{"UnregisterForGpInputChange", "8.15", "register", "unregister-for-gp-input-change", "no"},
	{"RegisterForGpOutputChange", "8.15", "register", "register-for-gp-output-change", "no"},
	{"UnregisterForGpOutputChange", "8.15", "register", "unregister-for-gp-output-change", "no"},
	{"RegisterForEventsEx", "8.15", "register", "watch --volume", "yes"},
	{"UnregisterForEventsEx", "8.15", "register", "watch --volume", "no"},
	{"RegisterForAllEvents", "8.15.1", "register", "watch", "yes"},
	{"UnregisterForAllEvents", "8.15.2", "register", "watch", "yes"},
	// §8.16 Trunking
	{"GetTrunkPorts", "8.16", "read", "walk", "yes, empty list only"},
	{"GetTrunklineSetup", "8.16", "read", "walk", "answers error 99 without trunk ports"},
	{"GetTrunklineActivities", "8.16", "read", "walk", "answers error 99 without trunk ports"},
	{"GetTrunkIfbs", "8.16", "read", "walk", "yes, empty list only"},
	{"GetTrunkingNetAddr", "8.16", "read", "walk", "yes"},
	{"SetTrunkingNetAddr", "8.16", "write", "set-trunking-net-addr", "no"},
	{"GetLTC", "8.16", "read", "walk", "yes, empty list only"},
	{"SetLTC", "8.16", "write", "set-ltc", "no"},
	// §8.17 – §8.24
	{"DialNumber", "8.17", "write", "dial-number", "no"},
	{"LineStatus", "8.17", "read", "line-status", "no"},
	{"HangUpCall", "8.17", "write", "hang-up-call", "no"},
	{"GetLicenseInfo", "8.18", "read", "walk", "yes"},
	{"GetPoolPortInfo", "8.19", "read", "walk", "yes"},
	{"SetSystemTimeOnAllNodes", "8.20", "write", "set-system-time-on-all-nodes", "no"},
	{"ConnectToArtist", "8.21", "write", "connect-to-artist", "no"},
	{"DisconnectFromArtist", "8.21", "write", "disconnect-from-artist", "no"},
	{"ResetAllNodes", "8.22", "write", "reset-all-nodes", "no"},
	{"DeletePortCommands", "8.23", "write", "delete-port-commands", "no"},
	{"GetStageNetAddr", "8.24", "read", "walk", "yes"},
	{"SetStageNetAddr", "8.24", "write", "set-stage-net-addr", "no"},
	{"GetStageRegistryUrl", "8.24", "read", "walk", "yes"},
	{"SetStageRegistryUrl", "8.24", "write", "set-stage-registry-url", "no"},
	// §9 Notifications
	{"SendString", "9.1", "notify", "watch", "no"},
	{"SendStringOff", "9.1", "notify", "watch", "no"},
	{"GpInputChange", "9.2", "notify", "watch", "no"},
	{"GpOutputChange", "9.2", "notify", "watch", "no"},
	{"LogicSourceChange", "9.3", "notify", "watch", "no"},
	{"XpVolumeChange", "9.4", "notify", "watch --volume", "yes"},
	{"ConfigurationChange (notification)", "9.5", "notify", "watch", "yes"},
	{"CrosspointChange", "9.6", "notify", "watch", "yes"},
	{"UpstreamFailed", "9.7", "notify", "watch", "no"},
	{"UpstreamFailedCleared", "9.7", "notify", "watch", "no"},
	{"DownstreamFailed", "9.7", "notify", "watch", "no"},
	{"DownstreamFailedCleared", "9.7", "notify", "watch", "no"},
	{"NodeControllerFailed", "9.7", "notify", "watch", "no"},
	{"NodeControllerReboot", "9.7", "notify", "watch", "yes"},
	{"ClientFailed", "9.7", "notify", "watch", "no"},
	{"ClientFailedCleared", "9.7", "notify", "watch", "no"},
	{"PortInactive", "9.7", "notify", "watch", "no"},
	{"PortActive", "9.7", "notify", "watch", "yes"},
	{"ConnectArtistRestored", "9.7", "notify", "watch", "yes"},
	{"ConnectArtistFailure", "9.7", "notify", "watch", "yes"},
	{"GatewayShutdown", "9.7", "notify", "watch", "no"},
	{"SicFailed", "9.7", "notify", "watch", "no"},
	{"GetAlive (notification)", "9.8", "notify", "watch", "yes"},
	{"PanelSpyStateChanged", "9.9.2", "notify", "watch --spy", "yes, error states only"},
	{"PanelSpyRotateEvent", "9.9.3", "notify", "watch --spy", "no"},
	{"PanelSpyKeyEvent", "9.9.4", "notify", "watch --spy", "no"},
	{"PanelSpyFuncKeyEvent", "9.9.5", "notify", "watch --spy", "no"},
	{"PanelSpyNumKeyEvent", "9.9.6", "notify", "watch --spy", "no"},
}

// rrcsCoverage prints the catalogue and its totals.
func rrcsCoverage(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("rrcs coverage", flag.ContinueOnError)
	output := fs.String("output", "text", "output: text | json")
	only := fs.String("only", "", "keep one group: call (no verb of its own yet) | unproven (never seen working on a real RRCS) | read | write | register | notify")
	if err := parseVerbFlags(fs, reorderFlagsFirst(args)); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return rrcsValErr("coverage", "takes no argument: it describes the connector, not a gateway")
	}
	keep := func(m rrcsMethod) bool {
		switch *only {
		case "":
			return true
		case "call":
			return m.Verb == "call"
		case "unproven":
			return !strings.HasPrefix(m.Real, "yes")
		}
		return m.Kind == *only
	}
	switch *only {
	case "", "call", "unproven", "read", "write", "register", "notify":
	default:
		return rrcsValErr("coverage", "--only takes call, unproven, read, write, register, notify")
	}
	rows := []rrcsMethod{}
	for _, m := range rrcsCatalog {
		if keep(m) {
			rows = append(rows, m)
		}
	}
	if *output == "json" {
		b, err := json.Marshal(rows)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "SECTION\tMETHOD\tKIND\tDHS VERB\tON A REAL RRCS")
	for _, m := range rows {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", m.Section, m.Name, m.Kind, m.Verb, m.Real)
	}
	_ = tw.Flush()

	type tally struct{ all, own, real int }
	kinds := []string{"read", "write", "register", "notify"}
	sum := map[string]*tally{}
	for _, k := range kinds {
		sum[k] = &tally{}
	}
	total := &tally{}
	for _, m := range rrcsCatalog {
		for _, t := range []*tally{sum[m.Kind], total} {
			t.all++
			if m.Verb != "call" && m.Verb != "" {
				t.own++
			}
			if strings.HasPrefix(m.Real, "yes") {
				t.real++
			}
		}
	}
	fmt.Println()
	tw = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "KIND\tIN THE SPECIFICATION\tREACHABLE\tWITH A VERB OF ITS OWN\tSEEN WORKING ON A REAL RRCS")
	for _, k := range append(kinds, "total") {
		t := total
		if k != "total" {
			t = sum[k]
		}
		_, _ = fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\n", k, t.all, t.all, t.own, t.real)
	}
	return tw.Flush()
}
