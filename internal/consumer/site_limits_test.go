package consumer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ADR-0036 built (ADR-0037 P7): a site limit narrows a factory limit,
// never widens it, and a refusal names both ranges.
func TestSiteLimits(t *testing.T) {
	file := filepath.Join(t.TempDir(), "site-limits.csv")
	_ = os.WriteFile(file, []byte("path,min,max,allowed\n"+
		".PortAes67Input.Channels,1,8,\n"+
		"net.1.node.61.port.1024.in.PortAes67Input.Channels,2,4,\n"+
		".PortAes67Output.Protocol,,,2|5\n"+
		".Ptp.PtpPriority,0,300,\n"+
		".PacketTime,,,125|250\n"), 0o644)
	s, err := LoadSiteLimits(file)
	if err != nil {
		t.Fatal(err)
	}
	if s.Len() != 5 {
		t.Fatalf("%d rules", s.Len())
	}
	// The exact path wins over the suffix.
	if r := s.For("net.1.node.61.port.1024.in.PortAes67Input.Channels"); r == nil || *r.Max != 4 {
		t.Errorf("exact rule: %+v", r)
	}
	if r := s.For("net.1.node.61.port.1025.in.PortAes67Input.Channels"); r == nil || *r.Max != 8 {
		t.Errorf("suffix rule: %+v", r)
	}
	if s.For("net.1.node.61.port.1025.in.Label") != nil {
		t.Error("a rule applied to a path without one")
	}
	factory := &Object{Kind: KindInt, Min: int64(1), Max: int64(16)}
	isValidation := func(err error, want string) bool {
		var v *ValidationError
		return errors.As(err, &v) && strings.Contains(err.Error(), want)
	}
	ch := "net.1.node.61.port.1025.in.PortAes67Input.Channels"
	if err := s.Check(ch, "8", factory); err != nil {
		t.Errorf("8 within the site limit: %v", err)
	}
	if err := s.Check(ch, "12", factory); !isValidation(err, "refused by the site limit (site 1..8, factory 1..16)") {
		t.Errorf("12 above the site limit: %v", err)
	}
	if err := s.Check(ch, "eight", factory); !isValidation(err, "is not a number") {
		t.Errorf("not a number: %v", err)
	}
	// A rule wider than the factory limit is refused as such.
	if err := s.Check("net.1.node.60.card.1.Ptp.PtpPriority", "100", &Object{Min: int64(0), Max: int64(255)}); !isValidation(err, "wider than the factory limit (site 0..300, factory 0..255)") {
		t.Errorf("wider rule: %v", err)
	}
	// Allowed values, by key or by name, within the factory's list.
	proto := "net.1.node.61.port.1025.out.PortAes67Output.Protocol"
	enum := &Object{EnumItems: []string{"2=Manual", "3=RTSP", "5=NMOS"}}
	if err := s.Check(proto, "5", enum); err != nil {
		t.Errorf("5 allowed: %v", err)
	}
	if err := s.Check(proto, "3", enum); !isValidation(err, "refused by the site limit (allowed 2|5)") {
		t.Errorf("3 not allowed: %v", err)
	}
	// A rule that allows what the factory does not list is refused.
	bad := &Object{EnumItems: []string{"333", "1000"}}
	if err := s.Check("x.PacketTime", "250", bad); !isValidation(err, "which the factory does not list") {
		t.Errorf("allowed outside the factory: %v", err)
	}
	// No factory schema known: the site rule alone applies.
	if err := s.Check(ch, "9", nil); !isValidation(err, "refused by the site limit (site 1..8)") {
		t.Errorf("no factory: %v", err)
	}
	// A nil set allows everything; a bad file is refused when loaded.
	var none *SiteLimits
	if err := none.Check(ch, "99", factory); err != nil {
		t.Error("nil limits refused")
	}
	badFile := filepath.Join(t.TempDir(), "bad.csv")
	_ = os.WriteFile(badFile, []byte("path,min,max,allowed\n.X,9,1,\n"), 0o644)
	if _, err := LoadSiteLimits(badFile); err == nil || !strings.Contains(err.Error(), "above max") {
		t.Errorf("min above max: %v", err)
	}
	_ = os.WriteFile(badFile, []byte("label,min\nx,1\n"), 0o644)
	if _, err := LoadSiteLimits(badFile); err == nil || !strings.Contains(err.Error(), "no path column") {
		t.Errorf("no path column: %v", err)
	}
}
