package viz

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRandom8IsLowercaseHex(t *testing.T) {
	for i := 0; i < 16; i++ {
		got := random8()
		if len(got) != 8 {
			t.Fatalf("random8() = %q, want length 8", got)
		}
		for _, r := range got {
			if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') {
				t.Fatalf("random8() = %q, want lowercase hex", got)
			}
		}
	}
}

func TestRandom8Unique(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 64; i++ {
		seen[random8()] = struct{}{}
	}
	if len(seen) < 60 {
		t.Fatalf("random8() collisions: %d unique in 64 draws", len(seen))
	}
}

func TestOutputPathPlacesUnderDesigns(t *testing.T) {
	ws := t.TempDir()
	got, err := OutputPath(ws, "metric_plot", "png")
	if err != nil {
		t.Fatalf("OutputPath: %v", err)
	}
	wantDir := filepath.Join(ws, "designs")
	if filepath.Dir(got) != wantDir {
		t.Errorf("OutputPath dir = %q, want %q", filepath.Dir(got), wantDir)
	}
	base := filepath.Base(got)
	if !strings.HasPrefix(base, "metric_plot_") || !strings.HasSuffix(base, ".png") {
		t.Errorf("OutputPath base = %q, want metric_plot_<id>.png", base)
	}
}

func TestOutputPathCreatesDesignsDir(t *testing.T) {
	ws := t.TempDir()
	if _, err := OutputPath(ws, "contact_map", "png"); err != nil {
		t.Fatalf("OutputPath: %v", err)
	}
	// Calling it again must not fail (dir already exists).
	if _, err := OutputPath(ws, "contact_map", "png"); err != nil {
		t.Fatalf("OutputPath (second call): %v", err)
	}
}
