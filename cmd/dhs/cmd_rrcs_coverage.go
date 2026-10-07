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
	{"SetXpPrio", "8.1", "write", "call", "no"},
	{"SetXpDestructive", "8.1", "write", "call", "no"},
	{"KillXp", "8.1", "write", "xp, ensure", "no"},
	{"GetXpStatus", "8.1", "read", "xp, ensure", "no"},
	{"GetAllActiveXps", "8.1", "read", "walk, list xp", "yes, empty list only"},
	{"GetActiveXpsRange", "8.1", "read", "call", "no"},
	// §8.2 Volume
	{"SetXpVolume", "8.2", "write", "call", "no"},
	{"GetXpVolume", "8.2", "read", "call", "no"},
	// §8.3 – §8.5 Alias, label, gain
	{"SetPortAlias", "8.3", "write", "call", "no"},
	{"GetPortAlias", "8.3", "read", "walk", "yes"},
	{"SetPortLabel", "8.4", "write", "call", "no"},
	{"GetPortLabel", "8.4", "read", "walk", "yes"},
	{"SetInputGain", "8.5", "write", "call", "no"},
	{"GetInputGain", "8.5", "read", "walk", "yes"},
	{"SetOutputGain", "8.5", "write", "call", "no"},
	{"GetOutputGain", "8.5", "read", "walk", "yes"},
	{"GetLevelMeterValues", "8.5", "read", "call", "no"},
	// §8.6 GPIOs
	{"SetGpOutput", "8.6", "write", "call", "no"},
	{"GetGpInputState", "8.6", "read", "call", "no"},
	{"GetGpOutputState", "8.6", "read", "call", "no"},
	{"GetAllGpIns", "8.6", "read", "walk", "yes, empty list only"},
	{"GetAllGpOuts", "8.6", "read", "walk", "yes, empty list only"},
	// §8.7 Logic sources
	{"SetLogicSourceState", "8.7", "write", "call", "no"},
	{"GetAllLogicSources", "8.7", "read", "walk", "no"},
	{"GetAllLogicSources_v2", "8.7", "read", "walk, list logic", "yes"},
	// §8.8 Status
	{"GetAllCaps", "8.8", "read", "walk", "yes"},
	{"GetAllPorts", "8.8", "read", "walk, list ports", "yes"},
	{"GetPort", "8.8", "read", "call", "no"},
	{"GetPortExTypeList", "8.8", "read", "walk", "yes"},
	{"GetAllIFBs", "8.8", "read", "walk, list ifbs", "yes"},
	{"GetErrorCodeList", "8.8", "read", "walk", "yes"},
	{"GetState", "8.8", "read", "info", "yes"},
	{"SetStateWorking", "8.8", "write", "call", "no"},
	{"SetStateStandby", "8.8", "write", "call", "no"},
	{"GetAlive", "8.8", "read", "call", "no"},
	{"GetVersion", "8.8", "read", "info", "yes"},
	{"IsRegisteredForEvents", "8.8", "read", "call", "no"},
	{"IsConnectedToArtist", "8.8", "read", "info", "yes"},
	{"IsRegisteredForAllEvents", "8.8", "read", "watch", "yes"},
	{"GetAllConferences", "8.8", "read", "walk, list conferences", "yes"},
	{"GetAllGroups", "8.8", "read", "walk, list groups", "yes"},
	{"GetAllKeyConfiguration", "8.8", "read", "walk", "yes"},
	{"GetAllDevices", "8.8", "read", "walk", "yes, empty list only"},
	{"GetNetName", "8.8", "read", "walk", "no"},
	{"SetNetName", "8.8", "write", "call", "no"},
	{"GetAllNodes", "8.8", "read", "walk, list nodes", "yes"},
	{"GetNode", "8.8", "read", "call", "no"},
	{"GetNodeTypes", "8.8", "read", "walk", "yes"},
	{"GetAllClientCards", "8.8", "read", "walk, list cards", "yes"},
	{"GetClientCard", "8.8", "read", "call", "no"},
	{"GetClientCardTypes", "8.8", "read", "walk", "yes"},
	{"GetConfigurationID", "8.8", "read", "info", "yes"},
	// §8.9 Object lists
	{"GetObjectList", "8.9.1", "read", "walk", "yes"},
	{"GetObjectProperty", "8.9.1", "read", "walk, get --id", "yes"},
	{"GetObjectPropertyNames", "8.9.1", "read", "get --id --names", "no"},
	{"GetCommandList", "8.9.2", "read", "call", "no"},
	{"GetPortsCommandLists", "8.9.3", "read", "walk, list keys", "yes"},
	// §8.10 Configuration changes
	{"ConfigurationChange", "8.10.1", "write", "call", "no"},
	{"ConfigurationChangeEx", "8.10.1", "write", "set, import, ensure", "stopped RRCS on an AES67 stream edit"},
	{"BufferConfigurationChange", "8.10.2", "write", "call", "no"},
	{"BufferConfigurationChangeEx", "8.10.2", "write", "call", "no"},
	{"ApplyConfigurationChange", "8.10.3", "write", "call", "no"},
	{"ApplyConfigurationChangeEx", "8.10.3", "write", "call", "no"},
	// §8.11 Key and marker manipulation
	{"ClearKeyLabel", "8.11", "write", "call", "no"},
	{"ClearKeyLabelAndMarker", "8.11", "write", "call", "no"},
	{"ClearKeyMarker", "8.11", "write", "call", "no"},
	{"GetAllRemoteKeys", "8.11", "read", "call", "no"},
	{"GetRemoteKey", "8.11", "read", "call", "no"},
	{"LockKey", "8.11", "write", "call", "no"},
	{"PressKey", "8.11", "write", "call", "no"},
	{"PressKeyEx", "8.11", "write", "call", "no"},
	{"SetKeyLabel", "8.11", "write", "call", "no"},
	{"SetKeyLabelAndMarker", "8.11", "write", "call", "no"},
	{"SetKeyMarker", "8.11", "write", "call", "no"},
	// §8.12 Panel spy
	{"ChangePanelSpyRegistry", "8.12", "register", "watch --spy", "accepted; no panel became active"},
	// §8.13 Port cloning
	{"StartPortCloning", "8.13", "write", "call", "no"},
	{"StopPortCloning", "8.13", "write", "call", "no"},
	{"GetAllActivePortClones", "8.13", "read", "walk", "no"},
	// §8.14 IFB volume
	{"SetIFBVolumeMixMinus", "8.14", "write", "call", "no"},
	{"GetIFBVolumeMixMinus", "8.14", "read", "call", "no"},
	{"RemoveIFBVolumeMixMinus", "8.14", "write", "call", "no"},
	// §8.15 Registration for notifications
	{"XpVolumeChangeRegistryReset", "8.15", "register", "call", "no"},
	{"XpVolumeChangeRegistryAdd", "8.15", "register", "watch --volume", "no"},
	{"XpVolumeChangeRegistryRemove", "8.15", "register", "call", "no"},
	{"RegisterForEvents", "8.15", "register", "call", "no"},
	{"UnregisterForEvents", "8.15", "register", "call", "no"},
	{"RegisterForGpInputChange", "8.15", "register", "call", "no"},
	{"UnregisterForGpInputChange", "8.15", "register", "call", "no"},
	{"RegisterForGpOutputChange", "8.15", "register", "call", "no"},
	{"UnregisterForGpOutputChange", "8.15", "register", "call", "no"},
	{"RegisterForEventsEx", "8.15", "register", "watch --volume", "no"},
	{"UnregisterForEventsEx", "8.15", "register", "watch --volume", "no"},
	{"RegisterForAllEvents", "8.15.1", "register", "watch", "yes"},
	{"UnregisterForAllEvents", "8.15.2", "register", "watch", "yes"},
	// §8.16 Trunking
	{"GetTrunkPorts", "8.16", "read", "walk", "yes, empty list only"},
	{"GetTrunklineSetup", "8.16", "read", "walk", "answers error 99 without trunk ports"},
	{"GetTrunklineActivities", "8.16", "read", "walk", "answers error 99 without trunk ports"},
	{"GetTrunkIfbs", "8.16", "read", "walk", "yes, empty list only"},
	{"GetTrunkingNetAddr", "8.16", "read", "walk", "yes"},
	{"SetTrunkingNetAddr", "8.16", "write", "call", "no"},
	{"GetLTC", "8.16", "read", "walk", "no"},
	{"SetLTC", "8.16", "write", "call", "no"},
	// §8.17 – §8.24
	{"DialNumber", "8.17", "write", "call", "no"},
	{"HangUpCall", "8.17", "write", "call", "no"},
	{"GetLicenseInfo", "8.18", "read", "walk", "yes"},
	{"GetPoolPortInfo", "8.19", "read", "call", "no"},
	{"SetSystemTimeOnAllNodes", "8.20", "write", "call", "no"},
	{"ConnectToArtist", "8.21", "write", "call", "no"},
	{"DisconnectFromArtist", "8.21", "write", "call", "no"},
	{"ResetAllNodes", "8.22", "write", "call", "no"},
	{"DeletePortCommands", "8.23", "write", "call", "no"},
	{"GetStageNetAddr", "8.24", "read", "walk", "yes"},
	{"SetStageNetAddr", "8.24", "write", "call", "no"},
	{"GetStageRegistryUrl", "8.24", "read", "walk", "no"},
	{"SetStageRegistryUrl", "8.24", "write", "call", "no"},
	// §9 Notifications
	{"SendString", "9.1", "notify", "watch", "no"},
	{"SendStringOff", "9.1", "notify", "watch", "no"},
	{"GpInputChange", "9.2", "notify", "watch", "no"},
	{"GpOutputChange", "9.2", "notify", "watch", "no"},
	{"LogicSourceChange", "9.3", "notify", "watch", "no"},
	{"XpVolumeChange", "9.4", "notify", "watch --volume", "no"},
	{"ConfigurationChange (notification)", "9.5", "notify", "watch", "no"},
	{"CrosspointChange", "9.6", "notify", "watch", "yes"},
	{"UpstreamFailed", "9.7", "notify", "watch", "no"},
	{"UpstreamFailedCleared", "9.7", "notify", "watch", "no"},
	{"DownstreamFailed", "9.7", "notify", "watch", "no"},
	{"DownstreamFailedCleared", "9.7", "notify", "watch", "no"},
	{"NodeControllerFailed", "9.7", "notify", "watch", "no"},
	{"NodeControllerReboot", "9.7", "notify", "watch", "no"},
	{"ClientFailed", "9.7", "notify", "watch", "no"},
	{"ClientFailedCleared", "9.7", "notify", "watch", "no"},
	{"PortInactive", "9.7", "notify", "watch", "no"},
	{"PortActive", "9.7", "notify", "watch", "yes"},
	{"ConnectArtistRestored", "9.7", "notify", "watch", "yes"},
	{"ConnectArtistFailure", "9.7", "notify", "watch", "no"},
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
