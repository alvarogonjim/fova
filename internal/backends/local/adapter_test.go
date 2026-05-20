package local

import (
	"context"
	"strings"
	"testing"
)

func TestRunDesignUnknownToolErrors(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RunDesign(context.Background(), reg, "design.nonesuch", []byte(`{}`)); err == nil {
		t.Fatal("expected an error for a tool with no adapter")
	}
}

func TestRunDesignNoAdapterMessageIsClear(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Every real design.* tool has an adapter after SP3 — use a fabricated name.
	_, err = RunDesign(context.Background(), reg, "design.nonesuch", []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("want a 'no local adapter' error, got: %v", err)
	}
}
