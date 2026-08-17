// Package blipperdb: PACK coordination.
//
// Physically removing deleted records renumbers every record that
// survives, and record numbers are a shared namespace: indexes
// hold them, and nothing propagates a change. dbf.Table.Pack
// returns a RecordMapping describing the move; this file applies
// that mapping to everything attached to the Area.
package blipperdb

import (
	"fmt"
	"io"

	"github.com/ha1tch/blipper/cdx"
	"github.com/ha1tch/blipper/dbf"
)

// Compactable is implemented by attachments holding state derived
// from record numbers, which must therefore be rebuilt when Pack
// renumbers them.
//
// The interface describes a property some attachments have rather
// than a duty all of them owe. AttachedCatalogue does not
// implement it — long field names have nothing to do with record
// numbers — and forcing a no-op Rebuild into existence would only
// leave a reader wondering what it was for. This follows the
// Flusher precedent, where OSDir correctly does not implement a
// capability it lacks.
//
// Rebuild receives the mapping and returns an error if the
// attachment cannot be brought back into agreement with the
// table.
type Compactable interface {
	Rebuild(mapping *dbf.RecordMapping) error
}

// Pack physically removes deleted records from the area's table
// and rebuilds every attachment that depends on record numbers.
//
// The record pointer is reset to the top of the table, since the
// record it referred to may no longer exist or may have moved.
//
// Attachments are rebuilt after the table is packed, not before:
// a rebuild reads the packed table, so the order is load-bearing.
// If an attachment fails to rebuild, Pack returns the error with
// the table already packed — the attachment is then stale and
// must be rebuilt or discarded, which is reported rather than
// hidden.
func (a *Area) Pack() (*dbf.RecordMapping, error) {
	// Pack rewrites every record and the header, so it needs a
	// lock over the file, not a record.
	if err := a.checkWritable(0); err != nil {
		return nil, err
	}
	mapping, err := a.table.Pack()
	if err != nil {
		return nil, err
	}

	// An identity pack moved nothing, so every attachment is
	// still correct. Skipping the rebuild here is what makes
	// defensive packing cheap.
	if mapping.Identity() {
		if err := a.GoTop(); err != nil {
			return mapping, err
		}
		return mapping, nil
	}

	for _, att := range a.compactables() {
		if err := att.Rebuild(mapping); err != nil {
			return mapping, fmt.Errorf("blipperdb: rebuilding after pack: %w", err)
		}
	}

	if err := a.GoTop(); err != nil {
		return mapping, err
	}
	return mapping, nil
}

// compactables returns every attachment implementing Compactable.
// Written as a type assertion over the concrete attachments
// rather than a registry, because the set is small, fixed, and
// known at compile time.
func (a *Area) compactables() []Compactable {
	var out []Compactable
	for _, idx := range a.indexes {
		out = append(out, &ntxCompactable{area: a, attached: idx})
	}
	if a.cdx != nil {
		out = append(out, a.cdx)
	}
	return out
}

// --- NTX ---

// ntxCompactable rebuilds one attached NTX index from the packed
// table.
//
// An NTX holds keys paired with record numbers. Remapping in
// place would mean rewriting every leaf, and the tree shape
// depends on insertion order, so the honest approach is to
// rebuild from the surviving records: read each one, derive its
// key with the index's own key function, and insert against the
// new number.
type ntxCompactable struct {
	area     *Area
	attached *attachedIndex
}

func (n *ntxCompactable) Rebuild(mapping *dbf.RecordMapping) error {
	if n.attached.keyFn == nil {
		return fmt.Errorf("index has no key function; cannot rebuild after pack")
	}
	idx := n.attached.index

	// Collect the old (key, recno) pairs before touching
	// anything. The keys are derived from the table as it was
	// numbered before the pack, so they are recomputed from the
	// mapping's old numbering rather than read back from the
	// index — which is already inconsistent with the table.
	type entry struct {
		key   []byte
		oldNo uint32
		newNo uint32
	}
	var moves []entry
	for oldNo := uint32(1); oldNo <= mapping.OldCount(); oldNo++ {
		newNo, survives := mapping.Lookup(oldNo)
		if !survives {
			// A removed record's key is unknown post-pack, since
			// its row is gone. Its index entry is found and
			// dropped below by scanning, not by key lookup.
			continue
		}
		if oldNo == newNo {
			continue // nothing to change
		}
		rec, err := n.area.table.Get(newNo)
		if err != nil {
			return fmt.Errorf("reading record %d: %w", newNo, err)
		}
		moves = append(moves, entry{key: n.attached.keyFn(rec), oldNo: oldNo, newNo: newNo})
	}

	// Drop every entry whose record no longer exists. Walking the
	// index and testing each record number against the mapping is
	// the only way to find them: their keys cannot be recomputed
	// from a table row that has been removed.
	cur := idx.NewCursor()
	cur.First()
	var stale []ntxEntry
	for cur.Next() {
		e := cur.Entry()
		if _, survives := mapping.Lookup(e.Recno); !survives {
			key := make([]byte, len(e.Key))
			copy(key, e.Key)
			stale = append(stale, ntxEntry{key: key, recNo: e.Recno})
		}
	}
	if err := cur.Err(); err != nil {
		return fmt.Errorf("scanning index: %w", err)
	}
	for _, e := range stale {
		if _, err := idx.Delete(e.key, e.recNo); err != nil {
			return fmt.Errorf("dropping stale index entry for record %d: %w", e.recNo, err)
		}
	}

	// Renumber the survivors that moved: delete at the old
	// number, insert at the new one.
	for _, m := range moves {
		if _, err := idx.Delete(m.key, m.oldNo); err != nil {
			return fmt.Errorf("removing old index entry for record %d: %w", m.oldNo, err)
		}
	}
	for _, m := range moves {
		if _, err := idx.Insert(m.key, m.newNo); err != nil {
			return fmt.Errorf("inserting index entry for record %d: %w", m.newNo, err)
		}
	}
	return nil
}

