package tui

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEnv is a tiny in-memory env lookup. It satisfies envLookup.
type fakeEnv map[string]string

func (f fakeEnv) Getenv(k string) string { return f[k] }

// fakeProbe is a stub sixelProbe. ok controls whether the probe reports support.
type fakeProbe struct{ ok bool }

func (p fakeProbe) Supports() bool { return p.ok }

func TestDetectKittyFromEnv(t *testing.T) {
	env := fakeEnv{"KITTY_WINDOW_ID": "1"}
	if got := detect(env, fakeProbe{ok: false}); got != Kitty {
		t.Errorf("detect(KITTY_WINDOW_ID) = %v, want Kitty", got)
	}
}

func TestDetectITerm2FromEnv(t *testing.T) {
	env := fakeEnv{"TERM_PROGRAM": "iTerm.app"}
	if got := detect(env, fakeProbe{ok: false}); got != ITerm2 {
		t.Errorf("detect(TERM_PROGRAM=iTerm.app) = %v, want ITerm2", got)
	}
	env = fakeEnv{"LC_TERMINAL": "iTerm2"}
	if got := detect(env, fakeProbe{ok: false}); got != ITerm2 {
		t.Errorf("detect(LC_TERMINAL=iTerm2) = %v, want ITerm2", got)
	}
}

func TestDetectSixelWhenProbeOK(t *testing.T) {
	env := fakeEnv{"TERM": "xterm-256color"}
	if got := detect(env, fakeProbe{ok: true}); got != Sixel {
		t.Errorf("detect(TERM=xterm-256color, probe=true) = %v, want Sixel", got)
	}
}

func TestDetectSixelProbeFailsFallsBackToOff(t *testing.T) {
	env := fakeEnv{"TERM": "xterm-256color"}
	if got := detect(env, fakeProbe{ok: false}); got != Off {
		t.Errorf("detect(TERM=xterm-256color, probe=false) = %v, want Off", got)
	}
}

func TestDetectEmptyEnvIsOff(t *testing.T) {
	if got := detect(fakeEnv{}, fakeProbe{ok: false}); got != Off {
		t.Errorf("detect(empty) = %v, want Off", got)
	}
}

func TestDetectKittyBeatsITerm(t *testing.T) {
	// If both env vars are set Kitty wins because it is checked first.
	env := fakeEnv{"KITTY_WINDOW_ID": "1", "TERM_PROGRAM": "iTerm.app"}
	if got := detect(env, fakeProbe{ok: false}); got != Kitty {
		t.Errorf("detect(kitty+iterm) = %v, want Kitty", got)
	}
}

func TestEncodeKittyChunksAt4096(t *testing.T) {
	// Build a payload whose base64 length is >4096 chars so we exercise chunking.
	// 4096 base64 chars decode from 3072 raw bytes; 5000 bytes guarantees ≥2 chunks.
	png := make([]byte, 5000)
	for i := range png {
		png[i] = byte(i & 0xff)
	}
	out, ok := Encode(Kitty, png)
	if !ok {
		t.Fatal("Encode(Kitty) returned ok=false")
	}
	if !strings.HasPrefix(out, "\x1b_Ga=T,f=100,m=1;") {
		t.Errorf("first chunk prefix wrong: %q", out[:24])
	}
	if !strings.HasSuffix(out, "\x1b\\") {
		t.Errorf("missing terminating ST: ...%q", out[len(out)-4:])
	}
	// Verify the last chunk uses m=0 (final).
	if !strings.Contains(out, "\x1b_Gm=0;") {
		t.Error("expected a m=0 final chunk header")
	}
	// Verify chunking: at 5000 raw bytes → 6668 base64 chars → 2 chunks.
	if got, want := strings.Count(out, "\x1b_G"), 2; got != want {
		t.Errorf("chunk count = %d, want %d", got, want)
	}
}

func TestEncodeKittySingleChunkUsesT(t *testing.T) {
	// Tiny payload fits one chunk → header is `a=T,f=100;` with no m= flag.
	png := []byte("PNG-bytes-small")
	out, ok := Encode(Kitty, png)
	if !ok {
		t.Fatal("Encode(Kitty) returned ok=false")
	}
	if !strings.HasPrefix(out, "\x1b_Ga=T,f=100;") {
		t.Errorf("single-chunk prefix wrong: %q", out[:24])
	}
	if strings.Contains(out, "m=1") || strings.Contains(out, "m=0") {
		t.Error("single-chunk frame must not carry an m= flag")
	}
	// Payload is exactly the base64 of png.
	want := base64.StdEncoding.EncodeToString(png)
	if !strings.Contains(out, want) {
		t.Errorf("base64 payload not found in output")
	}
}

