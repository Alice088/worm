package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom missing: %v", err)
	}
	if cfg.Name != "" {
		t.Fatalf("expected empty name, got %q", cfg.Name)
	}
	if cfg.Path() != path {
		t.Fatalf("expected path %q, got %q", path, cfg.Path())
	}
}

func TestSaveThenLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)

	if err := (Config{}).WithName("alice").SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Name != "alice" {
		t.Fatalf("expected name alice, got %q", cfg.Name)
	}
}

func TestSaveThenLoadServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)

	cfg := Config{}.WithName("alice").WithServer("http://localhost:8080")
	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	back, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if back.Server != "http://localhost:8080" {
		t.Fatalf("expected server to round-trip, got %q", back.Server)
	}
}

func TestSaveIsAtomicAndOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)

	if err := (Config{}).WithName("first").SaveTo(path); err != nil {
		t.Fatal(err)
	}
	if err := (Config{}).WithName("second").SaveTo(path); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "second" {
		t.Fatalf("expected second, got %q", cfg.Name)
	}

	// No temp files should be left behind.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected only the config file, found %d entries", len(entries))
	}
}

func TestLoadIgnoresCommentsAndUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	body := "# a comment\n\nunknown=value\nname=\"bob\"\n"

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Name != "bob" {
		t.Fatalf("expected bob, got %q", cfg.Name)
	}
}

func TestLoadRejectsMalformedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte("this is not key value\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadFrom(path); err == nil {
		t.Fatal("expected an error for a malformed line")
	}
}

func TestSaveCreatesParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", FileName)

	if err := (Config{}).WithName("x").SaveTo(path); err != nil {
		t.Fatalf("SaveTo nested: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
}

func TestValidateName(t *testing.T) {
	valid := []string{"alice", "a", "Привет", "name with spaces", "a-b_c.1"}
	for _, n := range valid {
		if err := ValidateName(n); err != nil {
			t.Fatalf("expected %q to be valid: %v", n, err)
		}
	}

	invalid := []string{"", "   ", "tab\there", "line\nbreak"}
	for _, n := range invalid {
		if err := ValidateName(n); err == nil {
			t.Fatalf("expected %q to be invalid", n)
		}
	}

	long := make([]rune, 33)
	for i := range long {
		long[i] = 'a'
	}
	if err := ValidateName(string(long)); err == nil {
		t.Fatal("expected a 33-char name to be rejected")
	}
}

func TestValidateNameEmptyReportsErrNoName(t *testing.T) {
	if err := ValidateName(""); !errors.Is(err, ErrNoName) {
		t.Fatalf("expected ErrNoName, got %v", err)
	}
}

func TestDefaultPathUsesWormHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WORM_HOME", dir)

	want := filepath.Join(dir, FileName)
	if got := DefaultPath(); got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestDefaultPathIsIndependentOfWorkingDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WORM_HOME", dir)

	before := DefaultPath()
	t.Chdir(t.TempDir())
	after := DefaultPath()

	if before != after {
		t.Fatalf("config path changed with the working dir: %q -> %q", before, after)
	}
}
