package ntx

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ha1tch/blipper/dbf"
)

// CharKey encodes a string as a fixed-size character key: left
// aligned, space padded, truncated if longer, matching Clipper's
// fixed-width character keys.
func CharKey(s string, size int) []byte {
	key := make([]byte, size)

	n := copy(key, s)

	for i := n; i < size; i++ {
		key[i] = ' '
	}

	return key
}

// DateKey encodes a date the way Clipper's DTOS() does: YYYYMMDD,
// with the zero time encoding as eight spaces (an empty date).
func DateKey(t time.Time) []byte {
	if t.IsZero() {
		return []byte("        ")
	}

	return []byte(fmt.Sprintf(
		"%04d%02d%02d",
		t.Year(),
		int(t.Month()),
		t.Day(),
	))
}

// LogicalKey encodes a logical value as "T" or "F".
func LogicalKey(b bool) []byte {
	if b {
		return []byte("T")
	}
	return []byte("F")
}

// NumericKey encodes a non-negative number the way Clipper's Str()
// does: right aligned in a field of the given length with the given
// decimal count, space padded. For non-negative values this collates
// numerically under byte comparison.
//
// Negative values are rejected: Clipper applies a byte transform to
// negative numeric keys so they collate below positives, and this
// package does not guess at that encoding until it has been verified
// against real Clipper output (tracked as register item T-01).
func NumericKey(value float64, length, decimals int) ([]byte, error) {
	if value < 0 {
		return nil, fmt.Errorf(
			"%w: negative numeric keys (see register item T-01)",
			dbf.ErrUnsupported,
		)
	}

	if length <= 0 || length > MaxKeySize {
		return nil, fmt.Errorf("bad numeric key length %d", length)
	}

	text := strconv.FormatFloat(value, 'f', decimals, 64)

	if len(text) > length {
		return nil, fmt.Errorf(
			"value %s does not fit a %d byte key",
			text,
			length,
		)
	}

	key := make([]byte, length)

	pad := length - len(text)

	for i := 0; i < pad; i++ {
		key[i] = ' '
	}

	copy(key[pad:], text)

	return key, nil
}

// Build populates an index from every record of a table, deriving
// keys with fn.
//
// Records marked as deleted are included, matching Clipper, which
// keeps deleted and filtered records in its indexes.
func Build(ix *Index, table *dbf.Table, fn KeyFunc) error {
	cursor := table.Cursor()

	for cursor.Next() {
		key := fn(cursor.Record())

		if _, err := ix.Insert(key, cursor.Recno()); err != nil {
			return fmt.Errorf("indexing record %d: %w", cursor.Recno(), err)
		}
	}

	return cursor.Err()
}
