package proteinio

import (
	"bufio"
	"io"
	"strings"
)

// aa3to1 maps the standard 20 three-letter amino-acid codes to their
// one-letter codes. It is shared by the PDB and mmCIF parsers.
var aa3to1 = map[string]string{
	"ALA": "A",
	"ARG": "R",
	"ASN": "N",
	"ASP": "D",
	"CYS": "C",
	"GLN": "Q",
	"GLU": "E",
	"GLY": "G",
	"HIS": "H",
	"ILE": "I",
	"LEU": "L",
	"LYS": "K",
	"MET": "M",
	"PHE": "F",
	"PRO": "P",
	"SER": "S",
	"THR": "T",
	"TRP": "W",
	"TYR": "Y",
	"VAL": "V",
}

// resToOne maps a three-letter residue name to its one-letter code, returning
// "X" for any unknown residue.
func resToOne(res string) string {
	if one, ok := aa3to1[strings.ToUpper(strings.TrimSpace(res))]; ok {
		return one
	}
	return "X"
}

// ChainsFromPDB reads ATOM records from a PDB file and returns a map of chain
// ID to its one-letter amino-acid sequence. One residue is recorded per CA
// atom, deduplicated by (chain, residue sequence number).
func ChainsFromPDB(r io.Reader) (map[string]string, error) {
	chains := map[string]string{}
	seen := map[string]bool{}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		// Need at least up to column 26 to slice the residue sequence number.
		if len(line) < 26 {
			continue
		}
		if strings.TrimSpace(line[0:6]) != "ATOM" {
			continue
		}
		if strings.TrimSpace(line[12:16]) != "CA" {
			continue
		}
		resName := line[17:20]
		chainID := strings.TrimSpace(line[21:22])
		resSeq := strings.TrimSpace(line[22:26])

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
