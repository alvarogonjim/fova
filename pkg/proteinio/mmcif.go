package proteinio

import (
	"bufio"
	"io"
	"strings"
)

// ChainsFromMMCIF parses the _atom_site loop of an mmCIF file and returns a map
// of chain ID to its one-letter amino-acid sequence. Column indices are
// resolved from the loop header order. One residue is recorded per CA atom,
// deduplicated by (chain, residue sequence number).
func ChainsFromMMCIF(r io.Reader) (map[string]string, error) {
	chains := map[string]string{}
	seen := map[string]bool{}

	var columns []string  // ordered _atom_site.* tags
	inLoopHeader := false // currently collecting tags after loop_
	inAtomSite := false   // collecting data rows for the _atom_site loop

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if line == "loop_" {
			inLoopHeader = true
			inAtomSite = false
			columns = nil
			continue
		}

		if strings.HasPrefix(line, "_") {
			if inLoopHeader {
				if strings.HasPrefix(line, "_atom_site.") {
					columns = append(columns, line)
				} else if len(columns) > 0 {
					// A different loop's tags; abandon this loop header.
					inLoopHeader = false
					columns = nil
				} else {
					// Tags from an unrelated loop; ignore until loop_.
					inLoopHeader = false
				}
				continue
			}
			// A bare data-name item ends any in-progress _atom_site loop.
			inAtomSite = false
			continue
		}

		// Non-tag, non-keyword line.
		if inLoopHeader {
			// First data row of the loop we have been collecting tags for.
			inLoopHeader = false
			if isAtomSiteLoop(columns) {
				inAtomSite = true
			} else {
				columns = nil
			}
		}

		if !inAtomSite {
			continue
		}

		idxAtom := columnIndex(columns, "_atom_site.label_atom_id")
		idxComp := columnIndex(columns, "_atom_site.label_comp_id")
		idxAsym := columnIndex(columns, "_atom_site.label_asym_id")
		idxSeq := columnIndex(columns, "_atom_site.label_seq_id")
		if idxAtom < 0 || idxComp < 0 || idxAsym < 0 || idxSeq < 0 {
			// Required columns missing; nothing useful to extract.
			inAtomSite = false
			continue
		}

		fields := strings.Fields(line)
		maxIdx := idxAtom
		for _, i := range []int{idxComp, idxAsym, idxSeq} {
			if i > maxIdx {
				maxIdx = i
			}
		}
		if maxIdx >= len(fields) {
			continue
		}

		if !strings.EqualFold(fields[idxAtom], "CA") {
			continue
		}
		chainID := fields[idxAsym]
		resSeq := fields[idxSeq]
		resName := fields[idxComp]

		key := chainID + "\x00" + resSeq
		if seen[key] {
			continue
		}
		seen[key] = true
		chains[chainID] += resToOne(resName)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return chains, nil
}

// isAtomSiteLoop reports whether the collected column tags belong to the
// _atom_site loop (its first tag starts with "_atom_site.").
func isAtomSiteLoop(columns []string) bool {
	return len(columns) > 0 && strings.HasPrefix(columns[0], "_atom_site.")
}

// columnIndex returns the position of tag within columns, or -1 if absent.
func columnIndex(columns []string, tag string) int {
	for i, c := range columns {
		if c == tag {
			return i
		}
	}
	return -1
}
