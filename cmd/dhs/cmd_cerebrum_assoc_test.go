package main

import "testing"

func TestAssocAction(t *testing.T) {
	a, err := assocAction("SRCE_ASSOC", "7", "", "5", "Snell SW-P-08", "ROUTER", "3", "41", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.LogicalSrceID != "7" || a.LogicalLevelID != "5" || a.TargetDeviceName != "Snell SW-P-08" ||
		a.TargetDeviceType != "ROUTER" || a.TargetLevelID != "3" || a.TargetSrceID != "41" || a.TargetDestID != "" {
		t.Errorf("SRCE_ASSOC = %+v", a)
	}
	if got := assocTarget(a); got != `"Snell SW-P-08" level=3 io=41` {
		t.Errorf("target = %s", got)
	}

	a, err = assocAction("DEST_ASSOC", "", "6", "1", "R", "ROUTER", "", "9", "", "")
	if err != nil || a.LogicalDestID != "6" || a.TargetDestID != "9" || a.LogicalSrceID != "" {
		t.Errorf("DEST_ASSOC = %+v, %v", a, err)
	}

	a, err = assocAction("SRCE_ASSOC_IP", "7", "", "4", "NMOS", "ROUTER", "", "", "VTX-01", "1")
	if err != nil || a.TargetSenderName != "VTX-01" || a.SubDevice != "1" || a.TargetDeviceType != "" || a.TargetSrceID != "" {
		t.Errorf("SRCE_ASSOC_IP = %+v, %v", a, err)
	}
	if got := assocTarget(a); got != `"NMOS" "VTX-01"` {
		t.Errorf("target = %s", got)
	}
	a, err = assocAction("DEST_ASSOC_IP", "", "5", "4", "NMOS", "ROUTER", "", "", "VRX-01", "")
	if err != nil || a.TargetReceiverName != "VRX-01" {
		t.Errorf("DEST_ASSOC_IP = %+v, %v", a, err)
	}

	for name, call := range map[string]func() error{
		"unknown kind": func() error { _, err := assocAction("ROUTE", "1", "", "1", "R", "", "", "1", "", ""); return err },
		"no id":        func() error { _, err := assocAction("SRCE_ASSOC", "", "6", "1", "R", "", "", "1", "", ""); return err },
		"no level":     func() error { _, err := assocAction("SRCE_ASSOC", "7", "", "", "R", "", "", "1", "", ""); return err },
		"no device":    func() error { _, err := assocAction("SRCE_ASSOC", "7", "", "1", "", "", "", "1", "", ""); return err },
		"no io":        func() error { _, err := assocAction("DEST_ASSOC", "", "6", "1", "R", "", "", "", "", ""); return err },
		"no sender name": func() error {
			_, err := assocAction("SRCE_ASSOC_IP", "7", "", "1", "R", "", "", "", "", "")
			return err
		},
	} {
		if call() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
