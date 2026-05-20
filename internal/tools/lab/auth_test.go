package lab

import "testing"

func TestTokenFromEnv(t *testing.T) {
	t.Setenv("ADAPTYV_API_TOKEN", "tok-abc123")
	got, err := Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got != "tok-abc123" {
		t.Errorf("Token = %q, want tok-abc123", got)
	}
}
