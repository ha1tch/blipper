// Package dbc implements a subset of Visual FoxPro 3.0's Database
// Container (.DBC) format sufficient to store long field names
// for a companion table (T-10).
//
// A .DBC is itself a DBF file with a fixed 8-column schema. Rows
// form a self-referencing tree via PARENTID: a single Database
// row at the root, one Table row per registered table, and one
// Field row per long-named field. Long names live in OBJECTNAME
// (up to 128 characters).
//
// This package writes and reads that structure. It deliberately
// does NOT populate the memo columns (PROPERTY, CODE, USER)
// because blipper does not implement the features they encode
// (validation rules, stored procedures, user metadata); leaving
// them empty avoids the need for a DBC-side memo file. It also
// does not claim the VFP-only version byte 0x30 — blipper's
// .DBC files use 0x03, matching the rest of the codebase.
//
// See docs/TRACKING.md (T-10) and the truth table in the same
// document for how a Catalogue pairs with a Table via byte 28
// of the DBF header.
package dbc

// Column names and physical dimensions matching VFP's DBC schema.
// The order here is the order the columns appear in the DBF.
const (
	FieldObjectID   = "OBJECTID"
	FieldParentID   = "PARENTID"
	FieldObjectType = "OBJECTTYPE"
	FieldObjectName = "OBJECTNAME"
	FieldProperty   = "PROPERTY"
	FieldCode       = "CODE"
	FieldRIInfo     = "RIINFO"
	FieldUser       = "USER"
)

// OBJECTTYPE values. VFP defines more; blipper writes only these
// three and refuses others on Open with a clear error rather than
// silently misinterpreting an unfamiliar row.
const (
	ObjectTypeDatabase = "Database"
	ObjectTypeTable    = "Table"
	ObjectTypeField    = "Field"
)

// MaxLongName is the maximum length of an OBJECTNAME value,
// bounded by the field's on-disk width. VFP's own limit for a
// long field name is 128; the DBC field width is 128 too.
const MaxLongName = 128

// ObjectID identifies one row in the DBC's self-referencing tree.
// The root Database row has ID 1, each subsequent object gets the
// next unused ID. IDs are never reused within a Catalogue.
type ObjectID uint32

// RootID is the OBJECTID of the Database row; the first row of
// any freshly-created .DBC.
const RootID ObjectID = 1

// A row groups the fields common to every DBC row after decode.
// It is an internal type; callers see Catalogue/Object accessors.
type row struct {
	ObjectID   ObjectID
	ParentID   ObjectID // 0 for the Database row itself
	ObjectType string   // one of the ObjectType* constants
	ObjectName string   // long name; unbounded input, truncated to MaxLongName on write
}
