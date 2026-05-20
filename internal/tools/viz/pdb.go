package viz

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

// vec3 is an in-package XYZ. Kept zero-dep so the parser doesn't pull gonum.
type vec3 struct{ X, Y, Z float64 }

// pdbChain is one chain's parsed backbone atoms, in file order.
type pdbChain struct {
	ID string
	CA []vec3
	N  []vec3
	C  []vec3
	// ResName holds the three-letter residue name per CA, parallel to CA.
	ResName []string
	// firstSeen records the file-order index of each chain's first appearance —
	// used by chainOrder to keep chains in input order rather than map order.
	firstSeen int
}

// parsePDBAtoms reads pdbPath and returns its chains keyed by chain ID. It is
// intentionally minimal: it only consumes ATOM records (skipping HETATM,
// HEADER, REMARK, …) and only the canonical fixed PDB columns (atom name at
// 13–16, chain ID at 22, x/y/z at 31–38/39–46/47–54). Residue numbering gaps
// are tolerated — atoms are appended in file order, period.
func parsePDBAtoms(pdbPath string) (map[string]*pdbChain, error) {
	f, err := os.Open(pdbPath)
	if err != nil {
		return nil, fmt.Errorf("open pdb: %w", err)
	}
	defer f.Close()
	chains := map[string]*pdbChain{}
	lineIdx := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		lineIdx++
		if !strings.HasPrefix(line, "ATOM") || len(line) < 54 {
			continue
		}
		atomName := strings.TrimSpace(line[12:16])
		if atomName != "CA" && atomName != "N" && atomName != "C" {
			continue
		}
		chainID := strings.TrimSpace(line[21:22])
		if chainID == "" {
			chainID = "_"
		}
		resName := strings.TrimSpace(line[17:20])
		x, errX := parseColFloat(line[30:38])
		y, errY := parseColFloat(line[38:46])
		z, errZ := parseColFloat(line[46:54])
		if errX != nil || errY != nil || errZ != nil {
			continue
		}
		ch, ok := chains[chainID]
		if !ok {
			ch = &pdbChain{ID: chainID, firstSeen: lineIdx}
			chains[chainID] = ch
		}
		v := vec3{X: x, Y: y, Z: z}
		switch atomName {
		case "CA":
			ch.CA = append(ch.CA, v)
			ch.ResName = append(ch.ResName, resName)
		case "N":
			ch.N = append(ch.N, v)
		case "C":
			ch.C = append(ch.C, v)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan pdb: %w", err)
	}
	if len(chains) == 0 {
		return nil, fmt.Errorf("no ATOM CA records found in %s", pdbPath)
	}
	return chains, nil
}

// parseColFloat trims and parses a PDB fixed-width column.
func parseColFloat(col string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(col), 64)
}

// chainOrder returns chain IDs in their first-appearance order.
func chainOrder(chains map[string]*pdbChain) []string {
	ids := make([]string, 0, len(chains))
	for id := range chains {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return chains[ids[i]].firstSeen < chains[ids[j]].firstSeen
	})
	return ids
}

// distance returns the Euclidean distance between a and b.
func distance(a, b vec3) float64 {
	dx, dy, dz := a.X-b.X, a.Y-b.Y, a.Z-b.Z
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}
