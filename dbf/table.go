package dbf

import (
	"fmt"
	"io"
	"time"
)

// Schema returns the table's schema.
//
// The returned schema must not be modified.
func (t *Table) Schema() Schema {
	return t.schema
}

// Header returns the table's logical header metadata.
func (t *Table) Header() Header {
	return t.header
}

// MemoFormat reports which memo file format the table expects, if
// any. Callers use this to decide which sibling to open and with
// which constructor: OpenMemo for .DBT (dBASE III+ layout),
// OpenDBaseIVMemo for .DBT (dBASE IV/5.0 layout — same extension,
// different physical format), or OpenFPT for .FPT.
//
// The value is derived from the on-disk version byte and preserved
// through header rewrites:
//
//	0x03 → MemoFormatNone (dBASE III+ without memo)
//	0x83 → MemoFormatDBT  (dBASE III+ with .DBT)
//	0xF5 → MemoFormatFPT  (FoxPro-format with .FPT)
//	0x8B → MemoFormatDBaseIV (dBASE IV/5.0 with .DBT, 8-byte block header)
//
// A schema with a Memo field but a version byte of 0x03 is
// possible only through hand-editing and is not distinguished
// here; callers who need that check can compare Schema.HasMemo
// against MemoFormat.
func (t *Table) MemoFormat() MemoFormat {
	switch t.versionByte {
	case dbfVersionFPT:
		return MemoFormatFPT
	case dbfVersionMemo:
		return MemoFormatDBT
	case dbfVersionDBaseIV:
		return MemoFormatDBaseIV
	default:
		return MemoFormatNone
	}
}

// RecordCount returns the number of records in the table, including
// records marked as deleted.
func (t *Table) RecordCount() uint32 {
	return t.recordCount
}

// Get reads the record with the given one-based record number.
func (t *Table) Get(recno uint32) (Record, error) {
	if err := t.checkRecno(recno); err != nil {
		return Record{}, err
	}

	raw := make([]byte, t.schema.RecordSize())

	if _, err := t.rw.Seek(t.recordOffset(recno), io.SeekStart); err != nil {
		return Record{}, err
	}

	if _, err := io.ReadFull(t.rw, raw); err != nil {
		return Record{}, fmt.Errorf("reading record %d: %w", recno, err)
	}

	record, err := decodeRecord(raw, t.schema)
	if err != nil {
		return Record{}, fmt.Errorf("record %d: %w", recno, err)
	}

	// Text fields arrive as raw bytes; convert them from the
	// file's declared code page. The identity codec, which is
	// what a file declaring nothing gets, leaves them alone.
	t.codec.applyDecode(&record, t.schema)

	return record, nil
}

// Put overwrites the record with the given one-based record number.
func (t *Table) Put(recno uint32, record Record) error {
	if err := t.checkRecno(recno); err != nil {
		return err
	}

	raw := make([]byte, t.schema.RecordSize())

	encoded, err := t.codec.applyEncode(record, t.schema)
	if err != nil {
		return fmt.Errorf("record %d: %w", recno, err)
	}
	if err := encodeRecord(raw, t.schema, encoded); err != nil {
		return fmt.Errorf("record %d: %w", recno, err)
	}

	if _, err := t.rw.Seek(t.recordOffset(recno), io.SeekStart); err != nil {
		return err
	}

	if _, err := t.rw.Write(raw); err != nil {
		return fmt.Errorf("writing record %d: %w", recno, err)
	}

	return t.flushHeader()
}

// Append adds a record to the end of the table and returns its
// one-based record number.
func (t *Table) Append(record Record) (uint32, error) {
	raw := make([]byte, t.schema.RecordSize())

	encoded, err := t.codec.applyEncode(record, t.schema)
	if err != nil {
		return 0, err
	}
	if err := encodeRecord(raw, t.schema, encoded); err != nil {
		return 0, err
	}

	recno := t.recordCount + 1

	if _, err := t.rw.Seek(t.recordOffset(recno), io.SeekStart); err != nil {
		return 0, err
	}

	if _, err := t.rw.Write(raw); err != nil {
		return 0, fmt.Errorf("appending record: %w", err)
	}

	// Restore the end-of-file marker after the new last record.
	if _, err := t.rw.Write([]byte{fileTerminator}); err != nil {
		return 0, fmt.Errorf("writing EOF marker: %w", err)
	}

	t.recordCount = recno

	if err := t.flushHeader(); err != nil {
		return 0, err
	}

	return recno, nil
}

// Delete marks the record with the given one-based record number as
// deleted.
//
// Deletion is logical: the record data is preserved and the record
// remains addressable. Physical removal is a future PACK operation.
func (t *Table) Delete(recno uint32) error {
	return t.setDeletionMarker(recno, deletedMarker)
}

