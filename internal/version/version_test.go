package version

import "testing"

func TestStringReturnsDefaultVersion(t *testing.T) {
	const want = "0.2.0-dev"
	if got := String(); got != want {
		t.Fatalf("version.String() = %q; want %q", got, want)
	}
}
