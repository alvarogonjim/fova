package proteinio

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestParseWriteFASTARoundTrip(t *testing.T) {
	input := ">seq1 first protein\n" +
		"MKTAYIAKQRQISFVKSHFSRQLEERLGLIEVQAPILSRVGDGTQDNLSGAEKAVQVKVK\n" +
		"ALPDAQFEVVHSLAKWKR\n" +
		"\n" +
		">seq2 second protein\n" +
		"GIVEQCCTSICSLYQLENYCN\n"

	recs, err := ParseFASTA(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseFASTA error: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}
	if recs[0].Header != "seq1 first protein" {
		t.Errorf("rec0 header = %q", recs[0].Header)
	}
	if recs[0].Sequence != "MKTAYIAKQRQISFVKSHFSRQLEERLGLIEVQAPILSRVGDGTQDNLSGAEKAVQVKVKALPDAQFEVVHSLAKWKR" {
		t.Errorf("rec0 sequence = %q", recs[0].Sequence)
	}
	if recs[1].Header != "seq2 second protein" {
		t.Errorf("rec1 header = %q", recs[1].Header)
	}
	if recs[1].Sequence != "GIVEQCCTSICSLYQLENYCN" {
		t.Errorf("rec1 sequence = %q", recs[1].Sequence)
	}

	var buf bytes.Buffer
	if err := WriteFASTA(&buf, recs); err != nil {
		t.Fatalf("WriteFASTA error: %v", err)
	}

	recs2, err := ParseFASTA(&buf)
	if err != nil {
		t.Fatalf("ParseFASTA (round trip) error: %v", err)
	}
	if !reflect.DeepEqual(recs, recs2) {
		t.Errorf("round trip mismatch:\n got  %#v\n want %#v", recs2, recs)
	}
}

func TestWriteFASTAWrapsAt60(t *testing.T) {
	seq := strings.Repeat("A", 125)
	var buf bytes.Buffer
	if err := WriteFASTA(&buf, []Record{{Header: "h", Sequence: seq}}); err != nil {
		t.Fatalf("WriteFASTA error: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if lines[0] != ">h" {
		t.Errorf("header line = %q", lines[0])
	}
	if len(lines[1]) != 60 || len(lines[2]) != 60 || len(lines[3]) != 5 {
		t.Errorf("unexpected wrap lengths: %d %d %d", len(lines[1]), len(lines[2]), len(lines[3]))
	}
}

func TestParseFASTAEmpty(t *testing.T) {
	recs, err := ParseFASTA(strings.NewReader(""))
	if err != nil {
		t.Fatalf("ParseFASTA error: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("expected empty slice, got %d records", len(recs))
	}
}

func TestParseFASTASequenceBeforeHeader(t *testing.T) {
	_, err := ParseFASTA(strings.NewReader("MKTAYIAK\n>seq1\nGIVEQ\n"))
	if err == nil {
		t.Fatal("expected error for sequence line before header, got nil")
	}
}

func TestChainsFromPDB(t *testing.T) {
	pdb := "ATOM      1  N   MET A   1      11.104  13.207  10.000  1.00  0.00           N\n" +
		"ATOM      2  CA  MET A   1      12.000  13.000  10.000  1.00  0.00           C\n" +
		"ATOM      3  C   MET A   1      12.500  13.500  10.000  1.00  0.00           C\n" +
		"ATOM      4  CA  LYS A   2      13.000  14.000  10.000  1.00  0.00           C\n" +
		"ATOM      5  CA  THR A   3      14.000  15.000  10.000  1.00  0.00           C\n" +
		"ATOM      6  CA  XXX A   4      15.000  16.000  10.000  1.00  0.00           C\n"
	chains, err := ChainsFromPDB(strings.NewReader(pdb))
	if err != nil {
		t.Fatalf("ChainsFromPDB error: %v", err)
	}
	want := map[string]string{"A": "MKTX"}
	if !reflect.DeepEqual(chains, want) {
		t.Errorf("chains = %#v, want %#v", chains, want)
	}
}

func TestChainsFromMMCIF(t *testing.T) {
	mmcif := "data_test\n" +
		"loop_\n" +
		"_atom_site.group_PDB\n" +
		"_atom_site.id\n" +
		"_atom_site.label_atom_id\n" +
		"_atom_site.label_comp_id\n" +
		"_atom_site.label_asym_id\n" +
		"_atom_site.label_seq_id\n" +
		"ATOM 1 N  MET A 1\n" +
		"ATOM 2 CA MET A 1\n" +
		"ATOM 3 C  MET A 1\n" +
		"ATOM 4 CA LYS A 2\n" +
		"ATOM 5 CA THR A 3\n"
	chains, err := ChainsFromMMCIF(strings.NewReader(mmcif))
	if err != nil {
		t.Fatalf("ChainsFromMMCIF error: %v", err)
	}
	want := map[string]string{"A": "MKT"}
	if !reflect.DeepEqual(chains, want) {
		t.Errorf("chains = %#v, want %#v", chains, want)
	}
}
