package updater

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const Repo = "Alice088/worm"

var APIBase = "https://api.github.com/repos/" + Repo

var DownloadBase = "https://github.com/" + Repo + "/releases/download"

var ErrUnsupported = errors.New("self-update is supported on linux and darwin only")

var ErrNoReleases = errors.New("no releases published yet")

var httpClient = &http.Client{Timeout: 90 * time.Second}

type Release struct {
	Tag   string
	Name  string
	Notes string
	URL   string
}

func Latest(ctx context.Context) (string, error) {
	release, err := LatestRelease(ctx)
	if err != nil {
		return "", err
	}
	return release.Tag, nil
}

func LatestRelease(ctx context.Context) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, APIBase+"/releases/latest", nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return Release{}, ErrNoReleases
	}
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("github api: %s", resp.Status)
	}

	var payload struct {
		TagName string `json:"tag_name"`
		Name    string `json:"name"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return Release{}, err
	}
	if payload.TagName == "" {
		return Release{}, errors.New("github api: empty tag name")
	}
	return Release{Tag: payload.TagName, Name: payload.Name, Notes: payload.Body, URL: payload.HTMLURL}, nil
}

func AssetName(version string) (string, error) {
	goos, goarch, err := platform()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("worm_%s_%s_%s.tar.gz", strings.TrimPrefix(version, "v"), goos, goarch), nil
}

func Install(ctx context.Context, version string) (string, error) {
	target, err := SelfPath()
	if err != nil {
		return "", err
	}
	asset, err := AssetName(version)
	if err != nil {
		return "", err
	}
	base := fmt.Sprintf("%s/%s/", DownloadBase, version)

	workDir, err := os.MkdirTemp("", "worm-update-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	archivePath := filepath.Join(workDir, asset)
	if err := download(ctx, base+asset, archivePath); err != nil {
		return "", err
	}
	sumsPath := filepath.Join(workDir, "checksums.txt")
	if err := download(ctx, base+"checksums.txt", sumsPath); err != nil {
		return "", err
	}
	if err := verifyChecksum(sumsPath, asset, archivePath); err != nil {
		return "", err
	}

	binaryPath := filepath.Join(workDir, "worm")
	if err := extract(archivePath, "worm", binaryPath); err != nil {
		return "", err
	}
	if err := replaceFile(binaryPath, target); err != nil {
		return "", err
	}
	return target, nil
}

func SelfPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

func Uninstall() error {
	target, err := SelfPath()
	if err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	server := filepath.Join(filepath.Dir(target), "worm-server")
	if err := os.Remove(server); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func platform() (string, string, error) {
	switch runtime.GOOS {
	case "linux", "darwin":
	default:
		return "", "", ErrUnsupported
	}
	switch runtime.GOARCH {
	case "amd64", "arm64":
	default:
		return "", "", fmt.Errorf("unsupported architecture: %s", runtime.GOARCH)
	}
	return runtime.GOOS, runtime.GOARCH, nil
}

func download(ctx context.Context, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return err
	}
	return file.Close()
}

func verifyChecksum(sumsPath, asset, archivePath string) error {
	data, err := os.ReadFile(sumsPath)
	if err != nil {
		return err
	}
	expected := ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == asset {
			expected = strings.ToLower(fields[0])
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("checksum for %s not found", asset)
	}

	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}

func extract(archivePath, name, dest string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%s not found in archive", name)
		}
		if err != nil {
			return err
		}
		if filepath.Base(header.Name) != name || header.Typeflag != tar.TypeReg {
			continue
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	}
}

func replaceFile(src, dst string) error {
	tmp := fmt.Sprintf("%s.new.%d", dst, os.Getpid())
	if err := copyFile(src, tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
