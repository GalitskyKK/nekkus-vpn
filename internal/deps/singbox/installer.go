package singbox

import (
	"archive/zip"
	"context"
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

type Status struct {
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
	Version   string `json:"version,omitempty"`
	Source    string `json:"source,omitempty"` // "env" | "settings" | "path" | "installed"
}

type release struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "sing-box.exe"
	}
	return "sing-box"
}

func defaultInstallDir(dataDir string) string {
	return filepath.Join(dataDir, "tools", "sing-box")
}

func getLatestRelease(ctx context.Context) (*release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/SagerNet/sing-box/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		return nil, fmt.Errorf("github api status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	if rel.TagName == "" {
		return nil, errors.New("missing tag_name in release response")
	}
	return &rel, nil
}

func selectAsset(rel *release) (asset, error) {
	arch := runtime.GOARCH
	osName := runtime.GOOS

	var wantSuffix string
	switch osName {
	case "windows":
		wantSuffix = fmt.Sprintf("windows-%s.zip", arch)
	case "linux":
		wantSuffix = fmt.Sprintf("linux-%s.tar.gz", arch)
	case "darwin":
		wantSuffix = fmt.Sprintf("darwin-%s.tar.gz", arch)
	default:
		return asset{}, fmt.Errorf("unsupported os: %s", osName)
	}

	// prefer non-legacy builds when both exist
	for _, a := range rel.Assets {
		if strings.Contains(a.Name, "legacy") {
			continue
		}
		if strings.HasSuffix(a.Name, wantSuffix) && strings.HasPrefix(a.Name, "sing-box-") && strings.Contains(a.Name, rel.TagName[1:]) {
			return a, nil
		}
	}
	for _, a := range rel.Assets {
		if strings.HasSuffix(a.Name, wantSuffix) {
			return a, nil
		}
	}

	return asset{}, fmt.Errorf("no asset found for %s/%s (%s)", osName, arch, wantSuffix)
}

func downloadToTemp(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		return "", fmt.Errorf("download status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	f, err := os.CreateTemp("", "nekkus-singbox-*")
	if err != nil {
		return "", err
	}
	defer func() {
		_ = f.Close()
		if err != nil {
			_ = os.Remove(f.Name())
		}
	}()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func safeJoin(root, rel string) (string, error) {
	clean := filepath.Clean(rel)
	if clean == "." || clean == string(filepath.Separator) {
		return "", errors.New("empty path")
	}
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("absolute path in archive: %s", rel)
	}
	// zip entries are '/' separated; filepath.Clean will convert, but still check traversal
	if strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("path traversal in archive: %s", rel)
	}
	full := filepath.Join(root, clean)
	if !strings.HasPrefix(full, filepath.Clean(root)+string(filepath.Separator)) && full != filepath.Clean(root) {
		return "", fmt.Errorf("path escapes root: %s", rel)
	}
	return full, nil
}

func extractZip(zipPath, targetDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == "" {
			continue
		}
		dstPath, err := safeJoin(targetDir, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dstPath, 0750); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dstPath), 0750); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			_ = rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		_ = out.Close()
		_ = rc.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

func findFile(root, filename string) (string, error) {
	var found string
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(d.Name(), filename) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	if found == "" {
		return "", fmt.Errorf("%s not found after extract", filename)
	}
	return found, nil
}

func copyDirFiles(srcDir, dstDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dstDir, 0750); err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		srcPath := filepath.Join(srcDir, e.Name())
		dstPath := filepath.Join(dstDir, e.Name())

		in, err := os.Open(srcPath)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		_ = out.Close()
		_ = in.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

func InstallLatest(ctx context.Context, dataDir string) (Status, error) {
	rel, err := getLatestRelease(ctx)
	if err != nil {
		return Status{}, err
	}
	a, err := selectAsset(rel)
	if err != nil {
		return Status{}, err
	}

	archivePath, err := downloadToTemp(ctx, a.BrowserDownloadURL)
	if err != nil {
		return Status{}, err
	}
	defer os.Remove(archivePath)

	extractDir, err := os.MkdirTemp("", "nekkus-singbox-extract-*")
	if err != nil {
		return Status{}, err
	}
	defer os.RemoveAll(extractDir)

	switch {
	case strings.HasSuffix(a.Name, ".zip"):
		if err := extractZip(archivePath, extractDir); err != nil {
			return Status{}, err
		}
	default:
		return Status{}, fmt.Errorf("unsupported archive type: %s", a.Name)
	}

	binPath, err := findFile(extractDir, binaryName())
	if err != nil {
		return Status{}, err
	}
	binDir := filepath.Dir(binPath)

	installDir := defaultInstallDir(dataDir)
	if err := os.MkdirAll(installDir, 0750); err != nil {
		return Status{}, err
	}
	if err := copyDirFiles(binDir, installDir); err != nil {
		return Status{}, err
	}

	finalPath := filepath.Join(installDir, binaryName())
	// make executable on unix
	if runtime.GOOS != "windows" {
		_ = os.Chmod(finalPath, 0755)
	}

	return Status{
		Installed: true,
		Path:      finalPath,
		Version:   strings.TrimPrefix(rel.TagName, "v"),
		Source:    "installed",
	}, nil
}

