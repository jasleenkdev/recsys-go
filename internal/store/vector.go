// internal/store/vector.go
package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// VectorLiteral formats a Go float slice as pgvector's text input
// format, e.g. "[0.1,0.2,0.3]" — used when inserting/updating a
// vector column via database/sql, which has no native vector type.
func VectorLiteral(v []float64) string {
	strs := make([]string, len(v))
	for i, f := range v {
		strs[i] = fmt.Sprintf("%f", f)
	}
	return "[" + strings.Join(strs, ",") + "]"
}

// ParsePgvectorText parses pgvector's text output format "[0.1,0.2,...]"
// back into a Go float slice.
func ParsePgvectorText(s string) ([]float64, error) {
	var vec []float64
	s = s[1 : len(s)-1]
	if s == "" {
		return nil, fmt.Errorf("empty vector")
	}
	dec := json.NewDecoder(bytes.NewReader([]byte("[" + s + "]")))
	if err := dec.Decode(&vec); err != nil {
		return nil, err
	}
	return vec, nil
}