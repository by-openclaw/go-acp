package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is14"
	"dhs/internal/amwa/consumer"
	"dhs/internal/amwa/session/configuration"
)

func TestConfigReport(t *testing.T) {
	const ep = "http://node:3000/x-nmos/configuration/v1.0"
	msg := "gain clamped"
	for name, tc := range map[string]struct {
		res    *consumer.ConfigResult
		req    consumer.ConfigRequest
		backup string
		want   []string
	}{
		"listing": {
			res:  &consumer.ConfigResult{Endpoint: ep, RolePaths: []string{"root", "root.gain"}},
			want: []string{"role paths via " + ep, "  root.gain"},
		},
		"describing": {
			res: &consumer.ConfigResult{Endpoint: ep, RolePath: "root.gain", PropertyIDs: []string{"1p1", "3p1"},
				MethodIDs: []string{"3m1"}, Descriptor: json.RawMessage(`{"name":"GainControl"}`)},
			req:  consumer.ConfigRequest{RolePath: "root.gain"},
			want: []string{"root.gain via " + ep, "properties: 1p1 3p1", "methods:    3m1", `"name": "GainControl"`},
		},
		"get": {
			res:  &consumer.ConfigResult{Endpoint: ep, RolePath: "root.gain", Value: json.RawMessage(`-6.0`)},
			req:  consumer.ConfigRequest{RolePath: "root.gain", Get: "3p1"},
			want: []string{"root.gain.3p1 = -6.0"},
		},
		"set": {
			res:  &consumer.ConfigResult{Endpoint: ep, RolePath: "root.gain", Value: json.RawMessage(`-3`)},
			req:  consumer.ConfigRequest{RolePath: "root.gain", Set: "3p1", SetValue: json.RawMessage(`-3`)},
			want: []string{"SET root.gain.3p1 via " + ep + ": the Device now holds -3"},
		},
		"dry-run set": {
			res:  &consumer.ConfigResult{Endpoint: ep, RolePath: "root.gain", Value: json.RawMessage(`-6.0`), DryRun: true},
			req:  consumer.ConfigRequest{RolePath: "root.gain", Set: "3p1", SetValue: json.RawMessage(`-3`)},
			want: []string{"DRY RUN", "would set root.gain.3p1 = -3 (the Device holds -6.0)"},
		},
		"invoke": {
			res: &consumer.ConfigResult{Endpoint: ep, RolePath: "root.gain", Value: json.RawMessage(`"reset"`),
				Result: configuration.Result{Status: 200}},
			req:  consumer.ConfigRequest{RolePath: "root.gain", Invoke: "3m1"},
			want: []string{"INVOKED 3m1 on root.gain via " + ep + ": status 200", `value: "reset"`},
		},
		"dry-run invoke": {
			res:  &consumer.ConfigResult{Endpoint: ep, RolePath: "root.gain", DryRun: true},
			req:  consumer.ConfigRequest{RolePath: "root.gain", Invoke: "3m1"},
			want: []string{"would invoke 3m1 on root.gain"},
		},
		"backup to a file": {
			res:    &consumer.ConfigResult{Endpoint: ep, RolePath: "root", Holder: &is14.BulkPropertiesHolder{Values: []is14.ObjectPropertiesHolder{{}, {}}}},
			req:    consumer.ConfigRequest{RolePath: "root", Backup: true},
			backup: "plant.json",
			want:   []string{"BACKUP of root via " + ep + ": 2 object(s) written to plant.json"},
		},
		"restore": {
			res: &consumer.ConfigResult{Endpoint: ep, RolePath: "root", Restored: true,
				Validations: []is14.ObjectPropertiesSetValidation{{
					Path: []string{"root", "gain"}, Status: 200, StatusMessage: &msg,
					Notices: []is14.PropertyRestoreNotice{{Name: "gain", NoticeMessage: "clamped to 12 dB"}},
				}}},
			req:  consumer.ConfigRequest{RolePath: "root", Restore: &is14.BulkPropertiesHolder{}},
			want: []string{"RESTORED root via " + ep, "200  root.gain", "gain: clamped to 12 dB"},
		},
		"validation only": {
			res:  &consumer.ConfigResult{Endpoint: ep, RolePath: "root", Validations: []is14.ObjectPropertiesSetValidation{{Path: []string{"root"}, Status: 400}}},
			req:  consumer.ConfigRequest{RolePath: "root", Restore: &is14.BulkPropertiesHolder{}, ValidateOnly: true},
			want: []string{"VALIDATED (nothing applied) root", "400  root"},
		},
	} {
		got := configReport(tc.res, tc.req, tc.backup)
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Errorf("%s: report lacks %q:\n%s", name, want, got)
			}
		}
	}

	// A backup to stdout prints the data set only: no line of prose in it.
	holder := &consumer.ConfigResult{Endpoint: ep, RolePath: "root", Holder: &is14.BulkPropertiesHolder{}}
	if got := configReport(holder, consumer.ConfigRequest{Backup: true}, "-"); got != "" {
		t.Errorf("a backup to stdout must add nothing, got %q", got)
	}
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backup.json")
	if err := writeFileAtomic(path, []byte(`{"values":[]}`)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != `{"values":[]}` {
		t.Errorf("file = %q, %v", got, err)
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Error("the temporary file was left behind")
	}
	if err := writeFileAtomic(filepath.Join(dir, "no-such-dir", "backup.json"), nil); err == nil {
		t.Error("a path that cannot be written must fail")
	}
}
