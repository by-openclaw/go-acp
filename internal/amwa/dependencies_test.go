// Package amwa_test enforces the strict 4-layer dependency architecture
// documented in internal/amwa/docs/dependencies.md.
//
// This test runs on every `go test ./internal/amwa/...` and catches what
// the depguard golangci-lint rule might miss in dynamic build configs.
// Both gates are required — depguard fails fast in CI lint stage; this
// test fails the test stage and runs even when golangci-lint isn't
// installed locally.
package amwa_test

import (
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const moduleRoot = "acp"

// TestCodecHasNoAcpImports asserts internal/amwa/codec/** stays
// stdlib-only (sibling codec packages allowed).
func TestCodecHasNoAcpImports(t *testing.T) {
	walkAndCheck(t, "codec", func(t *testing.T, pkgPath string, imports []string) {
		for _, imp := range imports {
			if strings.HasPrefix(imp, moduleRoot+"/internal/amwa/codec/") {
				continue
			}
			if strings.HasPrefix(imp, moduleRoot+"/") {
				t.Errorf("codec package %q imports %q — codec must be stdlib-only", pkgPath, imp)
			}
			// Third-party imports forbidden too. Heuristic: a dotted
			// host (github.com/...) means a third-party module path.
			if strings.Contains(imp, ".") && !strings.HasPrefix(imp, moduleRoot+"/") {
				t.Errorf("codec package %q imports third-party module %q — codec must be stdlib-only", pkgPath, imp)
			}
		}
	})
}

// TestSessionDoesNotImportPlugins asserts internal/amwa/session/**
// never reaches back into the plugin layer or cmd/.
func TestSessionDoesNotImportPlugins(t *testing.T) {
	walkAndCheck(t, "session", func(t *testing.T, pkgPath string, imports []string) {
		for _, imp := range imports {
			if isPluginLayer(imp) {
				t.Errorf("session package %q imports plugin %q (Layer 2 → Layer 3 back-arrow)", pkgPath, imp)
			}
			if strings.HasPrefix(imp, moduleRoot+"/cmd/") {
				t.Errorf("session package %q imports %q (must not depend on cmd/)", pkgPath, imp)
			}
		}
	})
}

// TestNoCrossPluginImports asserts consumer / provider / registry
// never import each other.
func TestNoCrossPluginImports(t *testing.T) {
	for _, plugin := range []string{"consumer", "provider", "registry"} {
		plugin := plugin
		t.Run(plugin, func(t *testing.T) {
			walkAndCheck(t, plugin, func(t *testing.T, pkgPath string, imports []string) {
				for _, imp := range imports {
					if isOtherPlugin(plugin, imp) {
						t.Errorf("%s package %q imports peer plugin %q (cross-plugin coupling forbidden)", plugin, pkgPath, imp)
					}
					if strings.HasPrefix(imp, moduleRoot+"/cmd/") {
						t.Errorf("%s package %q imports %q (must not depend on cmd/)", plugin, pkgPath, imp)
					}
					// Cross-protocol imports forbidden — internal/<other-proto>/*
					if isCrossProtocol(imp) {
						t.Errorf("%s package %q imports %q (cross-protocol leak)", plugin, pkgPath, imp)
					}
				}
			})
		})
	}
}

// walkAndCheck visits every Go package under internal/amwa/<subdir>
// and invokes check with its imports list.
func walkAndCheck(t *testing.T, subdir string, check func(*testing.T, string, []string)) {
	t.Helper()
	root := filepath.Join("internal", "amwa", subdir)
	// The test runs from the package directory `internal/amwa/` so
	// resolve relative-to-module paths.
	moduleDir := mustModuleDir(t)
	absRoot := filepath.Join(moduleDir, root)
	if _, err := os.Stat(absRoot); os.IsNotExist(err) {
		t.Logf("skip %q (not present yet)", absRoot)
		return
	}
	err := filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		pkg, err := build.ImportDir(path, build.IgnoreVendor)
		if err != nil {
			// No Go files in this directory — skip.
			if _, ok := err.(*build.NoGoError); ok {
				return nil
			}
			return err
		}
		// Build a stable package import path for diagnostics.
		rel, _ := filepath.Rel(moduleDir, path)
		pkgPath := moduleRoot + "/" + filepath.ToSlash(rel)
		all := make([]string, 0, len(pkg.Imports)+len(pkg.TestImports)+len(pkg.XTestImports))
		all = append(all, pkg.Imports...)
		all = append(all, pkg.TestImports...)
		all = append(all, pkg.XTestImports...)
		check(t, pkgPath, all)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %q: %v", absRoot, err)
	}
}

// mustModuleDir returns the absolute path of the module root by
// climbing parents until go.mod is found.
func mustModuleDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %q", dir)
		}
		dir = parent
	}
}

func isPluginLayer(importPath string) bool {
	for _, p := range []string{"consumer", "provider", "registry"} {
		if importPath == moduleRoot+"/internal/amwa/"+p {
			return true
		}
		if strings.HasPrefix(importPath, moduleRoot+"/internal/amwa/"+p+"/") {
			return true
		}
	}
	return false
}

func isOtherPlugin(self, importPath string) bool {
	for _, p := range []string{"consumer", "provider", "registry"} {
		if p == self {
			continue
		}
		if importPath == moduleRoot+"/internal/amwa/"+p ||
			strings.HasPrefix(importPath, moduleRoot+"/internal/amwa/"+p+"/") {
			return true
		}
	}
	return false
}

// isCrossProtocol reports whether importPath points into another
// per-protocol tree — e.g. acp/internal/probel-sw08p/*.
func isCrossProtocol(importPath string) bool {
	if !strings.HasPrefix(importPath, moduleRoot+"/internal/") {
		return false
	}
	rest := strings.TrimPrefix(importPath, moduleRoot+"/internal/")
	// Allowed neutral infrastructure prefixes.
	for _, neutral := range []string{
		"protocol",
		"provider",
		"registry",
		"storage",
		"metrics",
		"transport",
		"export",
		"scenario",
		"amwa",
	} {
		if rest == neutral || strings.HasPrefix(rest, neutral+"/") {
			return false
		}
	}
	return true
}
