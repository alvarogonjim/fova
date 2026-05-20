package llm

import "testing"

var _ Provider = NewGoogleProvider("k")

func TestNewGoogleProviderName(t *testing.T) {
	if got := NewGoogleProvider("k").Name(); got != "google" {
		t.Fatalf("Name() = %q, want %q", got, "google")
	}
}
