package version

import "testing"

func TestStringReturnsDefaultVersion(t *testing.T) {
	const want = "0.5.0"
	if got := String(); got != want {
		t.Fatalf("version.String() = %q; want %q", got, want)
	}
}
