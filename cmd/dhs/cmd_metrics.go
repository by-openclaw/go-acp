// runMetrics implements `dhs metrics <verb> [flags]`.
// Verbs:
//   export   fetch a snapshot from a running producer (/snapshot.json)
//            and render it as CSV or Markdown.
//   show     fetch the snapshot and print a Task-Manager-style
//            summary to stderr.
//
// Both verbs rely on the producer having been started with
// --metrics-addr :PORT so there is an HTTP endpoint to hit.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"acp/internal/metrics"
)

func runMetrics(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: dhs metrics <verb>\nverbs: export | show")
	}
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "export":
		return runMetricsExport(ctx, rest)
	case "show":
		return runMetricsShow(ctx, rest)
	default:
		return fmt.Errorf("unknown metrics verb %q", verb)
	}
}

// snapshotPayload mirrors what the producer's /snapshot.json emits.
type snapshotPayload struct {
	Connector metrics.Snapshot        `json:"connector"`
	Process   metrics.ProcessSnapshot `json:"process"`
	Labels    map[string]string       `json:"labels"`
}

func fetchSnapshot(ctx context.Context, url string, timeout time.Duration) (snapshotPayload, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return snapshotPayload{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return snapshotPayload{}, fmt.Errorf("GET %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return snapshotPayload{}, fmt.Errorf("GET %s: %s: %s", url, resp.Status, string(b))
	}
	var p snapshotPayload
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return snapshotPayload{}, fmt.Errorf("decode snapshot: %w", err)
	}
	return p, nil
}

func runMetricsExport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("metrics export", flag.ContinueOnError)
	var (
		url     = fs.String("url", "http://127.0.0.1:9100/snapshot.json", "producer snapshot URL")
		format  = fs.String("format", "md", "output format: csv | md")
		outPath = fs.String("file", "", "output file (default stdout)")
		timeout = fs.Duration("timeout", 5*time.Second, "HTTP request timeout")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	payload, err := fetchSnapshot(ctx, *url, *timeout)
	if err != nil {
		return err
	}

	var w io.Writer = os.Stdout
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		w = f
	}
	switch *format {
	case "csv":
		return metrics.WriteCSV(w, payload.Connector, payload.Process, payload.Labels)
	case "md", "markdown":
		return metrics.WriteMarkdown(w, payload.Connector, payload.Process, payload.Labels)
	default:
		return fmt.Errorf("unknown --format %q (want csv | md)", *format)
	}
}

func runMetricsShow(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("metrics show", flag.ContinueOnError)
	var (
		url     = fs.String("url", "http://127.0.0.1:9100/snapshot.json", "producer snapshot URL")
		timeout = fs.Duration("timeout", 5*time.Second, "HTTP request timeout")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	payload, err := fetchSnapshot(ctx, *url, *timeout)
	if err != nil {
		return err
	}
	return metrics.WriteMarkdown(os.Stdout, payload.Connector, payload.Process, payload.Labels)
}
