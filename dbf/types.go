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
	Float     FieldType = 'F'
	Memo      FieldType = 'M'
)

// isSupportedType reports whether this package supports the field type.
func isSupportedType(t FieldType) bool {
	switch t {
	case Character,
		Numeric,
		Logical,
		Date,
		Float,
		Memo:
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
}

// Cursor provides sequential access to records in a Table.
type Cursor struct {
	table   *Table
	recno   uint32
	current Record
	err     error
}
