package dbf

import (
	"errors"
	"fmt"
)

// _NullFlags bit allocation, fully solved.
//
// The partial derivation shipped in v0.9.14 (bit N = Nth nullable
// field, in declaration order) was correct against every specimen
// it was checked against, but incomplete: none of those specimens
// contained a Varchar or Varbinary field. A worked, byte-exact
// example found 2026-07-24 in a VFP 9 book chapter (source S10,
// docs/VFP30_FORMAT.md) gives the complete rule, verified against
// all seven of its documented records with an exact match.
//
// Walking fields in declaration order, each field allocates bits
// as follows:
//
//   - If the field's type is Varchar or Varbinary, it allocates
//     one "full" bit — 0 means content fills the field exactly;
//     1 means content is shorter, with the actual length stored
//     in the field's own last byte.
//   - If the field is Nullable (regardless of type), it allocates
//     one "null" bit — 0 not null, 1 null.
//   - A field that is both Varchar/Varbinary AND nullable gets
//     both bits, full-bit first (lower), null-bit second (higher),
//     adjacent — matching S7's "varlength bit, then null bit"
//     description independently.
//   - When such a field's value is NULL, both bits are set,
//     because a null value is inherently not full.
//
// This generalises the v0.9.14 result rather than replacing it: on
// a table with no Varchar/Varbinary fields, every nullable field
// allocates exactly one bit each, which is exactly what was
// verified against the five real Northwind fields in v0.9.14.
//
// Still untested: behavior when columns are added to or dropped
// from a table after creation, which could in principle leave gaps
// in the bit assignment. No specimen exercises this.
var (
	ErrNoNullFlagsField = errors.New("dbf: table has no _NullFlags system field")
	ErrNotNullable      = errors.New("dbf: field is not nullable")
	ErrNotVarLen        = errors.New("dbf: field is not Varchar/Varbinary")
)

// isVarLenType reports whether t allocates a _NullFlags "full" bit.
func isVarLenType(t FieldType) bool {
	return t == Varchar || t == Varbinary
}

// nullBitPosition returns the zero-based bit position of
// fieldIndex's NULL-status bit in the _NullFlags bitmap. For a
// Varchar/Varbinary field this is the higher of its two bits — see
// fullBitPosition for the lower one.
func nullBitPosition(schema Schema, fieldIndex int) (int, error) {
	if fieldIndex < 0 || fieldIndex >= len(schema.Fields) {
		return 0, fmt.Errorf("field index %d out of range", fieldIndex)
	}
	if !schema.Fields[fieldIndex].Nullable {
		return 0, fmt.Errorf("%w: %q", ErrNotNullable, schema.Fields[fieldIndex].Name)
	}
	pos := 0
	for i := 0; i < fieldIndex; i++ {
		f := schema.Fields[i]
		if isVarLenType(f.Type) {
			pos++
		}
		if f.Nullable {
			pos++
		}
	}
	// This field's own full-bit, if it has one, precedes its
	// null-bit.
	if isVarLenType(schema.Fields[fieldIndex].Type) {
		pos++
	}
	return pos, nil
}

// fullBitPosition returns the zero-based bit position of
// fieldIndex's "full" bit. Only meaningful for Varchar/Varbinary
// fields; returns ErrNotVarLen otherwise rather than a position
// that would silently mean something else.
func fullBitPosition(schema Schema, fieldIndex int) (int, error) {
	if fieldIndex < 0 || fieldIndex >= len(schema.Fields) {
		return 0, fmt.Errorf("field index %d out of range", fieldIndex)
	}
	if !isVarLenType(schema.Fields[fieldIndex].Type) {
		return 0, fmt.Errorf("%w: %q", ErrNotVarLen, schema.Fields[fieldIndex].Name)
	}
	pos := 0
	for i := 0; i < fieldIndex; i++ {
		f := schema.Fields[i]
		if isVarLenType(f.Type) {
			pos++
		}
		if f.Nullable {
			pos++
		}
	}
	return pos, nil
}

// nullFlagsFieldIndex locates the schema's hidden system field.
func nullFlagsFieldIndex(schema Schema) (int, bool) {
	for i, f := range schema.Fields {
		if f.SystemColumn {
			return i, true
		}
	}
	return 0, false
}

// bitSet reports whether the given bit (0-indexed, little-endian
// across raw's bytes) is set. A bit position beyond raw's actual
// width reads as unset rather than erroring — the safe reading
// given the untested add/drop-column case documented above.
//
// Standalone from readNullFlagsBit so decodeRecord (T-36,
// dbf/record_codec.go) can test a bit directly against a record's
// raw bytes before a Record value exists to read it back from.
func bitSet(raw []byte, bit int) bool {
	byteIdx := bit / 8
	if byteIdx >= len(raw) {
		return false
	}
	return raw[byteIdx]&(1<<(uint(bit)%8)) != 0
}

func readNullFlagsBit(r Record, schema Schema, bit int) (bool, error) {
	sysIndex, ok := nullFlagsFieldIndex(schema)
	if !ok {
		return false, ErrNoNullFlagsField
	}
	raw, ok := r.Values[sysIndex].([]byte)
	if !ok {
		return false, fmt.Errorf("dbf: _NullFlags field decoded as %T, want []byte", r.Values[sysIndex])
	}
	return bitSet(raw, bit), nil
}

// IsNull reports whether the named field is NULL in this record.
//
// Only meaningful for Visual FoxPro tables carrying a _NullFlags
// system field; returns ErrNoNullFlagsField otherwise, and
// ErrNotNullable if the named field was not declared nullable
// (byte 18 bit 0x02) regardless of whether a _NullFlags field
// exists.
func (r Record) IsNull(schema Schema, name string) (bool, error) {
	fieldIndex, err := schemaFieldIndex(schema, name)
	if err != nil {
		return false, err
	}
	bit, err := nullBitPosition(schema, fieldIndex)
	if err != nil {
		return false, err
	}
	return readNullFlagsBit(r, schema, bit)
}

// IsFull reports whether the named Varchar/Varbinary field's
// stored content fills the field exactly (true) or is shorter,
// with the actual length in the field's last byte (false).
//
// Returns ErrNotVarLen for any other field type.
func (r Record) IsFull(schema Schema, name string) (bool, error) {
	fieldIndex, err := schemaFieldIndex(schema, name)
	if err != nil {
		return false, err
	}
	bit, err := fullBitPosition(schema, fieldIndex)
	if err != nil {
		return false, err
	}
	notFull, err := readNullFlagsBit(r, schema, bit)
	if err != nil {
		return false, err
	}
	return !notFull, nil
}
