//go:build integration

package snmp_integration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dhs/internal/clock"
	dhsc "dhs/internal/consumer"
	"dhs/internal/plugin"
	snmp "dhs/internal/snmp/consumer"
)

// The generator for ADR-0025 deliverable 4's committed DM.
//
// Two facts about this agent decide how the fixture is built, and both
// were measured rather than assumed:
//
//  1. Its model does not fit in a repository. A walk reached 94 181
//     objects and 48 MB, and 97 % of that is two transport-stream
//     tables — 4096 rows × 13 columns of programme streams, plus a
//     42 705-entry DVB subtitle table — which no rule matches, no
//     operator reads, and no test needs a thousand copies of.
//     internal/manifest/TEMPLATE.md says the same: "sane size (no
//     multi-MB blobs — trim to a representative card if a real DM is
//     huge)".
//
//  2. A single walk of it does not FINISH. After about forty minutes
//     the agent stops answering, the walk reports "the model is not
//     complete" and keeps what it got — and what it got stops partway
//     through those tables, so everything after them in OID order
//     (`Software`, among others) is simply missing.
//
// So the fixture is built branch by branch instead: each top-level
// branch is walked on its own and capped at fixtureRowCap objects per
// parent. Every branch is present, every table keeps its shape, no
// table brings its bulk, and it takes seconds rather than failing after
// forty minutes.
//
// Regenerate after a firmware change:
//
//	SNMP_TEST_HOST=10.6.255.114 SNMP_WRITE_FIXTURE=1 \
//	  go test -tags integration ./internal/snmp/integration/ -run WriteTheCommittedDM -v

// fixtureRowCap is how many objects any one group contributes. 32 keeps
// a table recognisably a table — indices, a repeated column shape, more
// than one row of values — at a fiftieth of the size.
const fixtureRowCap = 32

// fixtureBranches are the branches walked into the fixture: `system`,
// which every agent has, and every branch this device's MIB declares
// under the enterprise arc its sysObjectID names.
//
// Status is named one child at a time rather than whole, and two
// tables are left out on purpose:
//
//	Status.TsDescriptor.Program.Stream          4096 rows × 13 columns
//	…Program.Stream.DvbSubService.Table.Entry   42 705 entries
//
// Those two ARE the 48 MB. They are the only part of this device a
// scoped read cannot make cheap, nothing alarms on them, and
// Program.Table below carries the same table shape at 128 rows. A
// walk that includes them does not finish: the agent stops answering
// after about forty minutes, which is how the first attempt at this
// fixture lost every branch that sorts after them — Software,
// Hardware, Network, Status.Input and the rest.
var fixtureBranches = []string{
	"system",
	"ateme.dr5000.Unit",
	"ateme.dr5000.Software",
	"ateme.dr5000.Hardware",
	"ateme.dr5000.Network",
	"ateme.dr5000.Time",
	"ateme.dr5000.Biss",
	"ateme.dr5000.Preset",
	"ateme.dr5000.Licenses",
	"ateme.dr5000.Snmp",
	"ateme.dr5000.Command",
	"ateme.dr5000.Channel",
	"ateme.dr5000.Status.Input",
	"ateme.dr5000.Status.Decode",
	"ateme.dr5000.Status.Dvbssu",
	"ateme.dr5000.Status.TsDescriptor.Nit",
	"ateme.dr5000.Status.TsDescriptor.TransportStreamId",
	"ateme.dr5000.Status.TsDescriptor.NullBitrate",
	"ateme.dr5000.Status.TsDescriptor.ProgramCount",
	"ateme.dr5000.Status.TsDescriptor.Program.Table",
}

type dmFile struct {
	Model    string        `json:"model"`
	SwRev    string        `json:"sw_rev"`
	Protocol string        `json:"protocol"`
	Objects  []dhsc.Object `json:"objects"`
}

func TestWriteTheCommittedDM(t *testing.T) {
	if os.Getenv("SNMP_WRITE_FIXTURE") == "" {
		t.Skip("set SNMP_WRITE_FIXTURE=1 to regenerate the committed fixture from the agent")
	}
	host := agent(t)

	f := &snmp.Factory{}
	p, ok := f.New(plugin.Deps{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:  clock.System(),
	}).(*snmp.Plugin)
	if !ok {
		t.Fatal("the factory must build a *snmp.Plugin")
	}
	if c := strings.TrimSpace(os.Getenv("SNMP_COMMUNITY")); c != "" {
		p.SetCommunity(c, "")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := p.Connect(ctx, host, 0); err != nil {
		t.Fatalf("connect %s: %v", host, err)
	}
	defer func() { _ = p.Disconnect() }()

	id, err := p.IdentityProbe(ctx, 0)
	if err != nil || id == "" {
		t.Fatalf("the agent must name itself for the DM to be keyed: %q %v", id, err)
	}
	model, swRev, ok := strings.Cut(id, "@")
	if !ok {
		t.Fatalf("identity %q is not Model@SwRev", id)
	}

	dm := dmFile{Model: model, SwRev: swRev, Protocol: "snmp"}
	var read int
	for _, branch := range fixtureBranches {
		objs, werr := p.WalkUnder(ctx, branch)
		if werr != nil {
			t.Fatalf("walk %s: %v", branch, werr)
		}
		if len(objs) == 0 {
			t.Errorf("branch %s came back empty — the fixture would claim this agent has no %s", branch, branch)
		}
		read += len(objs)
		dm.Objects = append(dm.Objects, capGroups(objs, fixtureRowCap)...)
	}

	out := filepath.Join(repoRoot(t), "internal", "snmp", "testdata",
		"integration-test", "dm", "snmp", id+".json")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	// Indented, because the point of committing it is that a reviewer
	// can read the diff.
	body, err := json.MarshalIndent(dm, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, append(body, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %d objects read, %d kept -> %s (%d KiB)", id, read, len(dm.Objects), out, len(body)/1024)
}

// capGroups keeps at most limit objects per parent path, in the order
// the walk produced them — which is OID order, so a capped table is its
// first rows and not an arbitrary sample.
func capGroups(objs []dhsc.Object, limit int) []dhsc.Object {
	seen := make(map[string]int, len(objs))
	kept := make([]dhsc.Object, 0, len(objs))
	for _, o := range objs {
		parent := ""
		if len(o.Path) > 1 {
			parent = strings.Join(o.Path[:len(o.Path)-1], ".")
		}
		if seen[parent] >= limit {
			continue
		}
		seen[parent]++
		kept = append(kept, o)
	}
	return kept
}
