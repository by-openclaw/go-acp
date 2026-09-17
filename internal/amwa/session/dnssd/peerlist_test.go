package dnssd

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePeerList_ValidRows(t *testing.T) {
	body := `# comment
10.0.0.1,8080
node.local,80,v1.3


# trailing comment
[fe80::1],8235,v1.2
`
	entries, err := parsePeerList(bufio.NewScanner(strings.NewReader(body)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("want 3 entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].Host != "10.0.0.1" || entries[0].Port != 8080 || entries[0].APIVer != "" {
		t.Errorf("entry[0]: %+v", entries[0])
	}
	if entries[1].Host != "node.local" || entries[1].Port != 80 || entries[1].APIVer != "v1.3" {
		t.Errorf("entry[1]: %+v", entries[1])
	}
	if entries[2].Host != "[fe80::1]" || entries[2].Port != 8235 || entries[2].APIVer != "v1.2" {
		t.Errorf("entry[2]: %+v", entries[2])
	}
}

func TestParsePeerList_RejectsBadRows(t *testing.T) {
	cases := []string{
		"only-one-field\n",
		",8080\n",
		"host,bad-port\n",
		"host,0\n",
		"host,99999\n",
		"host,80,extra,extra\n",
	}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			if _, err := parsePeerList(bufio.NewScanner(strings.NewReader(body))); err == nil {
				t.Fatalf("expected error on %q", body)
			}
		})
	}
}

func TestReadPeerList_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "peers.csv")
	body := "host-a,8080\nhost-b,8081,v1.3\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, err := ReadPeerList(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
}

func TestReadPeerList_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "peers.csv")
	if err := os.WriteFile(path, []byte("# only comments\n\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ReadPeerList(path); !errors.Is(err, ErrEmptyPeerList) {
		t.Fatalf("expected ErrEmptyPeerList, got %v", err)
	}
}

func TestReadPeerList_MissingFile(t *testing.T) {
	if _, err := ReadPeerList(filepath.Join(t.TempDir(), "missing.csv")); err == nil {
		t.Fatalf("expected error on missing file")
	}
}

// TestReadPeerList_ParseError covers the arm where the file opens fine
// but a row is malformed: ReadPeerList must wrap the parse error (rather
// than return ErrEmptyPeerList or nil).
func TestReadPeerList_ParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "peers.csv")
	if err := os.WriteFile(path, []byte("host,not-a-port\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := ReadPeerList(path)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if errors.Is(err, ErrEmptyPeerList) {
		t.Fatalf("parse error misreported as empty-list: %v", err)
	}
}

// TestParsePeerList_ScannerError covers the bufio.Scanner error arm: a
// single line longer than bufio.MaxScanTokenSize (64 KiB) makes Scan
// abort with ErrTooLong, which parsePeerList must surface via s.Err().
func TestParsePeerList_ScannerError(t *testing.T) {
	huge := strings.Repeat("a", bufio.MaxScanTokenSize+1) // no newline -> one over-long token
	if _, err := parsePeerList(bufio.NewScanner(strings.NewReader(huge))); err == nil {
		t.Fatal("expected scanner error on an over-long line")
	}
}
