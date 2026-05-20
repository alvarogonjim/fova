package agent

import "strings"

// reasoningFilter splits a stream of text deltas into visible chat text and
// hidden reasoning text. It removes <think>...</think> blocks emitted by
// reasoning-style models (Qwen 3, DeepSeek R1, and similar) so the chain of
// thought does not leak into the chat pane.
//
// The filter is stateful across deltas — an opening "<think>" can land in
// one delta and its "</think>" closing many deltas later. Partial tag
// matches at the end of a buffer are held back until the next delta either
// completes the tag or proves it was plain text. flush() releases any
// stuck-partial suffix at the end of a streaming turn so real content is
// never silently swallowed.
type reasoningFilter struct {
	inThink bool
	pending strings.Builder // un-consumed tail, possibly a partial tag
}

const (
	thinkOpen  = "<think>"
	thinkClose = "</think>"
)

// process consumes a streamed text delta and returns the visible portion
// (to forward to chat) and the reasoning portion (to drop or surface
// elsewhere via ReasoningDeltaMsg).
func (f *reasoningFilter) process(delta string) (visible, reasoning string) {
	f.pending.WriteString(delta)
	buf := f.pending.String()
	var vis, rea strings.Builder
	for len(buf) > 0 {
		if !f.inThink {
			if i := strings.Index(buf, thinkOpen); i >= 0 {
				vis.WriteString(buf[:i])
				buf = buf[i+len(thinkOpen):]
				f.inThink = true
				continue
			}
			if p := partialTagSuffix(buf, thinkOpen); p > 0 {
				vis.WriteString(buf[:len(buf)-p])
				buf = buf[len(buf)-p:]
				break // wait for more input to disambiguate
			}
			vis.WriteString(buf)
			buf = ""
		} else {
			if i := strings.Index(buf, thinkClose); i >= 0 {
				rea.WriteString(buf[:i])
				buf = buf[i+len(thinkClose):]
				f.inThink = false
				continue
			}
			if p := partialTagSuffix(buf, thinkClose); p > 0 {
				rea.WriteString(buf[:len(buf)-p])
				buf = buf[len(buf)-p:]
				break
			}
			rea.WriteString(buf)
			buf = ""
		}
	}
	f.pending.Reset()
	f.pending.WriteString(buf)
	return vis.String(), rea.String()
}

// flush returns whatever is left in the pending buffer (e.g. a "<thi" that
// never completed because the stream ended). Treated as visible text —
// better to leak a stub than swallow real content on an unfortunate cutoff.
// Also resets the inThink flag.
func (f *reasoningFilter) flush() (visible string) {
	visible = f.pending.String()
	f.pending.Reset()
	f.inThink = false
	return visible
}

// partialTagSuffix returns the length of the longest non-empty suffix of s
// that is also a prefix of tag. Used to decide whether to buffer a tail of
// s for the next delta.
func partialTagSuffix(s, tag string) int {
	n := len(tag) - 1
	if n > len(s) {
		n = len(s)
	}
	for i := n; i > 0; i-- {
		if strings.HasPrefix(tag, s[len(s)-i:]) {
			return i
		}
	}
	return 0
}
