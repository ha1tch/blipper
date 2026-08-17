package dbf

import (
	"fmt"
	"io"
)

// BlockMapping describes how memo blocks moved during a compaction.
//
// It is deliberately the same shape as RecordMapping. That type
// made record renumbering an explicit value every consumer works
// from, which is what made PACK's coordination tractable; memo
// blocks have the same problem — pointers to them live inside
// records that know nothing about the move — so they get the same
// answer rather than a second convention.
//
// Block numbers are as they appear in a memo pointer field: for
// DBT the block index, for FPT the absolute block number counted
// from byte zero. A new number of zero means the block was not
// carried over.
type BlockMapping struct {
	oldToNew map[uint32]uint32

	// Kept counts memo entries copied into the compacted file.
	Kept uint32

	// Dropped counts memo entries left behind because no
	// surviving record pointed at them.
	Dropped uint32

	// BytesBefore and BytesAfter record the memo file's size on
	// either side of the compaction, so callers can report what
	// the operation recovered.
	BytesBefore int64
	BytesAfter  int64
}

// Lookup returns the new block number for an old one, and whether
// the block was carried over.
func (m *BlockMapping) Lookup(old uint32) (uint32, bool) {
	if old == 0 {
		return 0, false
	}
	n, ok := m.oldToNew[old]
	return n, ok
}

// Identity reports whether the compaction moved nothing, which
// lets callers skip rewriting memo pointers.
func (m *BlockMapping) Identity() bool {
	if m.Dropped != 0 {
		return false
	}
	for old, new := range m.oldToNew {
		if old != new {
			return false
		}
	}
	return true
}

// memoPointers walks a table collecting the memo block each record
// references, per memo field.
//
// Liveness cannot be refcounted: a memo carries no back-pointer to
// the record that owns it, so the only sound way to learn which
// blocks are live is to read every record. That is one full scan,
// which is acceptable inside an operation that already rewrites a
// whole file.
func memoPointers(t *Table) (map[uint32]bool, error) {
	live := map[uint32]bool{}
	memoFields := make([]string, 0, 2)
	for _, f := range t.schema.Fields {
		if f.Type == Memo {
			memoFields = append(memoFields, f.Name)
		}
	}
	if len(memoFields) == 0 {
		return live, nil
	}
	for recno := uint32(1); recno <= t.recordCount; recno++ {
		rec, err := t.Get(recno)
		if err != nil {
			return nil, fmt.Errorf("scanning record %d for memo pointers: %w", recno, err)
		}
		for _, name := range memoFields {
			raw, err := rec.Get(t.schema, name)
			if err != nil {
				return nil, err
			}
			s, ok := raw.(string)
			if !ok {
				continue
			}
			block, present, err := ParseMemoPointer([]byte(s))
			if err != nil || !present {
				continue
			}
			live[block] = true
		}
	}
	return live, nil
}

// CompactMemo rewrites a table's memo file, keeping only the
// entries some surviving record points at, and returns the mapping
// describing where those entries moved.
//
// Two kinds of waste accumulate in a memo file. Records removed by
// Pack leave their memos unreachable, and every MemoSet appends a
// fresh entry and repoints the record, orphaning the old one —
// FoxPro behaves the same way, so the second is expected rather
// than a defect, but over time it is the larger source.
//
// Ordering is load-bearing: this must run *after* the table has
// been packed. Which memos are live is decided by which records
// survive, so compacting first would preserve entries belonging to
// records about to be removed.
//
// The caller is responsible for rewriting memo pointers in the
// table using the returned mapping; CompactMemo touches only the
// memo file. blipperdb.Area.PackAll does both.
func CompactMemo(t *Table, src, dst io.ReadWriteSeeker) (*BlockMapping, error) {
	switch t.MemoFormat() {
	case MemoFormatDBT:
		return compactDBT(t, src, dst)
	case MemoFormatFPT:
		return compactFPT(t, src, dst)
	default:
		return nil, fmt.Errorf("table has no memo file (MemoFormat=%s)", t.MemoFormat())
	}
}

