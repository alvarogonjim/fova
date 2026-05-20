package agent

import (
	"strings"
	"testing"
)

// feed drives the filter with a sequence of deltas and returns the
// concatenated visible and reasoning streams, including any flushed tail.
func feed(f *reasoningFilter, deltas ...string) (vis, rea string) {
	var vb, rb strings.Builder
	for _, d := range deltas {
		v, r := f.process(d)
		vb.WriteString(v)
		rb.WriteString(r)
	}
	vb.WriteString(f.flush())
	return vb.String(), rb.String()
}

func TestReasoningFilterPassesPlainText(t *testing.T) {
	f := &reasoningFilter{}
	v, r := feed(f, "Hello, world!")
	if v != "Hello, world!" {
		t.Errorf("visible = %q, want %q", v, "Hello, world!")
	}
	if r != "" {
		t.Errorf("reasoning = %q, want empty", r)
	}
}

func TestReasoningFilterStripsCompleteBlock(t *testing.T) {
	f := &reasoningFilter{}
	v, r := feed(f, "<think>The user said hi.</think>Hello!")
	if v != "Hello!" {
		t.Errorf("visible = %q, want %q", v, "Hello!")
	}
	if r != "The user said hi." {
		t.Errorf("reasoning = %q", r)
	}
}

func TestReasoningFilterHandlesSplitTag(t *testing.T) {
	// The opening tag and the closing tag both split across delta boundaries
	// in awkward places. The filter must reconstruct them across calls.
	f := &reasoningFilter{}
	v, r := feed(f, "<thi", "nk>foo", "bar</thi", "nk>baz")
	if v != "baz" {
		t.Errorf("visible = %q, want %q", v, "baz")
	}
	if r != "foobar" {
		t.Errorf("reasoning = %q", r)
	}
}

func TestReasoningFilterHandlesMultipleBlocks(t *testing.T) {
	f := &reasoningFilter{}
	v, r := feed(f, "<think>a</think>X<think>b</think>Y")
	if v != "XY" || r != "ab" {
		t.Errorf("visible=%q reasoning=%q", v, r)
	}
}

func TestReasoningFilterFlushLeaksPartial(t *testing.T) {
	f := &reasoningFilter{}
	v, r := feed(f, "Hello<thi")
	// A partial "<thi" at the end is plain content as far as the stream
	// knows — flush() emits it verbatim so a truncated stream never
	// silently swallows real text.
	if v != "Hello<thi" {
		t.Errorf("visible = %q, want Hello<thi", v)
	}
	if r != "" {
		t.Errorf("reasoning = %q", r)
	}
}

func TestReasoningFilterMidTurnText(t *testing.T) {
	f := &reasoningFilter{}
	v, r := feed(f, "Hi <think>noise</think> there")
	if v != "Hi  there" {
		t.Errorf("visible = %q, want %q", v, "Hi  there")
	}
	if r != "noise" {
		t.Errorf("reasoning = %q", r)
	}
}

func TestReasoningFilterAllReasoningNoText(t *testing.T) {
	f := &reasoningFilter{}
	v, r := feed(f, "<think>", "just thinking", "</think>")
	if v != "" {
		t.Errorf("visible = %q, want empty", v)
	}
	if r != "just thinking" {
		t.Errorf("reasoning = %q", r)
	}
}

func TestReasoningFilterStreamEndsInsideThink(t *testing.T) {
	// A stream that ends mid-think (no closing tag) flushes the buffered
	// reasoning into visible. The chat will show the reasoning, which is
	// the right fallback when the model never closes the tag.
	f := &reasoningFilter{}
	v, r := feed(f, "<think>open-ended thought")
	if r != "open-ended thought" {
		t.Errorf("reasoning = %q, want open-ended thought", r)
	}
	if v != "" {
		t.Errorf("visible during feed = %q", v)
	}
}
