package backends

import (
	"testing"
)

func TestSelectLocal(t *testing.T) {
	b, err := Select("local", t.TempDir())
	if err != nil {
		t.Fatalf("Select local: %v", err)
	}
	if b.Name() != "local" {
		t.Errorf("Name = %q, want local", b.Name())
	}
}

func TestSelectModal(t *testing.T) {
	b, err := Select("modal", t.TempDir())
	if err != nil {
		t.Fatalf("Select modal: %v", err)
	}
	if b.Name() != "modal" {
		t.Errorf("Name = %q, want modal", b.Name())
	}
}

func TestSelectDefaultsToLocal(t *testing.T) {
	b, err := Select("", t.TempDir())
	if err != nil || b.Name() != "local" {
		t.Fatalf("empty backend should default to local: %v / %v", b, err)
	}
}

func TestSelectUnknown(t *testing.T) {
	if _, err := Select("nonsense", t.TempDir()); err == nil {
		t.Error("an unknown backend name should error")
	}
}
