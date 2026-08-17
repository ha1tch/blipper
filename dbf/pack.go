package dbf

import (
	"fmt"
	"io"
)

// RecordMapping describes how record numbers moved during a Pack.
//
// PACK's real difficulty is not removing rows; it is that record
// numbers are a shared namespace across four file formats with no
// coordination mechanism between them. An index holds record
// numbers, a memo pointer lives inside a record, and neither knows
// the DBF renumbered. The mapping is the coordination that was
// missing, made explicit so every consumer works from the same
// account of what changed.
//
// Old record numbers are one-based, matching Get and Put. A new
// number of zero means the record was deleted and has no successor.
type RecordMapping struct {
	// oldToNew[i] is the new number of old record i+1, or 0 when
	// that record was removed.
	oldToNew []uint32

	// Removed counts records dropped by the pack.
	Removed uint32

	// Kept counts records that survived.
	Kept uint32
}

// Lookup returns the new number for an old record number, and
// whether the record survived. Out-of-range inputs report false
// rather than erroring: callers walking an index built before the
// pack may legitimately hold stale numbers.
func (m *RecordMapping) Lookup(old uint32) (uint32, bool) {
	if old == 0 || int(old) > len(m.oldToNew) {
		return 0, false
	}
	n := m.oldToNew[old-1]
	return n, n != 0
}

// OldCount returns how many records existed before the pack.
func (m *RecordMapping) OldCount() uint32 { return uint32(len(m.oldToNew)) }

// Identity reports whether the pack changed nothing, which lets
// callers skip an expensive index rebuild.
func (m *RecordMapping) Identity() bool { return m.Removed == 0 }

// Pack physically removes records marked deleted, renumbering
// those that remain, and returns the mapping describing the move.
//
// Pack rewrites the table in place and truncates it if the
// underlying stream supports truncation. Callers holding indexes,
// memo attachments, or anything else keyed on record numbers must
// apply the returned mapping; Pack itself touches only the DBF.
// blipperdb.Area.Pack coordinates the wider rebuild.
//
// The record pointer of any open Cursor is invalidated.
func (t *Table) Pack() (*RecordMapping, error) {
	mapping := &RecordMapping{
		oldToNew: make([]uint32, t.recordCount),
	}

	// First pass: decide what survives, without writing anything.
	// Doing this before any mutation means a read error leaves the
	// table untouched rather than half-packed.
	survivors := make([]uint32, 0, t.recordCount)
	for old := uint32(1); old <= t.recordCount; old++ {
		deleted, err := t.isDeleted(old)
		if err != nil {
			return nil, fmt.Errorf("pack: reading deletion flag of record %d: %w", old, err)
		}
		if deleted {
			mapping.Removed++
			continue
		}
		survivors = append(survivors, old)
		mapping.Kept++
		mapping.oldToNew[old-1] = mapping.Kept
	}

	if mapping.Identity() {
		// Nothing to do. Returning early avoids rewriting a table
		// that has no deleted records, which is the common case
		// for a caller packing defensively.
		return mapping, nil
	}

	// Second pass: compact the surviving records forward. Records
	// only ever move toward lower offsets, so a forward copy never
	// overwrites a record it has yet to read.
	recSize := int64(t.schema.RecordSize())
	buf := make([]byte, recSize)
	for newIdx, old := range survivors {
		newNo := uint32(newIdx + 1)
		if old == newNo {
			continue // already in place
		}
		if _, err := t.rw.Seek(t.recordOffset(old), io.SeekStart); err != nil {
			return nil, fmt.Errorf("pack: seek to old record %d: %w", old, err)
		}
		if _, err := io.ReadFull(t.rw, buf); err != nil {
			return nil, fmt.Errorf("pack: read old record %d: %w", old, err)
		}
		if _, err := t.rw.Seek(t.recordOffset(newNo), io.SeekStart); err != nil {
			return nil, fmt.Errorf("pack: seek to new record %d: %w", newNo, err)
		}
		if _, err := t.rw.Write(buf); err != nil {
			return nil, fmt.Errorf("pack: write new record %d: %w", newNo, err)
		}
	}

	t.recordCount = mapping.Kept

	// Terminate the file after the last surviving record and, when
	// the stream allows it, release the tail. A stream without
	// Truncate keeps its old length with the terminator marking
	// the real end, which every xBase reader honours.
	endOffset := t.recordOffset(mapping.Kept + 1)
	if _, err := t.rw.Seek(endOffset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("pack: seek to end of data: %w", err)
	}
	if _, err := t.rw.Write([]byte{fileTerminator}); err != nil {
		return nil, fmt.Errorf("pack: write EOF marker: %w", err)
	}
	if tr, ok := t.rw.(interface{ Truncate(int64) error }); ok {
		if err := tr.Truncate(endOffset + 1); err != nil {
			return nil, fmt.Errorf("pack: truncate: %w", err)
		}
	}

	if err := t.flushHeader(); err != nil {
		return nil, fmt.Errorf("pack: rewrite header: %w", err)
	}
	return mapping, nil
}

// isDeleted reports whether a record carries the deletion marker.
func (t *Table) isDeleted(recno uint32) (bool, error) {
	if err := t.checkRecno(recno); err != nil {
		return false, err
	}
	if _, err := t.rw.Seek(t.recordOffset(recno), io.SeekStart); err != nil {
		return false, err
	}
	var marker [1]byte
	if _, err := io.ReadFull(t.rw, marker[:]); err != nil {
		return false, err
	}
	return marker[0] == deletedMarker, nil
}
