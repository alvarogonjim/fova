package local

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// npzValue is one array decoded from a .npz member. Data is the flattened
// values (scalars, 1-D, or higher). Shape is the numpy shape — len 0 is a
// scalar, len 1 a vector. All dtypes are widened to float64.
type npzValue struct {
	Shape []int
	Data  []float64
}

// readNPZ opens a .npz (a ZIP of .npy members) and decodes each member.
// The member key is the file name without the ".npy" suffix. A member whose
// dtype the reader does not recognise is skipped rather than failing the
// whole file.
func readNPZ(path string) (map[string]npzValue, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	out := map[string]npzValue{}
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, ".npy") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		val, ok, err := decodeNPY(raw)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue // unrecognised dtype — skip this member
		}
		key := strings.TrimSuffix(f.Name, ".npy")
		out[key] = val
	}
	return out, nil
}

// decodeNPY decodes a single .npy byte slice. The bool return is false when
// the dtype is not one the reader supports (the member should be skipped).
func decodeNPY(raw []byte) (npzValue, bool, error) {
	if len(raw) < 10 || !bytes.Equal(raw[:6], []byte("\x93NUMPY")) {
		return npzValue{}, false, fmt.Errorf("npz: bad .npy magic")
	}
	major := raw[6]
	var headerLen int
	var headerStart int
	switch {
	case major >= 2:
		if len(raw) < 12 {
			return npzValue{}, false, fmt.Errorf("npz: truncated .npy header length")
		}
		headerLen = int(binary.LittleEndian.Uint32(raw[8:12]))
		headerStart = 12
	default: // major 1
		headerLen = int(binary.LittleEndian.Uint16(raw[8:10]))
		headerStart = 10
	}
	if len(raw) < headerStart+headerLen {
		return npzValue{}, false, fmt.Errorf("npz: truncated .npy header")
	}
	header := string(raw[headerStart : headerStart+headerLen])
	data := raw[headerStart+headerLen:]

	descr := extractDictString(header, "descr")
	shape, err := extractShape(header)
	if err != nil {
		return npzValue{}, false, err
	}

	count := 1
	for _, d := range shape {
		count *= d
	}

	values, ok, err := decodeValues(descr, data, count)
	if err != nil {
		return npzValue{}, false, err
	}
	if !ok {
		return npzValue{}, false, nil
	}
	return npzValue{Shape: shape, Data: values}, true, nil
}

// extractDictString pulls a single-quoted value for key out of a numpy header
// dict literal, e.g. extractDictString("{'descr': '<f8', ...}", "descr") ⇒ "<f8".
func extractDictString(header, key string) string {
	marker := "'" + key + "'"
	i := strings.Index(header, marker)
	if i < 0 {
		return ""
	}
	rest := header[i+len(marker):]
	q1 := strings.IndexByte(rest, '\'')
	if q1 < 0 {
		return ""
	}
	rest = rest[q1+1:]
	q2 := strings.IndexByte(rest, '\'')
	if q2 < 0 {
		return ""
	}
	return rest[:q2]
}

// extractShape parses the `shape` tuple from a numpy header dict literal.
// "()" ⇒ []int{} (scalar), "(3,)" ⇒ []int{3}, "(2, 4)" ⇒ []int{2, 4}.
func extractShape(header string) ([]int, error) {
	marker := "'shape'"
	i := strings.Index(header, marker)
	if i < 0 {
		return nil, fmt.Errorf("npz: header missing shape")
	}
	rest := header[i+len(marker):]
	open := strings.IndexByte(rest, '(')
	if open < 0 {
		return nil, fmt.Errorf("npz: header shape not a tuple")
	}
	closeIdx := strings.IndexByte(rest[open:], ')')
	if closeIdx < 0 {
		return nil, fmt.Errorf("npz: header shape tuple not closed")
	}
	inner := rest[open+1 : open+closeIdx]
	shape := []int{}
	for _, tok := range strings.Split(inner, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		n, err := strconv.Atoi(tok)
		if err != nil {
			return nil, fmt.Errorf("npz: bad shape dimension %q", tok)
		}
		shape = append(shape, n)
	}
	return shape, nil
}

// decodeValues decodes count little-endian values of the given numpy dtype,
// widening each to float64. The bool return is false for unrecognised dtypes.
func decodeValues(descr string, data []byte, count int) ([]float64, bool, error) {
	out := make([]float64, count)
	switch descr {
	case "<f8", "f8":
		if len(data) < count*8 {
			return nil, false, fmt.Errorf("npz: truncated <f8 data")
		}
		for i := 0; i < count; i++ {
			bits := binary.LittleEndian.Uint64(data[i*8 : i*8+8])
			out[i] = math.Float64frombits(bits)
		}
	case "<f4", "f4":
		if len(data) < count*4 {
			return nil, false, fmt.Errorf("npz: truncated <f4 data")
		}
		for i := 0; i < count; i++ {
			bits := binary.LittleEndian.Uint32(data[i*4 : i*4+4])
			out[i] = float64(math.Float32frombits(bits))
		}
	case "<i8", "i8":
		if len(data) < count*8 {
			return nil, false, fmt.Errorf("npz: truncated <i8 data")
		}
		for i := 0; i < count; i++ {
			out[i] = float64(int64(binary.LittleEndian.Uint64(data[i*8 : i*8+8])))
		}
	case "<i4", "i4":
		if len(data) < count*4 {
			return nil, false, fmt.Errorf("npz: truncated <i4 data")
		}
		for i := 0; i < count; i++ {
			out[i] = float64(int32(binary.LittleEndian.Uint32(data[i*4 : i*4+4])))
		}
	case "|b1", "b1":
		if len(data) < count {
			return nil, false, fmt.Errorf("npz: truncated |b1 data")
		}
		for i := 0; i < count; i++ {
			if data[i] != 0 {
				out[i] = 1.0
			}
		}
	default:
		return nil, false, nil
	}
	return out, true, nil
}
