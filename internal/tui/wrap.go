package tui

import (
	"strings"
	"unicode/utf8"
)

// wrapText soft-wraps s to at most w runes per line, breaking on whitespace.
// A single word longer than w is hard-broken. The result joins the lines with
// "\n". A w <= 0 returns s unchanged. Used by sidebar panels so empty-state
// hints no longer get clipped mid-word (jobs / designs / wet-lab).
func wrapText(s string, w int) string {
	if w <= 0 || utf8.RuneCountInString(s) <= w {
		return s
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}
	var lines []string
	var cur strings.Builder
	curLen := 0
	for _, word := range words {
		wl := utf8.RuneCountInString(word)
		// Single word longer than the budget: emit the running line, then
		// hard-break the long word.
		if wl > w {
			if curLen > 0 {
				lines = append(lines, cur.String())
				cur.Reset()
				curLen = 0
			}
			runes := []rune(word)
			for len(runes) > 0 {
				n := w
				if n > len(runes) {
					n = len(runes)
				}
				lines = append(lines, string(runes[:n]))
				runes = runes[n:]
			}
			continue
		}
		// Word fits on the current line (with a separator if non-empty).
		need := wl
		if curLen > 0 {
			need += 1 // for the space
		}
		if curLen+need > w {
			lines = append(lines, cur.String())
			cur.Reset()
			curLen = 0
			cur.WriteString(word)
			curLen = wl
			continue
		}
		if curLen > 0 {
			cur.WriteByte(' ')
			curLen++
		}
		cur.WriteString(word)
		curLen += wl
	}
	if curLen > 0 {
		lines = append(lines, cur.String())
	}
	return strings.Join(lines, "\n")
}