func TestEncodeITerm2(t *testing.T) {
	png := []byte("PNG-bytes")
	out, ok := Encode(ITerm2, png)
	if !ok {
		t.Fatal("Encode(ITerm2) returned ok=false")
	}
	want := "\x1b]1337;File=inline=1:" + base64.StdEncoding.EncodeToString(png) + "\x07"
	if out != want {
		t.Errorf("Encode(ITerm2) = %q, want %q", out, want)
	}
}

func TestEncodeOffAlwaysFalse(t *testing.T) {
	if out, ok := Encode(Off, []byte("x")); ok || out != "" {
		t.Errorf("Encode(Off) = (%q, %v), want (\"\", false)", out, ok)
	}
}

func TestEncodeSixelMissingBinaryIsFalse(t *testing.T) {
	if _, err := exec.LookPath("img2sixel"); err == nil {
		t.Skip("img2sixel is on PATH; this test asserts the absent-binary branch")
	}
	if out, ok := Encode(Sixel, []byte("png")); ok || out != "" {
		t.Errorf("Encode(Sixel) without img2sixel = (%q, %v), want (\"\", false)", out, ok)
	}
}

func TestEncodeSixelWithBinary(t *testing.T) {
	if _, err := exec.LookPath("img2sixel"); err != nil {
		t.Skip("img2sixel not on PATH")
	}
	// A 1x1 transparent PNG (smallest valid).
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}
	out, ok := Encode(Sixel, png)
	if !ok {
		t.Fatalf("Encode(Sixel) with img2sixel returned ok=false")
	}
	if out == "" {
		t.Error("Encode(Sixel) returned empty output")
	}
}

func TestOverrideModeAutoKeepsDetected(t *testing.T) {
	for _, det := range []Protocol{Off, Kitty, ITerm2, Sixel} {
		if got := OverrideMode(det, "auto"); got != det {
			t.Errorf("OverrideMode(%v, auto) = %v, want %v", det, got, det)
		}
		if got := OverrideMode(det, ""); got != det {
			t.Errorf("OverrideMode(%v, empty) = %v, want %v", det, got, det)
		}
	}
}

func TestOverrideModeForcesExplicit(t *testing.T) {
	cases := map[string]Protocol{
		"kitty":  Kitty,
		"iterm2": ITerm2,
		"sixel":  Sixel,
		"off":    Off,
	}
	for mode, want := range cases {
		if got := OverrideMode(Off, mode); got != want {
			t.Errorf("OverrideMode(Off, %q) = %v, want %v", mode, got, want)
		}
		if got := OverrideMode(Kitty, mode); got != want {
			t.Errorf("OverrideMode(Kitty, %q) = %v, want %v", mode, got, want)
		}
	}
}

func TestOverrideModeUnknownFallsBackToDetected(t *testing.T) {
	if got := OverrideMode(Kitty, "bogus"); got != Kitty {
		t.Errorf("OverrideMode(Kitty, bogus) = %v, want Kitty", got)
	}
}

func TestRenderImageMissingFileReturnsFalse(t *testing.T) {
	out, ok := RenderImage(Kitty, "/nonexistent/path.png")
	if ok || out != "" {
		t.Errorf("RenderImage(missing) = (%q, %v), want (\"\", false)", out, ok)
	}
}

func TestRenderImageOffProtocolReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.png")
	if err := os.WriteFile(path, []byte("PNG"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, ok := RenderImage(Off, path); ok || out != "" {
		t.Errorf("RenderImage(Off) = (%q, %v), want (\"\", false)", out, ok)
	}
}

func TestRenderImageKittyReadsAndEncodes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.png")
	body := []byte("imaginary PNG bytes")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	out, ok := RenderImage(Kitty, path)
	if !ok {
		t.Fatal("RenderImage(Kitty, valid) returned ok=false")
	}
	want := base64.StdEncoding.EncodeToString(body)
	if !strings.Contains(out, want) {
		t.Errorf("RenderImage output missing the base64 payload")
	}
	if !strings.HasPrefix(out, "\x1b_Ga=T,f=100;") || !strings.HasSuffix(out, "\x1b\\") {
		t.Errorf("RenderImage(Kitty) wrapper wrong: %q...%q", out[:20], out[len(out)-4:])
	}
}
