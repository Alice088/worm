// Package home owns the worm home directory (~/.worm) and everything stored
// inside it: the user config and the branding assets.
//
// Layout:
//
//	~/.worm/
//	  config        # name=<display name>
//	  assets/worm   # ASCII-art branding
//
// The directory is created on first use, and the default assets are installed
// from the binary so the app never depends on the current working directory.
package home

import (
	_ "embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// DirName is the name of the worm home directory inside the user's home.
const DirName = ".worm"

// ConfigFile is the config file name inside the home directory.
const ConfigFile = "config"

// AssetFile is the branding asset name inside the assets directory.
const AssetFile = "worm"

// assetSubdir is the sub-directory holding user-replaceable assets.
const assetSubdir = "assets"

// defaultAsset is compiled in so a fresh install has branding immediately.
//
//go:embed worm.txt
var defaultAsset []byte

// Dir returns the worm home directory path (~/.worm).
func Dir() (string, error) {
	// Override wins, mostly for tests and portable installs.
	if override := os.Getenv("WORM_HOME"); override != "" {
		return override, nil
	}

	base, err := os.UserHomeDir()
	if err != nil || base == "" {
		return "", errors.New("cannot determine home directory")
	}
	return filepath.Join(base, DirName), nil
}

// ConfigPath returns the path of the config file.
func ConfigPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ConfigFile), nil
}

// AssetPath returns the path of the branding asset.
func AssetPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, assetSubdir, AssetFile), nil
}

// Ensure creates the home directory layout if needed. Existing files are left
// untouched, so user edits survive upgrades.
func Ensure() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Join(dir, assetSubdir), 0o755); err != nil {
		return "", err
	}

	assetPath := filepath.Join(dir, assetSubdir, AssetFile)
	if _, err := os.Stat(assetPath); errors.Is(err, fs.ErrNotExist) {
		if err := os.WriteFile(assetPath, defaultAsset, 0o644); err != nil {
			return "", err
		}
	}

	cfgPath := filepath.Join(dir, ConfigFile)
	if _, err := os.Stat(cfgPath); errors.Is(err, fs.ErrNotExist) {
		// An empty file lets the config package parse cleanly and pick a name
		// from the environment until the user sets one.
		if err := os.WriteFile(cfgPath, []byte("# worm config\n"), 0o644); err != nil {
			return "", err
		}
	}

	return dir, nil
}

// LoadAsset returns the branding asset contents, installing defaults first if
// the file is missing.
func LoadAsset() (string, error) {
	if _, err := Ensure(); err != nil {
		return "", err
	}

	path, err := AssetPath()
	if err != nil {
		return "", err
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// DefaultAsset returns the compiled-in branding, useful as a fallback.
func DefaultAsset() string { return string(defaultAsset) }
