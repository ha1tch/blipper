// Package blipperdb: memo file attachment.
//
// This file adds Area-level attach/get/set support for memo files
// — both dBASE III+ DBT and FoxPro FPT — dispatched by the
// table's on-disk version byte via dbf.Table.MemoFormat.
//
// The design is content-focused: MemoGet returns bytes, MemoSet
// takes bytes, and the memo type (relevant only for FPT) is
// managed internally with MemoText as the default. Callers who
// need FPT type awareness — writing MemoPicture or MemoObject
// entries, or reading the type of a Clipper-written FPT memo —
// use AttachedMemo.FPT() to reach the underlying FPTFile
// directly. AttachedMemo.DBT() is the DBT counterpart.
//
// Writes do not update attached indexes or CDX tags. Consistency
// with rewritten memo pointers is the caller's responsibility.
// This mirrors what Area.Replace already does for non-memo
// fields.
package blipperdb

import (
	"fmt"
	"io"

	"github.com/ha1tch/blipper/dbf"
)

// AttachedMemo is a memo file attached to an Area. Exactly one of
// dbt or fpt is populated, matching the table's on-disk version
// byte (0x83 → DBT, 0xF5 → FPT).
type AttachedMemo struct {
	format dbf.MemoFormat
	dbt    *dbf.MemoFile
	fpt    *dbf.FPTFile

	// src is retained so Area.close can release the underlying
	// handle. The dbf memo types keep only what they parsed,
	// so without this the file descriptor would leak for the
	// lifetime of the process.
	src io.ReadWriteSeeker
}

// Format returns the memo format this attachment holds.
func (m *AttachedMemo) Format() dbf.MemoFormat { return m.format }

// DBT returns the underlying MemoFile when the attachment is a
// .DBT, or nil otherwise. Provided for callers who need the raw
// dBASE III+ memo API rather than the content-only Area methods.
func (m *AttachedMemo) DBT() *dbf.MemoFile { return m.dbt }

// FPT returns the underlying FPTFile when the attachment is a
// .FPT, or nil otherwise. Provided for callers who need FPT type
// awareness (MemoPicture, MemoObject) or the raw FoxPro memo API.
func (m *AttachedMemo) FPT() *dbf.FPTFile { return m.fpt }

// AttachMemo opens the sibling memo file at rw and attaches it to
// the area. Dispatches on the table's MemoFormat: 0x83-flavour
// tables get a DBT reader; 0xF5-flavour tables get an FPT reader.
// Errors if the table has no memo field (MemoFormatNone), or if
// a memo is already attached.
//
// The reader stays owned by the caller.
func (a *Area) AttachMemo(rw io.ReadWriteSeeker) (*AttachedMemo, error) {
	if a.memo != nil {
		return nil, fmt.Errorf("blipperdb: memo already attached")
	}
	format := a.table.MemoFormat()
	switch format {
	case dbf.MemoFormatNone:
		return nil, fmt.Errorf("blipperdb: table has no memo field (MemoFormat=%s)", format)
	case dbf.MemoFormatDBT:
		mf, err := dbf.OpenMemo(rw)
		if err != nil {
			return nil, fmt.Errorf("blipperdb: open .DBT: %w", err)
		}
		a.memo = &AttachedMemo{format: format, dbt: mf, src: rw}
	case dbf.MemoFormatFPT:
		fp, err := dbf.OpenFPT(rw)
		if err != nil {
			return nil, fmt.Errorf("blipperdb: open .FPT: %w", err)
		}
		a.memo = &AttachedMemo{format: format, fpt: fp, src: rw}
	default:
		return nil, fmt.Errorf("blipperdb: unknown memo format %s", format)
	}
	return a.memo, nil
}

// CreateMemo creates a fresh sibling memo file at rw, attaching
// it to the area. Dispatches on the table's MemoFormat. blockSize
// is honoured only for FPT (DBT has a fixed 512-byte block size);
// passing 0 accepts the format's default.
func (a *Area) CreateMemo(rw io.ReadWriteSeeker, blockSize uint16) (*AttachedMemo, error) {
	if a.memo != nil {
		return nil, fmt.Errorf("blipperdb: memo already attached")
	}
	format := a.table.MemoFormat()
	switch format {
	case dbf.MemoFormatNone:
		return nil, fmt.Errorf("blipperdb: table has no memo field (MemoFormat=%s)", format)
	case dbf.MemoFormatDBT:
		mf, err := dbf.CreateMemo(rw)
		if err != nil {
			return nil, fmt.Errorf("blipperdb: create .DBT: %w", err)
		}
		a.memo = &AttachedMemo{format: format, dbt: mf, src: rw}
	case dbf.MemoFormatFPT:
		fp, err := dbf.CreateFPT(rw, blockSize)
		if err != nil {
			return nil, fmt.Errorf("blipperdb: create .FPT: %w", err)
		}
		a.memo = &AttachedMemo{format: format, fpt: fp, src: rw}
	default:
		return nil, fmt.Errorf("blipperdb: unknown memo format %s", format)
	}
	return a.memo, nil
}

