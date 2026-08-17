package dbf

import (
	"fmt"
	"io"
	"time"
)

// DBaseIVTableKind selects which of the dBASE IV/5.0 lineage's
// three non-III+-compatible version bytes CreateDBaseIV writes.
//
// Named for the version byte's own meaning, not for one product:
// dBASE IV 2.0 (1988) and dBASE 5.0 for DOS (1994) write
// byte-identical headers and field descriptors for all three —
// confirmed directly in T-31 (docs/DBASE_FORMAT.md's dBASE 5.0
// section states this explicitly, and the live write-oracle used
// to verify T-31 was itself dBASE 5.0, not dBASE IV, exercising
// the same code path either product would). A function or type
// named after only one of the two products would misstate what it
// actually covers — the mistake this file's own naming was
// corrected out of before it shipped.
type DBaseIVTableKind byte

const (
	// DBaseIVTable is an ordinary table with a memo field —
	// version byte 0x8B. Requires schema.HasMemo(); a schema
	// without a memo field belongs to Create instead, which
	// already writes valid dBASE IV/5.0-readable output for that
	// case (version byte 0x03, no dBASE-IV-specific behaviour
	// needed at all).
	DBaseIVTable DBaseIVTableKind = DBaseIVTableKind(dbfVersionDBaseIV)

	// DBaseIVSQLTable is a dBASE IV SQL table — version byte
	// 0x43. Refuses a schema with a memo field: every specimen
	// found this session (6 real 1994 files) has the memo bit
	// clear, and nothing confirms what a memo-bearing 0x43 table
	// would even look like.
	DBaseIVSQLTable DBaseIVTableKind = DBaseIVTableKind(dbfVersionDBaseIVSQLTable)

	// DBaseIVSQLSystem is a dBASE IV SQL system file — version
	// byte 0x63. Same memo restriction as DBaseIVSQLTable, and
	// for the same reason: 11 real specimens, all without a memo
	// field.
	DBaseIVSQLSystem DBaseIVTableKind = DBaseIVTableKind(dbfVersionDBaseIVSQLSystem)
)

// String returns a readable name for the kind.
func (k DBaseIVTableKind) String() string {
	switch k {
	case DBaseIVTable:
		return "dBASE IV/5.0 table"
	case DBaseIVSQLTable:
		return "dBASE IV SQL table"
	case DBaseIVSQLSystem:
		return "dBASE IV SQL system file"
	default:
		return fmt.Sprintf("unknown dBASE IV table kind 0x%02X", byte(k))
	}
}

// CreateDBaseIV writes a new table in the dBASE IV/5.0 lineage —
// version byte 0x8B, 0x43, or 0x63 depending on kind. Covers both
// dBASE IV 2.0 and dBASE 5.0 for DOS equally, since the two
// products share this format byte-for-byte; see DBaseIVTableKind's
// doc comment.
//
// Every piece this needs was already built and tested for other
// purposes before this function existed: B/G field-descriptor
// encoding (dbf/dbasetypes.go, verified round-trip in T-31), the
// dBASE IV/5.0 memo file format (dbf/memo_dbaseiv.go, T-37), and
// header bytes 14/15/29 that this format leaves at their
// documented-zero defaults exactly like Create already does for
// ordinary III+ tables. There is no analogue here to T-10's VFP
// write exclusion — that refusal is a promise not yet earned,
// because VFP semantics blipper does not fully implement; nothing
// comparable is true of this lineage, which is dBASE III+'s
// format with a different version byte and two already-solved
// wrinkles.
//
// A schema with a Memo field requires kind == DBaseIVTable; a
// schema without one requires kind == DBaseIVSQLTable or
// DBaseIVSQLSystem. Mismatches are reported rather than silently
// producing a table shaped unlike anything real dBASE 5.0 was
// observed to write.
//
// Byte 28 (the production .MDX flag for this lineage) always
// starts at 0: a freshly created table has no .MDX yet. A caller
// attaching one afterward is responsible for updating that byte
// themselves — not yet exposed as its own function.
func CreateDBaseIV(rw io.ReadWriteSeeker, schema Schema, kind DBaseIVTableKind) (*Table, error) {
	if err := schema.Validate(); err != nil {
		return nil, fmt.Errorf("invalid schema: %w", err)
	}

	hasMemo := schema.HasMemo()
	switch kind {
	case DBaseIVTable:
		if !hasMemo {
			return nil, fmt.Errorf(
				"dbf: CreateDBaseIV(DBaseIVTable) requires a Memo field; "+
					"a schema without one should use Create instead, which already writes %s",
				"dBASE IV/5.0-readable output (version byte 0x03)")
		}
	case DBaseIVSQLTable, DBaseIVSQLSystem:
		if hasMemo {
			return nil, fmt.Errorf(
				"dbf: CreateDBaseIV(%s) refuses a schema with a Memo field: "+
					"no specimen with this combination was found this session, so nothing confirms the correct layout",
				kind)
		}
	default:
		return nil, fmt.Errorf("dbf: %w", fmt.Errorf("unrecognized DBaseIVTableKind 0x%02X", byte(kind)))
	}

	header := Header{LastUpdate: time.Now()}
	versionByte := byte(kind)

	if _, err := rw.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if err := writeHeader(rw, header, schema.HeaderSize(), schema.RecordSize(), 0, versionByte, 0); err != nil {
		return nil, fmt.Errorf("writing header: %w", err)
	}
	if err := writeFields(rw, schema.Fields); err != nil {
		return nil, fmt.Errorf("writing field descriptors: %w", err)
	}
	if _, err := rw.Write([]byte{fileTerminator}); err != nil {
		return nil, fmt.Errorf("writing EOF marker: %w", err)
	}

	codec, err := newTextCodec(CodePage(header.CodePage))
	if err != nil {
		return nil, err
	}

	return &Table{
		rw:          rw,
		hasMemo:     hasMemo,
		versionByte: versionByte,
		tableFlags:  0,
		header:      header,
		schema:      schema,
		recordCount: 0,
		recordStart: int64(schema.HeaderSize()),
		codec:       codec,
	}, nil
}
