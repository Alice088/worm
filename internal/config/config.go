// Package config loads and persists user settings in the ".worm" file.
//
// The file is stored as ".worm" in the current working directory, and falls
// back to the user's config dir when the working directory is not writable,
// so the app works both from a checkout and from a global install.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FileName is the on-disk name of the config file.
const FileName = ".worm"

// Config is the persisted user state.
type Config struct {
	// Name is the display name shown to other peers.
	Name string

	// path is where the config was loaded from (and will be written to).
	path string
}

// ErrNoName reports that no name has been configured yet.
var ErrNoName = errors.New("no name configured")

// Path returns the file the config is bound to.
func (c Config) Path() string { return c.path }

// Load reads the config from the default location. A missing file is not an
// error: it yields an empty Config with a resolved write path.
func Load() (Config, error) {
	return loadFrom(DefaultPath())
}

// LoadFrom reads the config from an explicit path.
func LoadFrom(path string) (Config, error) {
	return loadFrom(path)
}

// DefaultPath resolves where the config file lives.
func DefaultPath() string {
	// Prefer the working directory so a project-local .worm wins.
	if wd, err := os.Getwd(); err == nil {
		candidate := filepath.Join(wd, FileName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		// No file yet: use the working dir if it is writable.
		if isWritableDir(wd) {
			return candidate
		}
	}
	return filepath.Join(userConfigDir(), "worm", FileName)
}

func userConfigDir() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		home, herr := os.UserHomeDir()
		if herr != nil || home == "" {
			return "."
		}
		return home
	}
	return dir
}

func isWritableDir(dir string) bool {
	f, err := os.CreateTemp(dir, ".worm-write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	_ = os.Remove(name)
	return true
}

func loadFrom(path string) (Config, error) {
	cfg := Config{path: path}

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}

		key, value, ok := strings.Cut(text, "=")
		if !ok {
			return cfg, fmt.Errorf("%s:%d: expected key=value", path, line)
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)

		switch key {
		case "name":
			cfg.Name = value
		}
		// Unknown keys are ignored so newer files stay readable.
	}
	if err := scanner.Err(); err != nil {
		return cfg, err
	}

	return cfg, nil
}

// Save writes the config atomically, creating parent directories as needed.
func (c Config) Save() error {
	return c.saveTo(c.path)
}

// SaveTo writes the config to an explicit path, rebinding the receiver's path.
func (c Config) SaveTo(path string) error {
	return c.saveTo(path)
}

func (c Config) saveTo(path string) error {
	if path == "" {
		return errors.New("config: empty path")
	}

	var b strings.Builder
	b.WriteString("# worm config\n")
	b.WriteString("# name: the display name other peers see\n")
	fmt.Fprintf(&b, "name=%s\n", c.Name)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".worm-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.WriteString(b.String()); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}

	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// WithName returns a copy of the config carrying the given name.
func (c Config) WithName(name string) Config {
	c.Name = strings.TrimSpace(name)
	return c
}

// ValidateName reports whether name is acceptable as a display name.
func ValidateName(name string) error {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return fmt.Errorf("%w: name must not be empty", ErrNoName)
	case len(name) > 32:
		return errors.New("name must be at most 32 characters")
	case strings.ContainsAny(name, "\n\r\t"):
		return errors.New("name must not contain control characters")
	}
	return nil
}
