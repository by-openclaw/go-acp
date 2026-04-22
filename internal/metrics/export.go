package metrics

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// WriteCSV renders a Snapshot (+ optional process snapshot) as a
// CSV. Header: ts,metric,labels,value. One row per metric family ×
// label combo. Suitable for spreadsheet ingest or long-term archive.
func WriteCSV(w io.Writer, s Snapshot, p ProcessSnapshot, labels map[string]string) error {
	if _, err := io.WriteString(w, "ts,metric,labels,value\n"); err != nil {
		return err
	}
	ts := time.Now().Format(time.RFC3339Nano)
	baseLabels := formatLabels(labels)
	row := func(metric, extra string, value string) error {
		labelStr := baseLabels
		if extra != "" {
			if labelStr != "" {
				labelStr += ";"
			}
			labelStr += extra
		}
		_, err := fmt.Fprintf(w, "%s,%s,%s,%s\n", ts, metric, labelStr, value)
		return err
	}

	// Aggregate counters.
	for _, pair := range []struct {
		metric string
		value  uint64
	}{
		{"rx_frames_total", s.RxFrames},
		{"tx_frames_total", s.TxFrames},
		{"rx_bytes_total", s.RxBytes},
		{"tx_bytes_total", s.TxBytes},
		{"decode_errors_total", s.DecodeErrors},
		{"naks_total", s.NAKs},
		{"timeouts_total", s.Timeouts},
		{"reconnects_total", s.Reconnects},
		{"memory_bytes", s.EstimatedBytes},
		{"tree_bytes", s.TreeBytes},
		{"pool_bytes", s.PoolBytes},
		{"inflight_bytes", s.InflightBytes},
		{"disk_bytes", s.DiskBytes},
	} {
		if err := row(pair.metric, "", fmt.Sprintf("%d", pair.value)); err != nil {
			return err
		}
	}
	if err := row("cpu_percent", "", fmt.Sprintf("%.4f", s.CPUPercent)); err != nil {
		return err
	}
	if err := row("uptime_seconds", "", fmt.Sprintf("%.3f", s.Uptime.Seconds())); err != nil {
		return err
	}

	// Per-command rows, skipping unobserved ids.
	for i := 0; i < 256; i++ {
		if s.RxHitsByCmd[i] == 0 && s.TxHitsByCmd[i] == 0 {
			continue
		}
		lbl := fmt.Sprintf("cmd_id=%d;cmd_name=%q", i, s.CmdNames[i])
		if err := row("rx_cmd_hits_total", lbl, fmt.Sprintf("%d", s.RxHitsByCmd[i])); err != nil {
			return err
		}
		if err := row("tx_cmd_hits_total", lbl, fmt.Sprintf("%d", s.TxHitsByCmd[i])); err != nil {
			return err
		}
		if err := row("rx_cmd_bytes_total", lbl, fmt.Sprintf("%d", s.RxBytesByCmd[i])); err != nil {
			return err
		}
		if err := row("tx_cmd_bytes_total", lbl, fmt.Sprintf("%d", s.TxBytesByCmd[i])); err != nil {
			return err
		}
	}

	// Aggregate latency histogram.
	for i, v := range s.LatencyBuckets {
		lbl := fmt.Sprintf("le_us=%d", bucketCeilingUs(i))
		if err := row("handler_latency_us_bucket_total", lbl, fmt.Sprintf("%d", v)); err != nil {
			return err
		}
	}

	// Process section (only if non-zero uptime).
	if !p.StartedAt.IsZero() {
		procRow := func(metric, value string) error {
			_, err := fmt.Fprintf(w, "%s,process_%s,,%s\n", ts, metric, value)
			return err
		}
		pairs := []struct {
			metric string
			value  string
		}{
			{"heap_alloc_bytes", fmt.Sprintf("%d", p.HeapAllocBytes)},
			{"heap_sys_bytes", fmt.Sprintf("%d", p.HeapSysBytes)},
			{"heap_inuse_bytes", fmt.Sprintf("%d", p.HeapInuseBytes)},
			{"heap_idle_bytes", fmt.Sprintf("%d", p.HeapIdleBytes)},
			{"heap_released_bytes", fmt.Sprintf("%d", p.HeapReleasedBytes)},
			{"stack_inuse_bytes", fmt.Sprintf("%d", p.StackInuseBytes)},
			{"total_alloc_bytes", fmt.Sprintf("%d", p.TotalAllocBytes)},
			{"mallocs", fmt.Sprintf("%d", p.Mallocs)},
			{"frees", fmt.Sprintf("%d", p.Frees)},
			{"num_gc", fmt.Sprintf("%d", p.NumGC)},
			{"pause_total_ns", fmt.Sprintf("%d", p.PauseTotalNs)},
			{"gc_cpu_fraction", fmt.Sprintf("%.6f", p.GCCPUFraction)},
			{"next_gc_bytes", fmt.Sprintf("%d", p.NextGCBytes)},
			{"goroutines", fmt.Sprintf("%d", p.Goroutines)},
			{"gomaxprocs", fmt.Sprintf("%d", p.GOMAXPROCS)},
			{"num_cpu", fmt.Sprintf("%d", p.NumCPU)},
			{"uptime_seconds", fmt.Sprintf("%.3f", p.Uptime.Seconds())},
		}
		for _, pair := range pairs {
			if err := procRow(pair.metric, pair.value); err != nil {
				return err
			}
		}
	}
	return nil
}

