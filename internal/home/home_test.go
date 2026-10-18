package home

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirDefaultsToHome(t *testing.T) {
	t.Setenv("WORM_HOME", "")

	base, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}

	got, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, DirName); got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestWormHomeOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WORM_HOME", dir)

	got, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Fatalf("expected override %q, got %q", dir, got)
	}
}

func TestEnsureCreatesLayout(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WORM_HOME", dir)

	if _, err := Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	for _, rel := range []string{
		filepath.Join("assets", AssetFile),
		ConfigFile,
	} {
		path := filepath.Join(dir, rel)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("expected %s to exist: %v", rel, err)
		}
		if info.IsDir() {
			t.Fatalf("expected %s to be a file", rel)
		}
	}
}

func TestEnsureIsIdempotentAndPreservesUserEdits(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WORM_HOME", dir)

	if _, err := Ensure(); err != nil {
		t.Fatal(err)
	}

	// A user customizes both files.
	assetPath := filepath.Join(dir, "assets", AssetFile)
	cfgPath := filepath.Join(dir, ConfigFile)

	if err := os.WriteFile(assetPath, []byte("custom art"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("name=mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Ensure(); err != nil {
		t.Fatal(err)
	}

	gotAsset, _ := os.ReadFile(assetPath)
	if string(gotAsset) != "custom art" {
		t.Fatalf("asset was overwritten: %q", gotAsset)
	}
	gotCfg, _ := os.ReadFile(cfgPath)
	if string(gotCfg) != "name=mine\n" {
		t.Fatalf("config was overwritten: %q", gotCfg)
	}
}

func TestLoadAssetInstallsDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WORM_HOME", dir)

	got, err := LoadAsset()
	if err != nil {
		t.Fatalf("LoadAsset: %v", err)
	}
	if strings.TrimSpace(got) == "" {
		t.Fatal("expected non-empty default asset")
	}
	if got != DefaultAsset() {
		t.Fatal("expected the installed asset to equal the compiled-in default")
	}
}

func TestLoadAssetReturnsUserFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WORM_HOME", dir)

	if _, err := Ensure(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "assets", AssetFile)
	if err := os.WriteFile(path, []byte("my own logo"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadAsset()
	if err != nil {
		t.Fatalf("LoadAsset: %v", err)
	}
	if got != "my own logo" {
		t.Fatalf("expected user asset, got %q", got)
	}
}

func TestPathsMatchLayout(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WORM_HOME", dir)

	cfg, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, ConfigFile); cfg != want {
		t.Fatalf("ConfigPath: expected %q, got %q", want, cfg)
	}

	asset, err := AssetPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "assets", AssetFile); asset != want {
		t.Fatalf("AssetPath: expected %q, got %q", want, asset)
	}
}
