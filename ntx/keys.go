package ntx

import (
	"fmt"
	"math"
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

// NumericKey encodes a number the way Clipper does for an NTX index
// key: the absolute value rendered right aligned and ZERO padded in a
// field of the given length with the given decimal count.
//
// Negative values additionally receive Clipper's digit transform,
// out = 0x5C - in, applied to every byte except the decimal point.
// Because ',' (0x2C) < '.' (0x2E) < '0' (0x30), this makes every
// negative key sort below every positive one under plain byte
// comparison, and within negatives a larger magnitude sorts first.
//
// Note that keys are zero padded, not space padded as a DBF record
// field is: a leading space would collate below every digit and
// corrupt the ordering. Verified against Clipper 5.2e output; see
// docs/CLIPPER_ORACLE.md §9.
func NumericKey(value float64, length, decimals int) ([]byte, error) {
	if length <= 0 || length > MaxKeySize {
		return nil, fmt.Errorf("bad numeric key length %d", length)
	}

	negative := math.Signbit(value) && value != 0

	key, err := dbf.FormatNumeric(math.Abs(value), length, decimals, dbf.PadZero)
	if err != nil {
		return nil, err
	}

	if negative {
		key = dbf.ApplyNegativeKeyTransform(key)
	}

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