// ntxEntry is a key/recno pair captured while scanning an index.
type ntxEntry struct {
	key   []byte
	recNo uint32
}

// --- CDX ---

// Rebuild reindexes every tag in the attached CDX against the
// packed table.
//
// Unlike NTX, a CDX tag carries its key expression as text rather
// than a Go function, and blipper has no expression evaluator.
// The tag's entries are therefore remapped rather than
// recomputed: each surviving entry keeps its key and takes its
// record's new number, and entries whose records were removed are
// dropped. This is exact, because packing changes which records
// exist and what they are numbered, never their field values.
func (c *AttachedCDX) Rebuild(mapping *dbf.RecordMapping) error {
	tagNames := c.file.TagNames()
	specs := make([]cdx.TagSpec, 0, len(tagNames))

	for _, name := range tagNames {
		tag, err := c.file.Tag(name)
		if err != nil {
			return fmt.Errorf("resolving tag %q: %w", name, err)
		}
		spec := cdx.TagSpec{
			Name:    name,
			KeyExpr: tag.KeyExpr(),
		}
		err = c.file.Traverse(tag, func(e cdx.Entry) error {
			newNo, survives := mapping.Lookup(e.RecNo)
			if !survives {
				return nil
			}
			if spec.KeyLen == 0 {
				spec.KeyLen = uint16(len(e.Key))
			}
			key := make([]byte, len(e.Key))
			copy(key, e.Key)
			spec.Entries = append(spec.Entries, cdx.Entry{Key: key, RecNo: newNo})
			return nil
		})
		if err != nil {
			return fmt.Errorf("traversing tag %q: %w", name, err)
		}
		if spec.KeyLen == 0 {
			// An empty tag has no entry to take a width from;
			// fall back to the tag's declared key length.
			spec.KeyLen = tag.KeyLen()
		}
		specs = append(specs, spec)
	}

	if c.rw == nil {
		return fmt.Errorf("CDX was attached read-only; cannot rebuild after pack")
	}
	if _, err := c.rw.Seek(0, 0); err != nil {
		return fmt.Errorf("seeking CDX for rebuild: %w", err)
	}
	if err := cdx.Build(c.rw, specs); err != nil {
		return fmt.Errorf("rebuilding CDX: %w", err)
	}
	if _, err := c.rw.Seek(0, 0); err != nil {
		return err
	}
	reopened, err := cdx.Open(c.rw)
	if err != nil {
		return fmt.Errorf("reopening rebuilt CDX: %w", err)
	}
	c.file = reopened
	return nil
}

// PackAll packs the table and then compacts its memo file,
// rewriting the surviving records' memo pointers.
//
// dst receives the compacted memo file; it must be a distinct
// stream from the one currently attached, because compaction
// copies live entries into a fresh file rather than shuffling
// them in place. On success the area's memo attachment is
// replaced by the compacted file, and the caller is responsible
// for putting dst where src used to live.
//
// Ordering is load-bearing and is the reason this is a separate
// method rather than an option on Pack: which memos are live is
// decided by which records survive, so the table must be packed
// first. Compacting first would carefully preserve memos
// belonging to records about to be dropped.
//
// Pack alone remains the default. Memo compaction rewrites a
// second file, and a caller who never rewrites memos gains
// nothing from it — orphans accumulate through MemoSet, not
// through ordinary use.
func (a *Area) PackAll(dst io.ReadWriteSeeker) (*dbf.RecordMapping, *dbf.BlockMapping, error) {
	if a.memo == nil {
		return nil, nil, fmt.Errorf("blipperdb: no memo attached; use Pack")
	}
	src := a.memo.src
	if src == nil {
		return nil, nil, fmt.Errorf("blipperdb: memo has no underlying stream; cannot compact")
	}

	recMap, err := a.Pack()
	if err != nil {
		return recMap, nil, err
	}

	blockMap, err := dbf.CompactMemo(a.table, src, dst)
	if err != nil {
		return recMap, nil, fmt.Errorf("blipperdb: compacting memo: %w", err)
	}
	if err := dbf.RewriteMemoPointers(a.table, blockMap); err != nil {
		return recMap, blockMap, fmt.Errorf("blipperdb: rewriting memo pointers: %w", err)
	}

	// Re-attach against the compacted file, so subsequent MemoGet
	// calls resolve the new block numbers rather than the old.
	a.memo = nil
	if _, err := a.AttachMemo(dst); err != nil {
		return recMap, blockMap, fmt.Errorf("blipperdb: reattaching compacted memo: %w", err)
	}
	return recMap, blockMap, nil
}