func compactDBT(t *Table, src, dst io.ReadWriteSeeker) (*BlockMapping, error) {
	oldFile, err := OpenMemo(src)
	if err != nil {
		return nil, fmt.Errorf("opening source .DBT: %w", err)
	}
	live, err := memoPointers(t)
	if err != nil {
		return nil, err
	}
	newFile, err := CreateMemo(dst)
	if err != nil {
		return nil, fmt.Errorf("creating destination .DBT: %w", err)
	}

	m := &BlockMapping{oldToNew: map[uint32]uint32{}}
	m.BytesBefore = streamSize(src)

	// Copy in ascending block order so the compacted file is
	// deterministic rather than dependent on record order.
	for _, old := range sortedBlocks(live) {
		content, err := oldFile.Get(old)
		if err != nil {
			// A pointer into a corrupt or truncated memo file is
			// reported rather than silently dropped: losing memo
			// content quietly is worse than failing the compaction.
			return nil, fmt.Errorf("reading memo block %d: %w", old, err)
		}
		newBlock, err := newFile.Append(content)
		if err != nil {
			return nil, fmt.Errorf("writing memo block %d: %w", old, err)
		}
		m.oldToNew[old] = newBlock
		m.Kept++
	}
	m.Dropped = uint32(oldFile.NextFree()) - 1 - m.Kept
	m.BytesAfter = streamSize(dst)
	return m, nil
}

func compactFPT(t *Table, src, dst io.ReadWriteSeeker) (*BlockMapping, error) {
	oldFile, err := OpenFPT(src)
	if err != nil {
		return nil, fmt.Errorf("opening source .FPT: %w", err)
	}
	live, err := memoPointers(t)
	if err != nil {
		return nil, err
	}
	// The compacted file keeps the source's block size, so a
	// caller's configured geometry survives the operation.
	newFile, err := CreateFPT(dst, oldFile.BlockSize())
	if err != nil {
		return nil, fmt.Errorf("creating destination .FPT: %w", err)
	}

	m := &BlockMapping{oldToNew: map[uint32]uint32{}}
	m.BytesBefore = streamSize(src)

	for _, old := range sortedBlocks(live) {
		content, memoType, err := oldFile.Get(old)
		if err != nil {
			return nil, fmt.Errorf("reading memo block %d: %w", old, err)
		}
		newBlock, err := newFile.Append(content, memoType)
		if err != nil {
			return nil, fmt.Errorf("writing memo block %d: %w", old, err)
		}
		m.oldToNew[old] = newBlock
		m.Kept++
	}
	m.BytesAfter = streamSize(dst)
	return m, nil
}

// RewriteMemoPointers updates every record's memo fields to the
// block numbers a compaction assigned.
//
// Records whose memo block was dropped have their pointer cleared
// to the absent form, which is ten spaces. That case should not
// arise when the mapping came from a scan of this same table, and
// is handled rather than assumed away.
func RewriteMemoPointers(t *Table, m *BlockMapping) error {
	if m.Identity() {
		return nil
	}
	memoFields := make([]string, 0, 2)
	for _, f := range t.schema.Fields {
		if f.Type == Memo {
			memoFields = append(memoFields, f.Name)
		}
	}
	if len(memoFields) == 0 {
		return nil
	}

	for recno := uint32(1); recno <= t.recordCount; recno++ {
		rec, err := t.Get(recno)
		if err != nil {
			return fmt.Errorf("reading record %d: %w", recno, err)
		}
		changed := false
		for _, name := range memoFields {
			raw, err := rec.Get(t.schema, name)
			if err != nil {
				return err
			}
			s, ok := raw.(string)
			if !ok {
				continue
			}
			old, present, err := ParseMemoPointer([]byte(s))
			if err != nil || !present {
				continue
			}
			newBlock, kept := m.Lookup(old)
			if !kept {
				if err := rec.Set(t.schema, name, "          "); err != nil {
					return err
				}
				changed = true
				continue
			}
			if newBlock != old {
				if err := rec.Set(t.schema, name, string(FormatMemoPointer(newBlock))); err != nil {
					return err
				}
				changed = true
			}
		}
		if changed {
			if err := t.Put(recno, rec); err != nil {
				return fmt.Errorf("rewriting record %d: %w", recno, err)
			}
		}
	}
	return nil
}

// sortedBlocks returns the keys of a block set in ascending order,
// so a compaction produces the same layout on every run.
func sortedBlocks(set map[uint32]bool) []uint32 {
	out := make([]uint32, 0, len(set))
	for b := range set {
		out = append(out, b)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// streamSize reports a stream's length, or zero when it cannot be
// determined. Used only for the informational byte counts on a
// BlockMapping, so a failure here is not worth propagating.
func streamSize(s io.Seeker) int64 {
	cur, err := s.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0
	}
	end, err := s.Seek(0, io.SeekEnd)
	if err != nil {
		return 0
	}
	if _, err := s.Seek(cur, io.SeekStart); err != nil {
		return 0
	}
	return end
}
