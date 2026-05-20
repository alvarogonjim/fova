package backends

import (
	"context"
	"io"
	"strings"
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

func TestLocalBackendRunNoAdapterIsClear(t *testing.T) {
	b, err := Select("local", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.Run(context.Background(), "design.nonesuch", []byte(`{}`), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("want a 'no local adapter' error, got: %v", err)
	}
}

func TestLocalBackendRunReachesProteinMPNNAdapter(t *testing.T) {
	b, err := Select("local", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A bad target makes the adapter fail fast — but the error must be the
	// adapter's, proving dispatch reached design.proteinmpnn (not "no adapter").
	_, err = b.Run(context.Background(), "design.proteinmpnn", []byte(`{"target":"/no/such/file.pdb"}`), io.Discard)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("design.proteinmpnn should dispatch to its adapter, got: %v", err)
	}
}
