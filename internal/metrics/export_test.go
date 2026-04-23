package metrics

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestWriteCSV(t *testing.T) {
	conn := NewConnector()
	conn.RegisterCmd(0x02, "rx 002 Crosspoint Connect")
	conn.ObserveCmdRx(0x02, 11)
	conn.ObserveCmdTx(0x04, 12, 50*time.Microsecond)
	conn.SetTreeBytes(999)

	proc := NewProcess()

	var buf bytes.Buffer
	if err := WriteCSV(&buf, conn.Snapshot(), proc.Snapshot(), map[string]string{
		"proto": "probel-sw08p", "role": "provider",
	}); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	out := buf.String()

	for _, s := range []string{
		"ts,metric,labels,value",
		"rx_frames_total",
		"tree_bytes",
		"rx_cmd_hits_total,",
		"cmd_id=2",
		"handler_latency_us_bucket_total",
		"process_heap_alloc_bytes",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("CSV missing %q\n--- output ---\n%s", s, out)
		}
	}
}

func TestWriteMarkdown(t *testing.T) {
	conn := NewConnector()
	conn.RegisterCmd(0x02, "rx 002 Crosspoint Connect")
	conn.ObserveCmdRx(0x02, 11)
	conn.ObserveCmdTx(0x04, 12, 50*time.Microsecond)

	proc := NewProcess()

	var buf bytes.Buffer
	if err := WriteMarkdown(&buf, conn.Snapshot(), proc.Snapshot(), map[string]string{
		"proto": "probel-sw08p",
	}); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
	out := buf.String()

	for _, s := range []string{
		"# dhs metrics snapshot",
		"## Connector — aggregate",
		"## Top commands by hit count",
		"rx 002 Crosspoint Connect",
		"## Process",
		"| Goroutines |",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("MD missing %q", s)
		}
	}
}
