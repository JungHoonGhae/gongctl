// Package output renders results as JSON, JSON Lines, or aligned tables.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"
)

// Format is an output encoding.
type Format string

const (
	// JSON is pretty-printed JSON (default).
	JSON Format = "json"
	// JSONL is newline-delimited JSON, one record per line.
	JSONL Format = "jsonl"
	// Table is a human-readable aligned table.
	Table Format = "table"
)

// Parse validates a format string.
func Parse(s string) (Format, error) {
	switch Format(s) {
	case JSON, JSONL, Table:
		return Format(s), nil
	default:
		return "", fmt.Errorf("unknown format %q (want json, jsonl, or table)", s)
	}
}

// WriteJSON pretty-prints v as JSON.
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// WriteJSONL writes each item as a single JSON object on its own line.
func WriteJSONL(w io.Writer, items []any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, it := range items {
		if err := enc.Encode(it); err != nil {
			return err
		}
	}
	return nil
}

// LineEncoder writes one JSON record per line (JSON Lines / NDJSON).
type LineEncoder struct{ enc *json.Encoder }

// NewLineEncoder returns a streaming JSONL encoder, suitable for large syncs.
func NewLineEncoder(w io.Writer) *LineEncoder {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &LineEncoder{enc: enc}
}

// Encode writes v as a single line of JSON.
func (e *LineEncoder) Encode(v any) error { return e.enc.Encode(v) }

// WriteTable renders an aligned table. Column widths account for the
// double-width of CJK characters so Korean columns line up in a terminal.
func WriteTable(w io.Writer, headers []string, rows [][]string) error {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = displayWidth(h)
	}
	for _, row := range rows {
		for i := 0; i < len(headers) && i < len(row); i++ {
			if dw := displayWidth(row[i]); dw > widths[i] {
				widths[i] = dw
			}
		}
	}

	if err := writeRow(w, headers, widths); err != nil {
		return err
	}
	sep := make([]string, len(headers))
	for i := range sep {
		sep[i] = strings.Repeat("-", widths[i])
	}
	if err := writeRow(w, sep, widths); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writeRow(w, row, widths); err != nil {
			return err
		}
	}
	return nil
}

func writeRow(w io.Writer, cells []string, widths []int) error {
	var b strings.Builder
	for i, width := range widths {
		var cell string
		if i < len(cells) {
			cell = cells[i]
		}
		b.WriteString(cell)
		b.WriteString(strings.Repeat(" ", width-displayWidth(cell)))
		if i < len(widths)-1 {
			b.WriteString("  ")
		}
	}
	_, err := fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	return err
}

// displayWidth counts terminal columns, treating CJK runes as width 2.
func displayWidth(s string) int {
	n := 0
	for _, r := range s {
		if isWide(r) {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// isWide reports whether r occupies two terminal columns (Hangul, CJK, kana,
// and full-width forms).
func isWide(r rune) bool {
	switch {
	case unicode.Is(unicode.Hangul, r):
		return true
	case unicode.Is(unicode.Han, r):
		return true
	case unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r):
		return true
	case r >= 0xFF00 && r <= 0xFF60: // full-width forms
		return true
	case r >= 0x2E80 && r <= 0xA4CF: // CJK radicals .. Yi
		return true
	}
	return false
}
