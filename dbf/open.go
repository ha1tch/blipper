package dbf

import (
	"fmt"
	"io"
)

// Open reads the header and field descriptors of an existing DBF file
// and returns a Table positioned over it.
//
// The on-disk record size must agree with the record size computed
// from the field descriptors. The on-disk header size is authoritative
// for locating record data: some writers pad the header beyond the
// minimum, and Open honours that.
//
// Open does not close rw; the caller retains ownership of the
// underlying file.
func Open(rw io.ReadWriteSeeker) (*Table, error) {
	if _, err := rw.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	header, info, err := readHeader(rw)
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}

	fields, err := readFields(rw, isDBaseLineage(info.versionByte))
	if err != nil {
		return nil, fmt.Errorf("reading field descriptors: %w", err)
	}

	schema := Schema{Fields: fields}

	if err := schema.validateForOpen(); err != nil {
		return nil, fmt.Errorf("invalid schema: %w", err)
	}

	if got, want := info.recordSize, schema.RecordSize(); got != want {
		return nil, fmt.Errorf(
			"header record size %d disagrees with field descriptors (%d)",
			got,
			want,
		)
	}

	if info.headerSize < schema.HeaderSize() {
		return nil, fmt.Errorf(
			"header size %d too small for %d fields",
			info.headerSize,
			len(fields),
		)
	}

	// Parse the VFP backlink from the padded region if byte 28
	// bit 2 (DBC-owned) is set. The backlink is 263 bytes,
	// NUL-terminated, holding a relative path to the sibling
	// .DBC. See T-10's design detail.
	//
	// Truth-table enforcement: bit 3 without bit 2 is a
	// documented never-should-happen invariant. Refuse to open
	// such files rather than silently mis-interpret them.
	if info.tableFlags&dbfTableFlagBlipper != 0 && info.tableFlags&dbfTableFlagDBC == 0 {
		return nil, fmt.Errorf("malformed header: byte 28 = 0x%02X (blipper bit set without DBC bit)", info.tableFlags)
	}
	if info.tableFlags&dbfTableFlagDBC != 0 {
		paddingLen := int(info.headerSize) - int(schema.HeaderSize())
		if paddingLen < dbfBacklinkSize {
			return nil, fmt.Errorf("DBC-owned table: header padding %d bytes, want at least %d for backlink", paddingLen, dbfBacklinkSize)
		}
		var pad [dbfBacklinkSize]byte
		if _, err := io.ReadFull(rw, pad[:]); err != nil {
			return nil, fmt.Errorf("reading backlink: %w", err)
		}
		info.backlink = trimBacklink(pad[:])
	}

	// A declared code page is honoured; an unsupported one is
	// reported rather than silently ignored, so a caller learns
	// their file says something blipper cannot interpret. An
	// absent one leaves the identity codec in place.
	codec, err := newTextCodec(CodePage(header.CodePage))
	if err != nil {
		return nil, err
	}

	// T-27: record which resolution tier this is, for
	// Table.Encoding() introspection. blipperfs.resolveSiblings
	// may override this to EncodingSourceCpgSidecar afterward if
	// a `.cpg` sibling exists; a caller may override it further
	// still via SetCodePage/SetEncoding.
	enc := Encoding{Source: EncodingSourceIdentity, Name: "identity", codec: codec}
	if CodePage(header.CodePage) != CodePageNone {
		enc = Encoding{Source: EncodingSourceHeaderByte29, Name: CodePage(header.CodePage).String(), codec: codec}
	}

	return &Table{
		rw:          rw,
		header:      header,
		schema:      schema,
		recordCount: info.recordCount,
		recordStart: int64(info.headerSize),
		hasMemo:     info.hasMemo,
		versionByte: info.versionByte,
		tableFlags:  info.tableFlags,
		backlink:    info.backlink,
		codec:       codec,
		encoding:    enc,
	}, nil
}
