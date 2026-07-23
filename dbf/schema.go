package dbf

import (
	"fmt"
	"strings"
)

const (
	MaxFieldNameLength = 11
	MaxFields          = 128 // practical dBASE III+ limit
)

// Validate verifies that the schema can be stored as a dBASE III table.
func (s *Schema) Validate() error {
	if len(s.Fields) == 0 {
		return fmt.Errorf("schema contains no fields")
	}

	if len(s.Fields) > MaxFields {
		return fmt.Errorf("too many fields")
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

		if _, ok := names[key]; ok {
			return fmt.Errorf("duplicate field %q", name)
		}

		names[key] = struct{}{}

		switch f.Type {

		case Character:
			if f.Length == 0 {
				return fmt.Errorf("%s: character field has zero length", name)
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
			if f.Length != 10 {
				// dBASE stores memo block numbers as ASCII.
				return fmt.Errorf("%s: memo fields must be length 10", name)
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
		size += uint16(f.Length)
	}

	return size
}

// HeaderSize returns the size of the DBF header.
func (s *Schema) HeaderSize() uint16 {
	// 32-byte file header
	// 32 bytes per field descriptor
	// 1-byte header terminator (0x0D)

	return uint16(32 + len(s.Fields)*32 + 1)
}