// Undelete clears the deletion marker of the record with the given
// one-based record number, corresponding to Clipper's RECALL.
func (t *Table) Undelete(recno uint32) error {
	return t.setDeletionMarker(recno, activeMarker)
}

// Flush rewrites the file header, persisting the record count and
// stamping the last-update date.
func (t *Table) Flush() error {
	return t.flushHeader()
}

func (t *Table) setDeletionMarker(recno uint32, marker byte) error {
	if err := t.checkRecno(recno); err != nil {
		return err
	}

	if _, err := t.rw.Seek(t.recordOffset(recno), io.SeekStart); err != nil {
		return err
	}

	if _, err := t.rw.Write([]byte{marker}); err != nil {
		return fmt.Errorf("marking record %d: %w", recno, err)
	}

	return t.flushHeader()
}

// Reload re-reads the table's header from the underlying stream,
// picking up changes another process may have made — most
// commonly RecordCount, when another process appended records
// since this Table was opened or last reloaded.
//
// T-20: blipper has no automatic cache invalidation for a shared
// reader. Before this, no reload path existed at all — a shared
// reader's Table.RecordCount() simply never changed once Open
// returned, regardless of what any other process wrote. A caller
// sharing a table across processes must call Reload explicitly
// to observe writes made elsewhere; there is no notification
// mechanism, and none is planned — that would be a different,
// larger feature.
//
// Deliberately narrow: only RecordCount and the header's
// LastUpdate are refreshed. Schema and physical layout (header
// size, record size, version byte, table flags) are not re-read.
// Those cannot legitimately change without a structural migration
// this package does not support performing concurrently, and
// re-reading them here would risk silently accepting a
// half-written state as if it were a normal schema change. A
// record-size mismatch is reported as an error rather than
// applied, since it means the file is no longer the table this
// Table was opened against, not that a write is in progress.
//
// This is the one part of T-20 in scope here — cdx, dbc, and
// fatfs each cache their own state independently and need their
// own reload design; see docs/TRACKING.md.
func (t *Table) Reload() error {
	if _, err := t.rw.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("dbf: reload: %w", err)
	}

	header, info, err := readHeader(t.rw)
	if err != nil {
		return fmt.Errorf("dbf: reload: %w", err)
	}
	if info.recordSize != t.schema.RecordSize() {
		return fmt.Errorf(
			"dbf: reload: record size is now %d, was %d — this is a different table, not a concurrent write",
			info.recordSize, t.schema.RecordSize(),
		)
	}

	t.recordCount = info.recordCount
	t.header.LastUpdate = header.LastUpdate

	return nil
}

func (t *Table) checkRecno(recno uint32) error {
	if recno == 0 {
		return fmt.Errorf("record numbers are one-based")
	}

	if recno > t.recordCount {
		return fmt.Errorf("record %d: %w", recno, ErrEOF)
	}

	return nil
}

// recordOffset returns the file offset of a one-based record number.
func (t *Table) recordOffset(recno uint32) int64 {
	return t.recordStart +
		int64(recno-1)*int64(t.schema.RecordSize())
}

func (t *Table) flushHeader() error {
	t.header.LastUpdate = time.Now()

	if _, err := t.rw.Seek(0, io.SeekStart); err != nil {
		return err
	}

	return writeHeader(
		t.rw,
		t.header,
		uint16(t.recordStart),
		t.schema.RecordSize(),
		t.recordCount,
		t.versionByte,
		t.tableFlags,
	)
}

// TableFlags returns byte 28 of the on-disk header, verbatim,
// with no interpretation applied — its meaning depends entirely
// on lineage (see isDBaseLineage, dbf/header.go), and blipper
// deliberately does not decide that for the caller here.
//
// For VFP tables (see T-10's truth table): bit 2 (0x04) means
// DBC-owned; bit 3 (0x08) means blipper-written; combined 0x0C is
// the pair blipper writes for its own DBC-owned tables.
//
// For dBASE IV/5.0 tables (T-31): bit 0 (0x01) means a production
// `.MDX` accompanies the table — confirmed against both the
// original 1994 vendor specimens and a live 2026 write-oracle
// (source S13). Field-descriptor byte 31, documented everywhere
// as the per-field counterpart to this flag, does not reliably
// track current tag membership — see dbf/nullflags.go's neighbour
// and docs/DBASE_FORMAT.md's dBASE IV section. Determine tag
// membership from the sibling .MDX's own tag directory, never
// from either byte.
func (t *Table) TableFlags() byte { return t.tableFlags }

// Backlink returns the relative path to the sibling .DBC parsed
// from the VFP-format backlink region. Empty when byte 28 bit 2
// is not set.
func (t *Table) Backlink() string { return t.backlink }
