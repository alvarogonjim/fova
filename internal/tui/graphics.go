package tui

import (
	"bytes"
	"encoding/base64"
	"os"
	"os/exec"
	"strings"
)

// Protocol identifies a terminal inline-graphics protocol. Off means the host
// terminal cannot display inline graphics (or the user disabled them) and
// rendering must fall back to text.
type Protocol int

const (
	Off Protocol = iota
	Kitty
	ITerm2
	Sixel
)

// String returns the canonical lowercase name used in [ui].inline_graphics.
func (p Protocol) String() string {
	switch p {
	case Kitty:
		return "kitty"
	case ITerm2:
		return "iterm2"
	case Sixel:
		return "sixel"
	default:
		return "off"
	}
}

// envLookup is the slice of os.Getenv the detector needs. Tests stub it.
type envLookup interface {
	Getenv(key string) string
}

// osEnv satisfies envLookup with the real os.Getenv.
type osEnv struct{}

func (osEnv) Getenv(k string) string { return os.Getenv(k) }

// sixelProbe answers "does the active terminal speak SIXEL?". The real
// implementation writes a Device Attributes query to the tty and parses the
// reply; tests stub it. v0.5 ships a best-effort no-op probe so the test
// suite passes without hardware — terminals that actually support SIXEL must
// be opted into via [ui].inline_graphics = "sixel".
type sixelProbe interface {
	Supports() bool
}

// noProbe is the conservative default: report no SIXEL support unless the
// user explicitly opts in through the config override.
type noProbe struct{}

func (noProbe) Supports() bool { return false }

// Detect picks the active protocol from the real process environment.
// The detection order is Kitty → iTerm2 → Sixel → Off, matching SPECS §3
// SP-B detection rules.
func Detect() Protocol { return detect(osEnv{}, noProbe{}) }

// detect is the injectable form of Detect. Tests call it directly with a
// fakeEnv and fakeProbe so they never touch real env vars or terminals.
func detect(env envLookup, probe sixelProbe) Protocol {
	if env.Getenv("KITTY_WINDOW_ID") != "" {
		return Kitty
	}
	if env.Getenv("TERM_PROGRAM") == "iTerm.app" || env.Getenv("LC_TERMINAL") == "iTerm2" {
		return ITerm2
	}
	if strings.Contains(env.Getenv("TERM"), "xterm-256color") && probe.Supports() {
		return Sixel
	}
	return Off
}

// OverrideMode applies the [ui].inline_graphics config override on top of
// auto-detection. "auto" or "" keeps the detected protocol; any other value
// forces that protocol. An unrecognised value falls back to detected (config
// validation rejects it before this point, but defensive code keeps tests
// stable when a stray value sneaks through).
func OverrideMode(detected Protocol, mode string) Protocol {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "auto":
		return detected
	case "kitty":
		return Kitty
	case "iterm2":
		return ITerm2
	case "sixel":
		return Sixel
	case "off":
		return Off
	default:
		return detected
	}
}

// kittyChunkSize is the per-chunk base64 payload size for the Kitty graphics
// protocol. Kitty mandates 4096 base64 characters per intermediate chunk.
const kittyChunkSize = 4096

// Encode wraps the PNG bytes in the escape sequence for the named protocol.
// The second return is true when the protocol can actually produce output
// (Sixel returns false when img2sixel is missing).
func Encode(p Protocol, png []byte) (string, bool) {
	switch p {
	case Kitty:
		return encodeKitty(png), true
	case ITerm2:
		return encodeITerm2(png), true
	case Sixel:
		return encodeSixel(png)
	default:
		return "", false
	}
}

// encodeKitty implements the Kitty graphics protocol "transmit and display"
// frame (`a=T`) with a base PNG payload (`f=100`). Payloads ≤4096 base64
// chars ship as a single frame. Larger payloads are split into chunks: every
// chunk except the last carries `m=1` ("more follows"); the last carries
// `m=0`. Each chunk is wrapped in `\x1b_G...\x1b\\`.
func encodeKitty(png []byte) string {
	b64 := base64.StdEncoding.EncodeToString(png)
	if len(b64) <= kittyChunkSize {
		return "\x1b_Ga=T,f=100;" + b64 + "\x1b\\"
	}
	var out strings.Builder
	for i := 0; i < len(b64); i += kittyChunkSize {
		end := i + kittyChunkSize
		if end > len(b64) {
			end = len(b64)
		}
		var header string
		switch {
		case i == 0:
			header = "\x1b_Ga=T,f=100,m=1;"
		case end == len(b64):
			header = "\x1b_Gm=0;"
		default:
			header = "\x1b_Gm=1;"
		}
		out.WriteString(header)
		out.WriteString(b64[i:end])
		out.WriteString("\x1b\\")
	}
	return out.String()
}

// encodeITerm2 writes the iTerm2 inline image sequence (OSC 1337). The
// payload is the base64 PNG; the BEL byte (\x07) terminates the OSC.
func encodeITerm2(png []byte) string {
	return "\x1b]1337;File=inline=1:" + base64.StdEncoding.EncodeToString(png) + "\x07"
}

// RenderImage reads a PNG file and wraps it in the named protocol's escape
// sequence. Returns ("", false) when the protocol is Off, the file cannot be
// read, or the protocol's encoder declined (e.g. Sixel without img2sixel).
// The caller is responsible for choosing the protocol; pass the Model's
// cached value from Detect+OverrideMode.
func RenderImage(p Protocol, path string) (string, bool) {
	if p == Off {
		return "", false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return Encode(p, body)
}

// encodeSixel shells out to img2sixel when it is on PATH. The PNG bytes are
// piped to stdin; stdout is the escape sequence. exec.LookPath returns
// immediately when the binary is missing — the encode call never blocks.
func encodeSixel(png []byte) (string, bool) {
	if _, err := exec.LookPath("img2sixel"); err != nil {
		return "", false
	}
	cmd := exec.Command("img2sixel")
	cmd.Stdin = bytes.NewReader(png)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", false
	}
	return stdout.String(), true
}
