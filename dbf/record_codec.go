package dbf

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	activeMarker  = ' '
	deletedMarker = '*'
)

// encodeRecord encodes a logical record into dst using the schema.
//
// dst must be exactly schema.RecordSize() bytes.
//
// Encoding follows dBASE III+ / Clipper conventions:
//
//   - Character values are stored left-aligned, space padded. Values
//     longer than the field are truncated, matching Clipper REPLACE
//     semantics.
//   - Numeric and Float values are stored as right-aligned ASCII.
//     A value that does not fit the field width is an error (Clipper
//     stores asterisks in this case; refusing is strictly safer).
//   - Dates are stored as YYYYMMDD; the zero time is stored blank.
//   - Logical values are stored as 'T' or 'F'.
//   - Memo values are stored as the raw 10-byte block reference so
//     that read-modify-write cycles preserve any memo pointers found
//     in existing files. This package does not interpret memo data.
func encodeRecord(dst []byte, schema Schema, record Record) error {
	if len(dst) != int(schema.RecordSize()) {
		return fmt.Errorf(
			"encode buffer is %d bytes, record size is %d",
			len(dst),
			schema.RecordSize(),
		)
	}

	if len(record.Values) != len(schema.Fields) {
		return fmt.Errorf(
			"record has %d values, schema has %d fields",
			len(record.Values),
			len(schema.Fields),
		)
	}

	if record.Deleted {
		dst[0] = deletedMarker
	} else {
		dst[0] = activeMarker
	}

	pos := 1

	for i, field := range schema.Fields {
		out := dst[pos : pos+int(field.Length)]

		if err := encodeValue(out, field, record.Values[i]); err != nil {
			return fmt.Errorf("field %q: %w", field.Name, err)
		}

		pos += int(field.Length)
	}

	return nil
}

// decodeRecord decodes one raw DBF record according to the schema.
func decodeRecord(src []byte, schema Schema) (Record, error) {
	if len(src) != int(schema.RecordSize()) {
		return Record{}, fmt.Errorf(
			"%w: record is %d bytes, expected %d",
			ErrInvalidRecord,
			len(src),
			schema.RecordSize(),
		)
	}

	record := Record{
		Values: make([]any, len(schema.Fields)),
	}

	switch src[0] {
	case activeMarker:
		record.Deleted = false
	case deletedMarker:
		record.Deleted = true
	default:
		return Record{}, fmt.Errorf(
			"%w: bad deletion marker 0x%02X",
			ErrInvalidRecord,
			src[0],
		)
	}

	pos := 1

	for i, field := range schema.Fields {
		raw := src[pos : pos+int(field.Length)]

		value, err := decodeValue(raw, field)
		if err != nil {
			return Record{}, fmt.Errorf("field %q: %w", field.Name, err)
		}

		record.Values[i] = value
		pos += int(field.Length)
	}

	return record, nil
}

func encodeValue(dst []byte, field Field, value any) error {
	switch field.Type {

	case Character, Memo:
		s, ok := stringValue(value)
		if !ok {
			return fmt.Errorf("expected string, got %T", value)
		}
		encodePadded(dst, s)
		return nil

	case Numeric, Float:
		return encodeNumeric(dst, field, value)

	case Date:
		t, ok := value.(time.Time)
		if !ok && value != nil {
			return fmt.Errorf("expected time.Time, got %T", value)
		}
		return encodeDate(dst, t)

	case Logical:
		b, ok := value.(bool)
		if !ok && value != nil {
			return fmt.Errorf("expected bool, got %T", value)
		}
		if b {
			dst[0] = 'T'
		} else {
			dst[0] = 'F'
		}
		return nil

	default:
		return fmt.Errorf("%w: field type %q", ErrUnsupported, rune(field.Type))
	}
}

