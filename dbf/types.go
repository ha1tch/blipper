package dbf

import (
	"io"
	"time"
)

// FieldType identifies one of the standard dBASE III+ field types.
type FieldType byte

const (
	Character FieldType = 'C'
	Numeric   FieldType = 'N'
	Logical   FieldType = 'L'
	Date      FieldType = 'D'

	// System is field-type byte '0' (ASCII 0x30, ordinary digit
	// character, not the null byte 0x00 — corrected 2026-07-24
	// against real data; earlier notes carried from secondary
	// documentation had this wrong). Used only by the hidden
	// _NullFlags column VFP appends to a table with any nullable
	// or Varchar/Varbinary field. Decoded as raw bytes: see
	// dbf/nullflags.go.
	System FieldType = '0'

	Float FieldType = 'F'
	Memo  FieldType = 'M'
)

// isSupportedType reports whether this package supports the field type.
func isSupportedType(t FieldType) bool {
	switch t {
	case Character,
		Numeric,
		Logical,
		Date,
		Float,
		Memo,
		Varchar,
		Varbinary:
		return true
	case Integer, Double, Currency, General, DateTime, Blob:
		// Visual FoxPro binary types. Accepted on read so a 0x30
		// table opens; see dbf/vfptypes.go for what is and is not
		// implemented.
		return true
	case System:
		// The hidden _NullFlags column. See dbf/nullflags.go.
		return true
	case DBaseBinary, DBaseGeneral:
		// dBASE III+/IV/5.0's own B/G meaning. Never appears on
		// disk under these values — remapped from 'B'/'G' by
		// readField. See dbf/dbasetypes.go.
		return true
	default:
		return false
	}
}

// Field describes a single column in a DBF table.
type Field struct {
	Name     string
	Type     FieldType
	Length   uint8
	Decimals uint8

	// SystemColumn, Nullable and Binary mirror header byte 18's
	// three low bits (0x01, 0x02, 0x04), Visual FoxPro only.
	// Zero/false for every dBASE III+ and Clipper field, since
	// that byte is reserved-for-LAN-use in that lineage rather
	// than a flags byte.
	SystemColumn bool
	Nullable     bool
	Binary       bool
}

// Schema describes the logical layout of a DBF table.
//
// A schema is considered immutable after a table has been created.
type Schema struct {
	Fields []Field
}

// Header contains the logical metadata stored in the DBF file header.
type Header struct {
	LastUpdate time.Time
	CodePage   byte
}

// Record represents one logical DBF record.
//
// Values correspond positionally to Schema.Fields.
//
// The zero value is not guaranteed to represent a valid DBF record;
// use NewRecord when creating records for a schema.
type Record struct {
	Deleted bool
	Values  []any
}

// Table represents an open DBF file.
type Table struct {
	rw io.ReadWriteSeeker

	header Header
	schema Schema

	recordCount uint32
	recordStart int64

	// hasMemo mirrors bit 7 of the on-disk version byte.
	hasMemo bool

	// versionByte is the raw first byte of the on-disk header,
	// preserved across rewrites so that opening an FPT-flavour
	// table (0xF5) and writing back does not silently demote it
	// to DBT-flavour (0x83).
	versionByte byte

	// tableFlags is byte 28 of the on-disk header (see T-10's
	// truth table). Preserved across rewrites for the same
	// reason as versionByte: to keep DBC-owned tables
	// identifying as DBC-owned.
	tableFlags byte

	// backlink is the 263-byte VFP backlink (parsed as a
	// NUL-terminated relative path to the sibling .DBC). Empty
	// when no backlink is present.
	backlink string

	// codec converts text fields between the file's declared
	// code page and Go strings. The zero value passes bytes
	// through, which is what a file declaring no language
	// driver gets.
	codec textCodec

	// encoding tracks which of the four resolution tiers
	// (explicit override, .cpg sidecar, header byte 29, identity)
	// currently governs codec, purely for introspection — T-27.
	// Open sets it from header byte 29; SetCodePage/SetEncoding
	// update it when called, always winning by virtue of being
	// explicit and called after whatever Open or sidecar
	// resolution already did.
	encoding Encoding
}

// Cursor provides sequential access to records in a Table.
type Cursor struct {
	table   *Table
	recno   uint32
	current Record
	err     error
}
