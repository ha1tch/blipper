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

	return record, nil
}

// Put overwrites the record with the given one-based record number.
func (t *Table) Put(recno uint32, record Record) error {
	if err := t.checkRecno(recno); err != nil {
		return err
	}

	raw := make([]byte, t.schema.RecordSize())

	if err := encodeRecord(raw, t.schema, record); err != nil {
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

	if err := encodeRecord(raw, t.schema, record); err != nil {
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
	)
}
