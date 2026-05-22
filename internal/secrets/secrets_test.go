package secrets

import "testing"

func TestSetGetRoundTrip(t *testing.T) {
	defer UseInMemoryKeyring()()
	if err := Set("anthropic-api-key", "sk-xyz"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok := Get("anthropic-api-key")
	if !ok || got != "sk-xyz" {
		t.Errorf("Get = %q, %v; want sk-xyz, true", got, ok)
	}
}

func TestGetMissing(t *testing.T) {
	defer UseInMemoryKeyring()()
	if _, ok := Get("absent"); ok {
		t.Error("Get of a missing key should return ok=false")
	}
}

func TestAPIKeyName(t *testing.T) {
	if got := APIKeyName("anthropic"); got != "anthropic-api-key" {
		t.Errorf("APIKeyName = %q, want anthropic-api-key", got)
	}
}
