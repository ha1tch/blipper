package dbf

import (
	"fmt"
	"io"
	"time"
)

// Create writes a new, empty DBF file to rw and returns a Table
// positioned over it.
//
// The resulting file contains the header, the field descriptors, the
// header terminator and the end-of-file marker, and is readable by
// dBASE III+ and Clipper 5.x.
//
// Create does not close rw; the caller retains ownership of the
// underlying file.
func Create(rw io.ReadWriteSeeker, schema Schema) (*Table, error) {
	if err := schema.Validate(); err != nil {
		return nil, fmt.Errorf("invalid schema: %w", err)
	}

	header := Header{
		LastUpdate: time.Now(),
	}

	// A schema carrying a Memo field implies an accompanying memo
	// file. dbf.Create defaults to the DBT-flavour version byte
	// (0x83) when a memo field is present, matching how blipper
	// has always written such tables. Callers who want an
	// FPT-flavour table (0xF5) should call CreateFPTFlavour
	// instead.
	hasMemo := schema.HasMemo()
	versionByte := byte(dbfVersion)
	if hasMemo {
		versionByte = dbfVersionMemo
	}

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

// CreateWithBacklink creates a new DBC-owned table: byte 28 = 0x0C
// (VFP DBC-owned bit plus blipper provenance bit, per T-10's
// truth table), a 263-byte backlink written between the field
// terminator and the first record, and header size inflated to
// include the backlink.
//
// backlinkPath is the relative path to the sibling .DBC as VFP
// would resolve it (typically "TABLE.DBC" when the .DBC lives
// alongside the .DBF). It is written NUL-terminated in the
// backlink region; anything past the first NUL is padding.
// Paths longer than 262 bytes are rejected.
//
// The caller is responsible for creating the .DBC itself (via
// the dbc package) and registering this table's fields there.
// This function only writes the DBF side of the pairing.
func CreateWithBacklink(rw io.ReadWriteSeeker, schema Schema, backlinkPath string) (*Table, error) {
	if err := schema.Validate(); err != nil {
		return nil, fmt.Errorf("invalid schema: %w", err)
	}
	if len(backlinkPath) >= dbfBacklinkSize {
		return nil, fmt.Errorf("backlink path %q too long (max %d bytes)", backlinkPath, dbfBacklinkSize-1)
	}

	header := Header{LastUpdate: time.Now()}
	hasMemo := schema.HasMemo()
	versionByte := byte(dbfVersion)
	if hasMemo {
		versionByte = dbfVersionMemo
	}

	// The physical header size includes the backlink.
	physHeaderSize := schema.HeaderSize() + uint16(dbfBacklinkSize)

	if _, err := rw.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if err := writeHeader(rw, header, physHeaderSize, schema.RecordSize(), 0, versionByte, dbfTableFlagDBCPair); err != nil {
		return nil, fmt.Errorf("writing header: %w", err)
	}
	if err := writeFields(rw, schema.Fields); err != nil {
		return nil, fmt.Errorf("writing field descriptors: %w", err)
	}
	// Backlink region — 263 bytes between the field terminator
	// (written by writeFields) and the fileTerminator/first
	// record position.
	var backlink [dbfBacklinkSize]byte
	copy(backlink[:], []byte(backlinkPath))
	if _, err := rw.Write(backlink[:]); err != nil {
		return nil, fmt.Errorf("writing backlink: %w", err)
	}
	if _, err := rw.Write([]byte{fileTerminator}); err != nil {
		return nil, fmt.Errorf("writing EOF marker: %w", err)
	}

	return &Table{
		rw:          rw,
		hasMemo:     hasMemo,
		versionByte: versionByte,
		tableFlags:  dbfTableFlagDBCPair,
		backlink:    backlinkPath,
		header:      header,
		schema:      schema,
		recordCount: 0,
		recordStart: int64(physHeaderSize),
	}, nil
}
