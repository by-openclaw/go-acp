// The DhsFaultControl worker — the vendor-class seam that lets an
// operator (or the Ansible verify plays) drive the BCP-008 monitor
// health engine over the standard wire.
//
// Every monitor status property is read-only by class definition, so
// neither the AMWA tool nor a controller can induce a fault through
// NcObject.Set — which is exactly why the tool marks its
// fault-transition tests Manual. This worker turns "manually force an
// error condition" into four spec-legal method invocations (MS-05-02
// authority-key 0, the DhsGainControl pattern), reachable over IS-12
// AND over IS-14's rolePaths/{path}/methods/{id} REST invoke:
//
//	4m1 InjectMonitorFault(monitorRole, domain, status, message)
//	4m2 ClearMonitorFault(monitorRole, domain)
//	4m3 SetMonitorSyncSource(monitorRole, sourceId)
//	4m4 AddMonitorPacketCounters(monitorRole, counter, name, increment)
//
// The methods only ever feed the health engine (monitor_health.go),
// so injected transitions get the same debounce, transition counters,
// status messages, and overallStatus mapping as organic ones.
package provider

import (
	"encoding/json"
	"fmt"

	"dhs/internal/amwa/codec/ms05"
)

const (
	faultClassName = "DhsFaultControl"
	faultRole      = "FaultControl"
)

// faultClassID: NcWorker branch, authority key 0, class 2 (class 1 is
// DhsGainControl).
var faultClassID = ms05.NcClassId{1, 2, 0, 2}

// vendorFaultClass is the DhsFaultControl descriptor (own elements
// only, like every published model file).
func vendorFaultClass() ms05.NcClassDescriptor {
	str := func(name, desc string) ms05.NcParameterDescriptor {
		return ms05.NcParameterDescriptor{
			NcDescriptor: ms05.NcDescriptor{Description: strp(desc)},
			Name:         name,
			TypeName:     strp("NcString"),
		}
	}
	return ms05.NcClassDescriptor{
		NcDescriptor: ms05.NcDescriptor{Description: strp("dhs fault injection for BCP-008 status monitors")},
		ClassID:      faultClassID,
		Name:         faultClassName,
		FixedRole:    strp(faultRole),
		Properties:   []ms05.NcPropertyDescriptor{},
		Methods: []ms05.NcMethodDescriptor{
			{
				NcDescriptor:   ms05.NcDescriptor{Description: strp("Force a domain status on a monitor")},
				ID:             ms05.NcMethodId{Level: 4, Index: 1},
				Name:           "InjectMonitorFault",
				ResultDatatype: "NcMethodResult",
				Parameters: []ms05.NcParameterDescriptor{
					str("monitorRole", "Monitor role, e.g. ReceiverMonitor-00"),
					str("domain", "Status property name, e.g. linkStatus"),
					{
						NcDescriptor: ms05.NcDescriptor{Description: strp("Status enum value (2 PartiallyHealthy, 3 Unhealthy)")},
						Name:         "status",
						TypeName:     strp("NcUint64"),
					},
					str("message", "Status message accompanying the fault"),
				},
			},
			{
				NcDescriptor:   ms05.NcDescriptor{Description: strp("Clear an injected fault (recovery is delayed by statusReportingDelay)")},
				ID:             ms05.NcMethodId{Level: 4, Index: 2},
				Name:           "ClearMonitorFault",
				ResultDatatype: "NcMethodResult",
				Parameters: []ms05.NcParameterDescriptor{
					str("monitorRole", "Monitor role"),
					str("domain", "Status property name"),
				},
			},
			{
				NcDescriptor:   ms05.NcDescriptor{Description: strp("Change the synchronization source (dips to PartiallyHealthy)")},
				ID:             ms05.NcMethodId{Level: 4, Index: 3},
				Name:           "SetMonitorSyncSource",
				ResultDatatype: "NcMethodResult",
				Parameters: []ms05.NcParameterDescriptor{
					str("monitorRole", "Monitor role"),
					str("sourceId", "New synchronization source id"),
				},
			},
			{
				NcDescriptor:   ms05.NcDescriptor{Description: strp("Accumulate a late/lost/transmission packet counter")},
				ID:             ms05.NcMethodId{Level: 4, Index: 4},
				Name:           "AddMonitorPacketCounters",
				ResultDatatype: "NcMethodResult",
				Parameters: []ms05.NcParameterDescriptor{
					str("monitorRole", "Monitor role"),
					str("counter", "Counter kind: late, lost or transmission"),
					str("name", "Counter entry name"),
					{
						NcDescriptor: ms05.NcDescriptor{Description: strp("Amount to add")},
						Name:         "increment",
						TypeName:     strp("NcUint64"),
					},
				},
			},
		},
		Events: []ms05.NcEventDescriptor{},
	}
}

// faultArgs is the union argument shape of all four methods.
type faultArgs struct {
	MonitorRole string `json:"monitorRole"`
	Domain      string `json:"domain"`
	Status      *int   `json:"status"`
	Message     string `json:"message"`
	SourceID    string `json:"sourceId"`
	Counter     string `json:"counter"`
	Name        string `json:"name"`
	Increment   uint64 `json:"increment"`
}

// invokeFaultMethod runs one DhsFaultControl method by name against
// the health engine. Shared by the IS-12 and IS-14 dispatchers.
func (s *IS14ConfigurationServer) invokeFaultMethod(name string, rawArgs json.RawMessage) error {
	var a faultArgs
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &a); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if a.MonitorRole == "" {
		return fmt.Errorf("%s: monitorRole argument required", name)
	}
	switch name {
	case "InjectMonitorFault":
		if a.Domain == "" || a.Status == nil {
			return fmt.Errorf("InjectMonitorFault: domain and status arguments required")
		}
		if *a.Status < monStatusPartiallyHealthy || *a.Status > monStatusUnhealthy {
			return fmt.Errorf("InjectMonitorFault: status must be 2 (PartiallyHealthy) or 3 (Unhealthy), got %d", *a.Status)
		}
		return s.SetMonitorFault(a.MonitorRole, a.Domain, *a.Status, a.Message)
	case "ClearMonitorFault":
		if a.Domain == "" {
			return fmt.Errorf("ClearMonitorFault: domain argument required")
		}
		return s.SetMonitorFault(a.MonitorRole, a.Domain, monStatusHealthy, "")
	case "SetMonitorSyncSource":
		if a.SourceID == "" {
			return fmt.Errorf("SetMonitorSyncSource: sourceId argument required")
		}
		return s.SetMonitorSyncSource(a.MonitorRole, a.SourceID)
	case "AddMonitorPacketCounters":
		switch a.Counter {
		case "late", "lost", "transmission":
		default:
			return fmt.Errorf("AddMonitorPacketCounters: counter must be late, lost or transmission, got %q", a.Counter)
		}
		if a.Name == "" {
			return fmt.Errorf("AddMonitorPacketCounters: name argument required")
		}
		return s.AddMonitorPacketCounters(a.MonitorRole, a.Counter, a.Name, a.Increment)
	}
	return fmt.Errorf("no DhsFaultControl method %q", name)
}

// faultMethodNames gates the by-name dispatch.
func isFaultMethod(name string) bool {
	switch name {
	case "InjectMonitorFault", "ClearMonitorFault", "SetMonitorSyncSource", "AddMonitorPacketCounters":
		return true
	}
	return false
}