// WriteMarkdown renders a Snapshot (+ optional process snapshot) as
// a Markdown document suitable for pasting into PRs or incident
// notes. Groups metrics by family (connector counters, connector
// memory, per-cmd top-N, process).
func WriteMarkdown(w io.Writer, s Snapshot, p ProcessSnapshot, labels map[string]string) error {
	write := func(s string) error {
		_, err := io.WriteString(w, s)
		return err
	}
	if err := write("# dhs metrics snapshot\n\n"); err != nil {
		return err
	}
	if err := write(fmt.Sprintf("_Taken %s — uptime %s_\n\n",
		time.Now().Format(time.RFC3339), s.Uptime.Round(time.Millisecond))); err != nil {
		return err
	}

	if len(labels) > 0 {
		if err := write("## Target\n\n"); err != nil {
			return err
		}
		if err := write("| label | value |\n|---|---|\n"); err != nil {
			return err
		}
		keys := make([]string, 0, len(labels))
		for k := range labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := write(fmt.Sprintf("| %s | %s |\n", k, labels[k])); err != nil {
				return err
			}
		}
		if err := write("\n"); err != nil {
			return err
		}
	}

	if err := write("## Connector — aggregate\n\n"); err != nil {
		return err
	}
	p50, p95, p99 := s.LatencyPercentiles()
	rows := [][2]string{
		{"CPU %", fmt.Sprintf("%.2f", s.CPUPercent)},
		{"Memory (est)", fmt.Sprintf("%d B", s.EstimatedBytes)},
		{"Rx frames", fmt.Sprintf("%d", s.RxFrames)},
		{"Tx frames", fmt.Sprintf("%d", s.TxFrames)},
		{"Rx bytes", fmt.Sprintf("%d", s.RxBytes)},
		{"Tx bytes", fmt.Sprintf("%d", s.TxBytes)},
		{"Decode errors", fmt.Sprintf("%d", s.DecodeErrors)},
		{"NAKs", fmt.Sprintf("%d", s.NAKs)},
		{"Timeouts", fmt.Sprintf("%d", s.Timeouts)},
		{"Reconnects", fmt.Sprintf("%d", s.Reconnects)},
		{"Latency p50 (µs)", fmt.Sprintf("%d", p50)},
		{"Latency p95 (µs)", fmt.Sprintf("%d", p95)},
		{"Latency p99 (µs)", fmt.Sprintf("%d", p99)},
	}
	if err := write("| metric | value |\n|---|---:|\n"); err != nil {
		return err
	}
	for _, r := range rows {
		if err := write(fmt.Sprintf("| %s | %s |\n", r[0], r[1])); err != nil {
			return err
		}
	}

	top := s.TopCmdsByHits(20)
	if len(top) > 0 {
		if err := write("\n## Top commands by hit count\n\n"); err != nil {
			return err
		}
		if err := write("| cmd id | name | rx hits | tx hits | rx bytes | tx bytes |\n"); err != nil {
			return err
		}
		if err := write("|---:|---|---:|---:|---:|---:|\n"); err != nil {
			return err
		}
		for _, row := range top {
			name := row.Name
			if name == "" {
				name = fmt.Sprintf("cmd 0x%02x", row.ID)
			}
			if err := write(fmt.Sprintf("| %d | %s | %d | %d | %d | %d |\n",
				row.ID, name, row.RxHits, row.TxHits, row.RxBytes, row.TxBytes)); err != nil {
				return err
			}
		}
	}

	if !p.StartedAt.IsZero() {
		if err := write("\n## Process\n\n"); err != nil {
			return err
		}
		procRows := [][2]string{
			{"Heap alloc", fmt.Sprintf("%d B", p.HeapAllocBytes)},
			{"Heap sys", fmt.Sprintf("%d B", p.HeapSysBytes)},
			{"Heap inuse", fmt.Sprintf("%d B", p.HeapInuseBytes)},
			{"Stack inuse", fmt.Sprintf("%d B", p.StackInuseBytes)},
			{"Total alloc (cumulative)", fmt.Sprintf("%d B", p.TotalAllocBytes)},
			{"Goroutines", fmt.Sprintf("%d", p.Goroutines)},
			{"Num GC cycles", fmt.Sprintf("%d", p.NumGC)},
			{"GC pause total", fmt.Sprintf("%d ns", p.PauseTotalNs)},
			{"GC CPU fraction", fmt.Sprintf("%.6f", p.GCCPUFraction)},
			{"GOMAXPROCS", fmt.Sprintf("%d", p.GOMAXPROCS)},
			{"NumCPU", fmt.Sprintf("%d", p.NumCPU)},
			{"Uptime", p.Uptime.Round(time.Millisecond).String()},
		}
		if err := write("| metric | value |\n|---|---:|\n"); err != nil {
			return err
		}
		for _, r := range procRows {
			if err := write(fmt.Sprintf("| %s | %s |\n", r[0], r[1])); err != nil {
				return err
			}
		}
	}
	return nil
}

func formatLabels(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", k, m[k]))
	}
	return strings.Join(parts, ";")
}
