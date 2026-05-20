package local

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubGet replaces httpGet for the duration of the test so we don't hit the
// network. The first call returns body; subsequent calls return the same.
func stubGet(t *testing.T, body string) {
	t.Helper()
	old := httpGet
	t.Cleanup(func() { httpGet = old })
	httpGet = func(ctx context.Context, url string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(body)), nil
	}
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func TestEnsureWeightsDownloadsWhenMissing(t *testing.T) {
	home := t.TempDir()
	body := "fake-weight-bytes"
	stubGet(t, body)

	root, err := EnsureWeights(context.Background(), home, "rfdiffusion", []WeightSpec{
		{URL: "http://example.test/w1.pt", Path: "w1.pt", SHA256: sha256Hex(body)},
	})
	if err != nil {
		t.Fatalf("EnsureWeights: %v", err)
	}
	want := filepath.Join(home, ".fova", "models", "rfdiffusion")
	if root != want {
		t.Errorf("root = %q, want %q", root, want)
	}
	got, err := os.ReadFile(filepath.Join(root, "w1.pt"))
	if err != nil {
		t.Fatalf("read weight: %v", err)
	}
	if string(got) != body {
		t.Errorf("weight contents = %q", string(got))
	}
}

func TestEnsureWeightsSkipsWhenChecksumMatches(t *testing.T) {
	home := t.TempDir()
	body := "cached-weight"

	// Pre-populate the cache.
	root := ModelsRoot(home, "ligandmpnn")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "v_32.pt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	called := false
	old := httpGet
	t.Cleanup(func() { httpGet = old })
	httpGet = func(context.Context, string) (io.ReadCloser, error) {
		called = true
		return io.NopCloser(strings.NewReader("WRONG")), nil
	}

	if _, err := EnsureWeights(context.Background(), home, "ligandmpnn", []WeightSpec{
		{URL: "http://example.test/v_32.pt", Path: "v_32.pt", SHA256: sha256Hex(body)},
	}); err != nil {
		t.Fatalf("EnsureWeights: %v", err)
	}
	if called {
		t.Error("httpGet was called even though the cached file matched the checksum")
	}
}

func TestEnsureWeightsFailsOnChecksumMismatch(t *testing.T) {
	home := t.TempDir()
	stubGet(t, "actual-bytes")

	_, err := EnsureWeights(context.Background(), home, "boltz2", []WeightSpec{
		{URL: "http://example.test/m.pt", Path: "m.pt", SHA256: sha256Hex("expected-bytes")},
	})
	if err == nil {
		t.Fatal("EnsureWeights: expected checksum mismatch error")
	}
	// The bad file must not be left on disk.
	if _, err := os.Stat(filepath.Join(home, ".fova", "models", "boltz2", "m.pt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("bad weight file should be removed; got err = %v", err)
	}
}

func TestEnsureWeightsAllowsEmptyChecksum(t *testing.T) {
	home := t.TempDir()
	stubGet(t, "anything")

	if _, err := EnsureWeights(context.Background(), home, "chai1", []WeightSpec{
		{URL: "http://example.test/m.pt", Path: "m.pt"},
	}); err != nil {
		t.Fatalf("EnsureWeights: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".fova", "models", "chai1", "m.pt")); err != nil {
		t.Errorf("file should exist: %v", err)
	}
}
