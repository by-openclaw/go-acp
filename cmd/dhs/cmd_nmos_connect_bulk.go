package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"dhs/internal/amwa/codec/is05"
	"dhs/internal/amwa/consumer"
)

// A salvo on the command line: `--route <receiver>=<sender>` as often
// as needed, and/or `--routes <file>` holding one `receiver,sender`
// per line. An empty sender disconnects that receiver. Ids, not
// labels, for the reason the single verb gives.

// parseRoute reads one `<receiver>=<sender>` argument.
func parseRoute(arg string) (receiver, sender string, err error) {
	receiver, sender, ok := strings.Cut(arg, "=")
	receiver, sender = strings.TrimSpace(receiver), strings.TrimSpace(sender)
	if !ok || receiver == "" {
		return "", "", fmt.Errorf("--route %q: want <receiver-uuid>=<sender-uuid> (an empty sender disconnects)", arg)
	}
	return receiver, sender, nil
}

// readRoutes reads a routes file: `receiver,sender` per line, blank
// lines and `#` comments skipped, an optional `receiver,sender` header
// tolerated. A line with no comma, or with an empty sender, disconnects
// the receiver it names.
func readRoutes(r io.Reader) ([][2]string, error) {
	var out [][2]string
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		receiver, sender, _ := strings.Cut(line, ",")
		receiver, sender = strings.TrimSpace(receiver), strings.TrimSpace(sender)
		if strings.EqualFold(receiver, "receiver") {
			continue // header
		}
		if receiver == "" {
			return nil, fmt.Errorf("line %d: no receiver", n)
		}
		out = append(out, [2]string{receiver, sender})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// bulkRequests assembles the salvo from both sources, flags first.
func bulkRequests(routeFlags []string, routesFile, senderNode, mode, when string, force bool) ([]consumer.ConnectRequest, error) {
	var pairs [][2]string
	for _, arg := range routeFlags {
		receiver, sender, err := parseRoute(arg)
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, [2]string{receiver, sender})
	}
	if routesFile != "" {
		f, err := os.Open(routesFile)
		if err != nil {
			return nil, fmt.Errorf("--routes: %w", err)
		}
		defer func() { _ = f.Close() }()
		fromFile, err := readRoutes(f)
		if err != nil {
			return nil, fmt.Errorf("--routes %s: %w", routesFile, err)
		}
		pairs = append(pairs, fromFile...)
	}
	reqs := make([]consumer.ConnectRequest, len(pairs))
	for i, p := range pairs {
		reqs[i] = consumer.ConnectRequest{
			ReceiverID: p[0],
			SenderID:   p[1],
			SenderNode: senderNode,
			Mode:       is05.ActivationMode(mode),
			When:       when,
			Force:      force,
		}
	}
	return reqs, nil
}

// bulkReport renders the salvo's outcome, one line per route, and
// returns an error when any route was not applied — a salvo that half
// landed must not exit 0.
func bulkReport(reqs []consumer.ConnectRequest, res *consumer.BulkConnectResult, dryRun bool) (string, error) {
	var w strings.Builder
	sender := func(i int) string {
		if reqs[i].SenderID == "" {
			return "(disconnect)"
		}
		return reqs[i].SenderID
	}
	if dryRun {
		fmt.Fprintf(&w, "DRY RUN — nothing was sent\n\n")
		for i, rt := range res.Routes {
			cur := "(none)"
			if rt.CurrentSenderID != nil {
				cur = *rt.CurrentSenderID
			}
			body, _ := json.MarshalIndent(rt.Patch, "    ", "  ")
			fmt.Fprintf(&w, "route %d  %s <- %s\n  via        %s/bulk/receivers\n  currently  sender=%s master_enable=%t\n  params     %s\n",
				i+1, rt.ReceiverID, sender(i), rt.Endpoint, cur, rt.CurrentMasterEnable, body)
		}
		return w.String(), nil
	}
	for i, rt := range res.Routes {
		verdict := "OK  "
		if !rt.OK() {
			verdict = "FAIL"
		}
		fmt.Fprintf(&w, "%s %3d  %s <- %s", verdict, rt.Code, rt.ReceiverID, sender(i))
		if rt.Error != "" {
			fmt.Fprintf(&w, "  (%s)", rt.Error)
		}
		w.WriteString("\n")
	}
	failed := res.Failed()
	fmt.Fprintf(&w, "\n%d routes in %d bulk requests: %d applied, %d not applied\n",
		len(res.Routes), res.Requests, len(res.Routes)-failed, failed)
	if failed > 0 {
		return w.String(), fmt.Errorf("nmos connect: %d of %d routes were not applied", failed, len(res.Routes))
	}
	return w.String(), nil
}

// runNMOSConnectBulk is the salvo half of `dhs consumer nmos connect`.
func runNMOSConnectBulk(ctx context.Context, c *consumer.Controller, reqs []consumer.ConnectRequest, dryRun bool) error {
	res, err := c.ConnectBulk(ctx, reqs, dryRun)
	if err != nil {
		return err
	}
	text, err := bulkReport(reqs, res, dryRun)
	fmt.Print(text)
	return err
}
