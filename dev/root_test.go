package dev

import (
	"os"
	"path/filepath"
	"testing"
)

// root/web/terminal.ts plus a nested dir to run from, mirroring the real
// layout (checkout/cmd/demo)
func fakeCheckout(t *testing.T) (root, nested string) {
	t.Helper()
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "web", "terminal.ts"), []byte("// entry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested = filepath.Join(root, "cmd", "demo")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	return root, nested
}

// resolves symlinks so comparisons survive macOS /var -> /private/var
func realpath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSourceRootFoundFromNestedDir(t *testing.T) {
	root, nested := fakeCheckout(t)
	t.Chdir(nested)
	got, err := SourceRoot()
	if err != nil {
		t.Fatalf("SourceRoot from %s: %v", nested, err)
	}
	if realpath(t, got) != realpath(t, root) {
		t.Fatalf("SourceRoot = %s, want %s", got, root)
	}
	entry, err := ClientEntry()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(entry); err != nil {
		t.Fatalf("ClientEntry %s does not exist: %v", entry, err)
	}
}

func TestResolveEntryFallsBackAndPassesThrough(t *testing.T) {
	root, nested := fakeCheckout(t)
	t.Chdir(nested)

	// the old hardcoded path doesn't resolve from here, the fallback must find
	// the checkout's entry instead of failing
	got, err := resolveEntry("../web/terminal.ts")
	if err != nil {
		t.Fatalf("resolveEntry fallback: %v", err)
	}
	if realpath(t, got) != realpath(t, filepath.Join(root, "web", "terminal.ts")) {
		t.Fatalf("resolveEntry = %s, want the checkout entry", got)
	}

	// an entry that DOES exist is used verbatim, no surprise substitution
	local := filepath.Join(nested, "custom.ts")
	if err := os.WriteFile(local, []byte("// custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveEntry("custom.ts"); err != nil || got != "custom.ts" {
		t.Fatalf("resolveEntry(existing) = %q, %v; want it unchanged", got, err)
	}
}

func TestSourceRootErrorsOutsideACheckout(t *testing.T) {
	t.Chdir(t.TempDir())
	if root, err := SourceRoot(); err == nil {
		t.Fatalf("SourceRoot outside a checkout returned %q, want an error", root)
	}
}