// Memo returns the attached memo, or nil if none.
func (a *Area) Memo() *AttachedMemo { return a.memo }

// MemoGet reads the memo referenced by the named field in the
// current record. Returns empty content and no error when the
// memo pointer is absent (all-spaces field). Errors if no memo
// is attached, if the current record cannot be read, if the
// field is missing or has an invalid pointer, or if the referenced
// block cannot be read.
//
// For FPT memos this returns content only; the memo type is
// discarded. Callers who need the type read the entry via
// AttachedMemo.FPT().Get(block).
func (a *Area) MemoGet(field string) ([]byte, error) {
	if a.memo == nil {
		return nil, fmt.Errorf("blipperdb: no memo attached")
	}
	rec, err := a.Record()
	if err != nil {
		return nil, fmt.Errorf("blipperdb: current record: %w", err)
	}
	// The memo pointer is stored in the field as a 10-byte
	// right-aligned ASCII string. Read the raw string value and
	// parse it.
	raw, err := rec.Get(a.table.Schema(), field)
	if err != nil {
		return nil, fmt.Errorf("blipperdb: read field %q: %w", field, err)
	}
	ptrStr, ok := raw.(string)
	if !ok {
		return nil, fmt.Errorf("blipperdb: field %q is not a memo pointer (type %T)", field, raw)
	}
	block, present, err := dbf.ParseMemoPointer([]byte(ptrStr))
	if err != nil {
		return nil, fmt.Errorf("blipperdb: parse memo pointer for %q: %w", field, err)
	}
	if !present {
		return []byte{}, nil
	}
	switch a.memo.format {
	case dbf.MemoFormatDBT:
		return a.memo.dbt.Get(block)
	case dbf.MemoFormatFPT:
		content, _, err := a.memo.fpt.Get(block)
		return content, err
	}
	return nil, fmt.Errorf("blipperdb: memo format %s not handled", a.memo.format)
}

// MemoSet appends the given content to the attached memo file
// and writes the resulting block pointer into the named field of
// the current record, rewriting the record on disk.
//
// FPT content is written as MemoText. Callers who need to write
// MemoPicture or MemoObject use AttachedMemo.FPT().Append
// directly, then update the field pointer with the returned
// block via a separate record rewrite.
//
// The previous memo (if any) is not reclaimed — its blocks
// become orphaned. This matches FoxPro's own behaviour and is
// consistent with T-03's PACK-scoped compaction model.
func (a *Area) MemoSet(field string, content []byte) error {
	if a.memo == nil {
		return fmt.Errorf("blipperdb: no memo attached")
	}
	if err := a.checkWritable(a.recno); err != nil {
		return err
	}
	var block uint32
	switch a.memo.format {
	case dbf.MemoFormatDBT:
		b, err := a.memo.dbt.Append(content)
		if err != nil {
			return fmt.Errorf("blipperdb: append .DBT: %w", err)
		}
		block = b
	case dbf.MemoFormatFPT:
		b, err := a.memo.fpt.Append(content, dbf.MemoText)
		if err != nil {
			return fmt.Errorf("blipperdb: append .FPT: %w", err)
		}
		block = b
	default:
		return fmt.Errorf("blipperdb: memo format %s not handled", a.memo.format)
	}
	// Rewrite the record with the new memo pointer.
	rec, err := a.Record()
	if err != nil {
		return fmt.Errorf("blipperdb: current record: %w", err)
	}
	ptr := string(dbf.FormatMemoPointer(block))
	if err := rec.Set(a.table.Schema(), field, ptr); err != nil {
		return fmt.Errorf("blipperdb: set field %q: %w", field, err)
	}
	if err := a.table.Put(a.recno, rec); err != nil {
		return fmt.Errorf("blipperdb: put record %d: %w", a.recno, err)
	}
	return nil
}
