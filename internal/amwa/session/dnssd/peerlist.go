// Package dnssd is the session layer that wraps the codec/dnssd
// pure-bytes layer with real network I/O: mDNS responder + browser,
// unicast DNS-SD resolver, and the static peer-list reader for the
// "no DNS-SD at all" deployment mode (Lawo VSM, air-gapped plants).
//
// Layer 2 per internal/amwa/docs/dependencies.md — depends on Layer 1
// codec only; never imports Layer 3 plugin packages.
package dnssd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// PeerListEntry is one row of a CSV peer file (Mode C — direct-Node).
//
// CSV format:
//
//	host,port[,api_ver]
//
// Lines starting with `#` and blank lines are skipped. APIVer defaults
// to empty (caller picks the highest version it supports).
type PeerListEntry struct {
	Host   string
	Port   int
	APIVer string
}

// ErrEmptyPeerList is returned by ReadPeerList when the file parses
// without errors but contains no usable rows.
var ErrEmptyPeerList = errors.New("dnssd: peer list contained no entries")

// ReadPeerList parses a CSV peer file from disk.
func ReadPeerList(path string) ([]PeerListEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("dnssd: open peer list %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	out, err := parsePeerList(bufio.NewScanner(f))
	if err != nil {
		return nil, fmt.Errorf("dnssd: parse peer list %q: %w", path, err)
	}
	if len(out) == 0 {
		return nil, ErrEmptyPeerList
	}
	return out, nil
}

func parsePeerList(s *bufio.Scanner) ([]PeerListEntry, error) {
	var out []PeerListEntry
	lineNo := 0
	for s.Scan() {
		lineNo++
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 2 || len(fields) > 3 {
			return nil, fmt.Errorf("line %d: want 2 or 3 fields, got %d", lineNo, len(fields))
		}
		host := strings.TrimSpace(fields[0])
		if host == "" {
			return nil, fmt.Errorf("line %d: empty host", lineNo)
		}
		port, err := strconv.Atoi(strings.TrimSpace(fields[1]))
		if err != nil || port <= 0 || port > 65535 {
			return nil, fmt.Errorf("line %d: bad port %q", lineNo, fields[1])
		}
		entry := PeerListEntry{Host: host, Port: port}
		if len(fields) == 3 {
			entry.APIVer = strings.TrimSpace(fields[2])
		}
		out = append(out, entry)
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
