package datastore

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// TestProjectRoot pins ADR-0028's root rule: a binary under <X>/bin/
// roots artifacts at <X> so .cache/, snapshots/ and captures/ never
// land inside bin/; any other layout (dropped binary) roots next to
// the binary itself.
func TestProjectRoot(t *testing.T) {
	x := t.TempDir()
	cases := []struct {
		name string
		exe  string
		want string
	}{
		{"bin/ layout roots one level up", filepath.Join(x, "bin", "dhs"), x},
		{"dropped binary roots beside itself", filepath.Join(x, "opt", "dhs.exe"), filepath.Join(x, "opt")},
		{"a directory merely named bin* is not the convention", filepath.Join(x, "binaries", "dhs"), filepath.Join(x, "binaries")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			swapExecutable(t, func() (string, error) { return tc.exe, nil })
			got, err := ProjectRoot()
			if err != nil {
				t.Fatalf("ProjectRoot: %v", err)
			}
			if got != tc.want {
				t.Fatalf("ProjectRoot: got %q want %q", got, tc.want)
			}
			store, err := NewTreeStoreInProjectCache()
			if err != nil {
				t.Fatalf("NewTreeStoreInProjectCache: %v", err)
			}
			if want := filepath.Join(tc.want, ".cache"); store.BaseDir() != want {
				t.Fatalf("cache root: got %q want %q", store.BaseDir(), want)
			}
		})
	}
}

// TestProjectRoot_UnlocatableBinary: when the OS cannot say where the
// binary is there is no sane root to guess, so both entry points fail
// with the cause wrapped rather than rooting artifacts in cwd.
func TestProjectRoot_UnlocatableBinary(t *testing.T) {
	cause := errors.New("readlink /proc/self/exe: permission denied")
	swapExecutable(t, func() (string, error) { return "", cause })
	if _, err := ProjectRoot(); !errors.Is(err, cause) {
		t.Fatalf("ProjectRoot err = %v, want wrapped %v", err, cause)
	}
	store, err := NewTreeStoreInProjectCache()
	if !errors.Is(err, cause) || store != nil {
		t.Fatalf("NewTreeStoreInProjectCache = (%v, %v), want (nil, wrapped %v)", store, err, cause)
	}
}

// TestTreeStore_BaseDir: the accessor is what audit/ and dm/ siblings
// are composed from, and it is nil-safe like IdentityPath.
func TestTreeStore_BaseDir(t *testing.T) {
	dir := t.TempDir()
	if got := NewTreeStore(dir).BaseDir(); got != dir {
		t.Fatalf("BaseDir: got %q want %q", got, dir)
	}
	var nilStore *TreeStore
	if got := nilStore.BaseDir(); got != "" {
		t.Fatalf("nil store BaseDir = %q, want empty", got)
	}
}

// TestSanitizePathSeg pins the shared ADR-0028 segment sanitiser: the
// nine characters illegal on Windows or POSIX filenames become '_',
// everything else (spaces, '@', dots, IPv4 dots) is kept verbatim so
// DM filenames stay recognisable on disk.
func TestSanitizePathSeg(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"plain identity is untouched", "RRS18@1601", "RRS18@1601"},
		{"space and dots survive", "CONVERT Hybrid@6.7.4", "CONVERT Hybrid@6.7.4"},
		{"IPv6 colons become underscores", "fe80::1", "fe80__1"},
		{"IPv4 is already safe", "10.6.239.113", "10.6.239.113"},
		{"every illegal character is replaced", `a/b\c:d*e?f"g<h>i|j`, "a_b_c_d_e_f_g_h_i_j"},
		{"empty stays empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizePathSeg(tc.in)
			if got != tc.want {
				t.Fatalf("SanitizePathSeg(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.ContainsAny(got, `/\:*?"<>|`) {
				t.Fatalf("SanitizePathSeg(%q) = %q still carries an illegal character", tc.in, got)
			}
		})
	}
}
