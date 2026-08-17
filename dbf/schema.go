package dbf

import (
	"fmt"
	"strings"
)

const (
	MaxFieldNameLength = 11

	// MaxFields is the largest field count a dBASE III+ header can
	// describe. The header size is a uint16 holding 32 + n*32 + 1, so
	// n is bounded by (65535-33)/32. This is a format limit, not a
	// convention: Clipper wrote tables with 159 fields, which an
	// earlier, invented limit of 128 rejected.
	MaxFields = (65535 - 33) / 32 // 2046

	// MaxRecordSize is the largest record a dBASE III+ header can
	// describe, the record size field being a uint16.
	MaxRecordSize = 65535
)

// Validate verifies that the schema can be stored as a dBASE III
// table. Used at Create time: enforces every invariant including
// field-name uniqueness.
//
// Open uses the more permissive validateForOpen (unexported)
// because real Clipper-written tables in the wild carry duplicate
// field names — Clipper never enforced uniqueness in the on-disk
// header. The specimen that surfaced this (T-07) is UM.DBF in a
// production POS/MTS Clipper application: three fields all named
// ACCUMDSUM, plain 0x03 version byte, no DBC sidecar. Rejecting
// such tables on Open makes real corpora unreadable; enforcing
// uniqueness on Create keeps blipper honest.
//
// When a duplicate exists, Record.Get(schema, name) resolves to
// the first matching field (schemaFieldIndex is a linear scan).
// This matches Clipper's own behavior. Callers who need
// positional access to a later duplicate should use GetIndex/
// SetIndex.
func (s *Schema) Validate() error {
	return s.validate(true)
}

// validateForOpen is the permissive variant used when reading an
// existing table header. Same checks as Validate but tolerates
// duplicate field names.
func (s *Schema) validateForOpen() error {
	return s.validate(false)
}

// validate is the shared body. strict=true rejects duplicate
// field names; strict=false tolerates them.
func (s *Schema) validate(strict bool) error {
	if len(s.Fields) == 0 {
		return fmt.Errorf("schema contains no fields")
	}

	if len(s.Fields) > MaxFields {
		return fmt.Errorf("too many fields: %d exceeds the %d a dBASE III+ header can describe",
			len(s.Fields), MaxFields)
	}

	// Guard the record size before RecordSize can truncate it: both
	// it and HeaderSize return uint16 and would otherwise wrap.
	recordSize := 1
	for _, f := range s.Fields {
		recordSize += int(f.Length)
	}

	if recordSize > MaxRecordSize {
		return fmt.Errorf("record size %d exceeds the %d a dBASE III+ header can describe",
			recordSize, MaxRecordSize)
	}

	names := make(map[string]struct{}, len(s.Fields))

	for i, f := range s.Fields {
		name := strings.TrimSpace(f.Name)

		if name == "" {
			return fmt.Errorf("field %d has no name", i)
		}

		if len(name) > MaxFieldNameLength {
			return fmt.Errorf("field %q exceeds %d characters", name, MaxFieldNameLength)
		}

		key := strings.ToUpper(name)

		if strict {
			if _, ok := names[key]; ok {
				return fmt.Errorf("duplicate field %q", name)
			}
			names[key] = struct{}{}
		}

		switch f.Type {

		case Character, Varchar, Varbinary:
			if f.Length == 0 {
				return fmt.Errorf("%s: field has zero length", name)
			}

		case Numeric:
			if f.Length == 0 {
				return fmt.Errorf("%s: numeric field has zero length", name)
			}
			if f.Decimals >= f.Length {
				return fmt.Errorf("%s: invalid decimal count", name)
			}

		case Float:
			if f.Length == 0 {
				return fmt.Errorf("%s: float field has zero length", name)
			}
			if f.Decimals >= f.Length {
				return fmt.Errorf("%s: invalid decimal count", name)
			}

		case Date:
			if f.Length != 8 {
				return fmt.Errorf("%s: date fields must be 8 bytes", name)
			}

		case Logical:
			if f.Length != 1 {
				return fmt.Errorf("%s: logical fields occupy one byte", name)
			}

		case Memo:
			// Two widths are legal, because the two lineages
			// store the block number differently. dBASE III+
			// and FoxPro 2 write it as a 10-byte right-aligned
			// ASCII string; Visual FoxPro writes a 4-byte
			// little-endian integer. Verified across the vendor
			// specimens in dbf/testdata/vfp/, where every VFP
			// memo field is 4 bytes and every dBASE one is 10.
			if f.Length != 10 && f.Length != 4 {
				return fmt.Errorf("%s: memo fields are 10 bytes (dBASE, ASCII pointer) or 4 bytes (VFP, binary pointer), got %d",
					name, f.Length)
			}

		case DBaseBinary, DBaseGeneral:
			// Same 10-digit ASCII pointer convention as dBASE's
			// own Memo, and always exactly 10 — unlike Memo,
			// these two values only ever arise from a table
			// already confirmed dBASE-lineage (see readField),
			// so there is no VFP alternative width to accept.
			if f.Length != 10 {
				return fmt.Errorf("%s: dBASE B/G fields are 10-byte ASCII .DBT pointers, got %d",
					name, f.Length)
			}

		case System:
			// No width constraint — 1 or 2 bytes observed
			// depending on the nullable-field count.

		case Integer, Double, Currency, General, DateTime, Blob:
			// The VFP binary types have fixed widths that are
			// not configurable; a file declaring a different one
			// is malformed.
			want, _ := vfpFieldWidth(f.Type)
			if int(f.Length) != want {
				return fmt.Errorf("%s: %c fields are %d bytes, got %d",
					name, rune(f.Type), want, f.Length)
			}

		default:
			return fmt.Errorf("%s: unsupported field type %q", name, rune(f.Type))
		}
	}

	return nil
}

// RecordSize returns the size of one record including the deletion flag.
func (s *Schema) RecordSize() uint16 {
	size := 1 // deletion marker

	for _, f := range s.Fields {
		size += int(f.Length)
	}

	return uint16(size)
}

// HeaderSize returns the size of the DBF header.
func (s *Schema) HeaderSize() uint16 {
	// 32-byte file header
	// 32 bytes per field descriptor
	// 1-byte header terminator (0x0D)

	return uint16(32 + len(s.Fields)*32 + 1)
}

// HasMemo reports whether the schema contains a Memo field, which
// implies the table is accompanied by a .DBT memo file.
func (s *Schema) HasMemo() bool {
	for _, f := range s.Fields {
		if f.Type == Memo {
			return true
		}
	}

	return false
}
