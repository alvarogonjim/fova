package local

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// WeightSpec describes one model-weight file to fetch into the cache.
//
// SHA256, when non-empty, is verified after download; mismatches cause the
// downloaded file to be deleted and EnsureWeights to return an error.
//
// The toml tags let tools.toml declare weight specs inline as an array of
// tables (`[[tools.<name>.weights]] url = "…" path = "…" sha256 = "…"`).
type WeightSpec struct {
	URL    string `toml:"url"`
	Path   string `toml:"path"` // relative to the per-tool cache directory
	SHA256 string `toml:"sha256"`
}

// weightsClient targets multi-GB ckpt transfers. No overall Client.Timeout
// (a 5 GB file at 1 MB/s is legitimately ~85 min), but per-connection and
// idle-read deadlines so a dead TCP stalls fast instead of consuming the
// install budget silently.
var weightsClient = &http.Client{
	Transport: &http.Transport{
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		DisableCompression:    true, // ckpts are already binary
	},
}

// httpGet is the seam tests use to inject a fake HTTP fetcher.
var httpGet = func(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "fova-installer")
	resp, err := weightsClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp.Body, nil
}

// ModelsRoot returns the per-tool weights directory under ~/.fova/models/.
// Callers ensure the directory exists.
func ModelsRoot(home, toolName string) string {
	return filepath.Join(home, ".fova", "models", toolName)
}

// EnsureWeights downloads any missing or checksum-mismatched specs into the
// per-tool cache at ModelsRoot(home, toolName). Files already on disk with a
// matching SHA256 are not re-fetched.
//
// Returns the absolute path to the cache root on success.
func EnsureWeights(ctx context.Context, home, toolName string, specs []WeightSpec) (string, error) {
	root := ModelsRoot(home, toolName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create models cache: %w", err)
	}
	for _, s := range specs {
		dest := filepath.Join(root, s.Path)
		ok, err := verifyChecksum(dest, s.SHA256)
		if err == nil && ok {
			continue
		}
		if err := download(ctx, s.URL, dest); err != nil {
			return "", fmt.Errorf("fetch %s: %w", s.URL, err)
		}
		if s.SHA256 != "" {
			ok, err := verifyChecksum(dest, s.SHA256)
			if err != nil {
				return "", err
			}
			if !ok {
				_ = os.Remove(dest)
				return "", fmt.Errorf("checksum mismatch for %s", s.URL)
			}
		}
	}
	return root, nil
}

func download(ctx context.Context, url, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	body, err := httpGet(ctx, url)
	if err != nil {
		return err
	}
	defer body.Close()

	tmp := dest + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, body); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func verifyChecksum(path, want string) (bool, error) {
	if want == "" {
		// No checksum requested — file existence is the only check.
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	return hex.EncodeToString(h.Sum(nil)) == want, nil
}
