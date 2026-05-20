// Package proteinio provides helpers for reading and writing common protein
// file formats (FASTA, PDB and mmCIF) using only the standard library.
package proteinio

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// Record is a single FASTA entry: a header and its associated sequence.
type Record struct {
	Header   string
	Sequence string
}

// fastaWrapWidth is the number of sequence characters written per line.
const fastaWrapWidth = 60

// ParseFASTA reads FASTA records from r. It supports multiple records and
// multi-line sequences, skips blank lines and trims the leading ">" and
// surrounding whitespace from headers. It returns an error if a sequence line
// appears before any header. An empty input yields an empty slice and no error.
func ParseFASTA(r io.Reader) ([]Record, error) {
	recs := []Record{}
	var current *Record
	var seq strings.Builder

	flush := func() {
		if current != nil {
			current.Sequence = seq.String()
			recs = append(recs, *current)
		}
		seq.Reset()
	}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ">") {
			flush()
			header := strings.TrimSpace(strings.TrimPrefix(line, ">"))
			current = &Record{Header: header}
			continue
		}
		if current == nil {
			return nil, errors.New("proteinio: sequence line before any FASTA header")
		}
		seq.WriteString(line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	flush()
	return recs, nil
}

// WriteFASTA writes recs to w as FASTA, wrapping each sequence at 60
// characters per line.
func WriteFASTA(w io.Writer, recs []Record) error {
	bw := bufio.NewWriter(w)
	for _, rec := range recs {
		if _, err := bw.WriteString(">" + rec.Header + "\n"); err != nil {
			return err
		}
		seq := rec.Sequence
		for len(seq) > 0 {
			n := fastaWrapWidth
			if n > len(seq) {
				n = len(seq)
			}
			if _, err := bw.WriteString(seq[:n] + "\n"); err != nil {
				return err
			}
			seq = seq[n:]
		}
	}
	return bw.Flush()
}
