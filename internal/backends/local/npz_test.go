package local

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeNPY builds a valid version-1.0 .npy byte slice for a float64 array.
// shapeText is the numpy shape tuple: "()" scalar, "(3,)" a 3-vector.
func makeNPY(shapeText string, vals []float64) []byte {
	hdr := "{'descr': '<f8', 'fortran_order': False, 'shape': " + shapeText + ", }"
	total := 10 + len(hdr) + 1 // 6 magic + 2 version + 2 len + header + \n
	if pad := (64 - total%64) % 64; pad > 0 {
		hdr += strings.Repeat(" ", pad)
	}
	hdr += "\n"
	var b bytes.Buffer
	b.WriteString("\x93NUMPY")
	b.WriteByte(1)
	b.WriteByte(0)
	_ = binary.Write(&b, binary.LittleEndian, uint16(len(hdr)))
	b.WriteString(hdr)
	for _, v := range vals {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	return b.Bytes()
}

func makeNPZ(t *testing.T, members map[string][]byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scores.npz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, data := range members {
		w, err := zw.Create(name + ".npy")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadNPZ(t *testing.T) {
	path := makeNPZ(t, map[string][]byte{
		"ptm":           makeNPY("()", []float64{0.83}),
		"per_chain_ptm": makeNPY("(3,)", []float64{0.9, 0.8, 0.7}),
	})
	got, err := readNPZ(path)
	if err != nil {
		t.Fatalf("readNPZ: %v", err)
	}
	if len(got["ptm"].Shape) != 0 || got["ptm"].Data[0] != 0.83 {
		t.Errorf("ptm scalar wrong: %+v", got["ptm"])
	}
	pc := got["per_chain_ptm"]
	if len(pc.Shape) != 1 || pc.Shape[0] != 3 || pc.Data[2] != 0.7 {
		t.Errorf("per_chain_ptm vector wrong: %+v", pc)
	}
}
