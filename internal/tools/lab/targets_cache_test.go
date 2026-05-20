package lab

import (
	"path/filepath"
	"testing"
	"time"
)

// TestTargetsCacheRoundTrip writes a value, reads it back, and checks the
// raw bytes survive.
func TestTargetsCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c, err := OpenTargetsCache(dir, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("OpenTargetsCache: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	want := []byte(`{"pdb_id":"1LYZ","chain":"A"}`)
	if err := c.Put("1LYZ", want); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok, err := c.Get("1LYZ")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("Get: ok = false, want true")
	}
	if string(got) != string(want) {
		t.Errorf("Get value = %q, want %q", got, want)
	}
}

// TestTargetsCacheMiss verifies a fresh cache reports ok=false for an
// unknown key.
func TestTargetsCacheMiss(t *testing.T) {
	c, err := OpenTargetsCache(t.TempDir(), 7*24*time.Hour)
	if err != nil {
		t.Fatalf("OpenTargetsCache: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if _, ok, _ := c.Get("nothing"); ok {
		t.Error("Get on missing key returned ok = true")
	}
}

// TestTargetsCacheExpired verifies an entry older than the TTL is treated as
// a miss and the stale row is overwritten on the next Put.
func TestTargetsCacheExpired(t *testing.T) {
	dir := t.TempDir()
	c, err := OpenTargetsCache(dir, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("OpenTargetsCache: %v", err)
	}

	// Force a past timestamp by writing through the unexported helper.
	if err := c.putWithTime("OLD", []byte(`{"v":1}`), time.Now().Add(-30*24*time.Hour)); err != nil {
		t.Fatalf("putWithTime: %v", err)
	}
	if _, ok, _ := c.Get("OLD"); ok {
		t.Error("Get on expired key returned ok = true")
	}

	// A fresh Put replaces the stale row and the cache returns ok again.
	if err := c.Put("OLD", []byte(`{"v":2}`)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok, _ := c.Get("OLD")
	if !ok {
		t.Fatal("Get after fresh Put: ok = false")
	}
	if string(got) != `{"v":2}` {
		t.Errorf("Get value = %q, want fresh value", got)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Re-open the same directory and verify the row is still readable.
	c2, err := OpenTargetsCache(dir, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	defer c2.Close()
	if _, ok, _ := c2.Get("OLD"); !ok {
		t.Error("Get after re-open: ok = false")
	}
}

// TestTargetsCacheDefaultPath verifies OpenTargetsCacheDefault uses the
// canonical ~/.fova/cache/targets.db layout (well, against a temp HOME).
func TestTargetsCacheDefaultPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	c, err := OpenTargetsCacheDefault()
	if err != nil {
		t.Fatalf("OpenTargetsCacheDefault: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	want := filepath.Join(dir, ".fova", "cache", "targets.db")
	if c.Path() != want {
		t.Errorf("Path() = %q, want %q", c.Path(), want)
	}
}
