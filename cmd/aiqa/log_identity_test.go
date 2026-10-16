package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileGenerationStaysStableAcrossAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "follow.log")
	if err := os.WriteFile(path, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	identity := fileGeneration{}
	want := identity.next(before)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("second\n"); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := identity.next(after); got != want {
		t.Fatalf("append changed identity: got %q, want %q", got, want)
	}
}

func TestFileGenerationChangesAfterReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "follow.log")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	identity := fileGeneration{}
	old := identity.next(before)

	replacement := filepath.Join(dir, "replacement.log")
	if err := os.WriteFile(replacement, []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := identity.next(after); got == old {
		t.Fatalf("replacement retained identity %q", got)
	}
}