func decodeValue(raw []byte, field Field) (any, error) {
	switch field.Type {

	case Character, Memo:
		return strings.TrimRight(string(raw), " "), nil

	case Numeric, Float:
		return decodeNumeric(raw, field)

	case Date:
		return decodeDate(raw)

	case Logical:
		switch raw[0] {
		case 'T', 't', 'Y', 'y':
			return true, nil
		case 'F', 'f', 'N', 'n', ' ', '?':
			// Blank and '?' mark an uninitialized logical field.
			return false, nil
		default:
			return nil, fmt.Errorf(
				"%w: bad logical byte 0x%02X",
				ErrInvalidRecord,
				raw[0],
			)
		}

	default:
		return nil, fmt.Errorf(
			"%w: field type %q",
			ErrUnsupported,
			rune(field.Type),
		)
	}
}

// encodePadded writes s left-aligned into dst, space padded, truncating
// if s is longer than dst.
func encodePadded(dst []byte, s string) {
	n := copy(dst, s)

	for i := n; i < len(dst); i++ {
		dst[i] = ' '
	}
}

func encodeNumeric(dst []byte, field Field, value any) error {
	var text string

	switch {
	case value == nil:
		text = ""

	case field.Type == Numeric && field.Decimals == 0:
		i, ok := intValue(value)
		if !ok {
			return fmt.Errorf("expected integer value, got %T", value)
		}
		text = strconv.FormatInt(i, 10)

	default:
		f, ok := floatValue(value)
		if !ok {
			return fmt.Errorf("expected numeric value, got %T", value)
		}
		text = strconv.FormatFloat(f, 'f', int(field.Decimals), 64)
	}

	if len(text) > len(dst) {
		return fmt.Errorf(
			"value %s does not fit in %d byte field",
			text,
			len(dst),
		)
	}

	// Right aligned, space padded.
	pad := len(dst) - len(text)

	for i := 0; i < pad; i++ {
		dst[i] = ' '
	}

	copy(dst[pad:], text)

	return nil
}

func decodeNumeric(raw []byte, field Field) (any, error) {
	text := strings.TrimSpace(string(raw))

	// A blank numeric field decodes as zero.
	if text == "" {
		if field.Type == Numeric && field.Decimals == 0 {
			return int64(0), nil
		}
		return float64(0), nil
	}

	if field.Type == Numeric && field.Decimals == 0 {
		i, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf(
				"%w: bad numeric value %q",
				ErrInvalidRecord,
				text,
			)
		}
		return i, nil
	}

	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: bad numeric value %q",
			ErrInvalidRecord,
			text,
		)
	}

	return f, nil
}

func encodeDate(dst []byte, t time.Time) error {
	if len(dst) != 8 {
		return fmt.Errorf("date field must be 8 bytes, have %d", len(dst))
	}

	if t.IsZero() {
		encodePadded(dst, "")
		return nil
	}

	year := t.Year()

	if year < 0 || year > 9999 {
		return fmt.Errorf("year %d out of DBF range", year)
	}

	copy(dst, fmt.Sprintf("%04d%02d%02d", year, int(t.Month()), t.Day()))

	return nil
}

func decodeDate(raw []byte) (time.Time, error) {
	text := strings.TrimSpace(string(raw))

	// A blank date field decodes as the zero time.
	if text == "" {
		return time.Time{}, nil
	}

	t, err := time.Parse("20060102", text)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"%w: bad date value %q",
			ErrInvalidRecord,
			text,
		)
	}

	return t, nil
}

func stringValue(v any) (string, bool) {
	if v == nil {
		return "", true
	}
	s, ok := v.(string)
	return s, ok
}

func intValue(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case uint:
		return int64(n), true
	case uint8:
		return int64(n), true
	case uint16:
		return int64(n), true
	case uint32:
		return int64(n), true
	case uint64:
		return int64(n), true
	case float32:
		if float32(int64(n)) == n {
			return int64(n), true
		}
		return 0, false
	case float64:
		if float64(int64(n)) == n {
			return int64(n), true
		}
		return 0, false
	default:
		return 0, false
	}
}

func floatValue(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}
