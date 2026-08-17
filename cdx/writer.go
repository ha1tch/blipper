package cdx

import (
	"encoding/binary"
	"fmt"
	"io"
	"sort"
	"strings"
)

// TagSpec describes one tag to be written to a new CDX.
//
// Entries must be pre-sorted ascending by Key. Build does not
// re-sort; callers pass them in the order they should appear on
// disk. Keys shorter than KeyLen are right-padded with spaces on
// write; longer keys are rejected with an error.
type TagSpec struct {
	Name    string  // up to 10 characters, uppercased on write
	KeyExpr string  // uncompiled expression text, e.g. "CODE"
	KeyLen  uint16  // fixed key width in bytes
	Entries []Entry // ascending by Key
}

// buildLayout describes where each piece of the CDX lives on disk.
// A CDX for N tags takes 3 blocks of overhead (file-header
// descriptor + file-header pool + tag-directory root) plus 3
// blocks per tag (tag-header descriptor + tag-header pool +
// per-tag leaf).
type buildLayout struct {
	tagDirRoot    int64
	tagHeaderPtrs []int64 // one per tag, in write order (block offset of the descriptor)
	tagLeafPtrs   []int64 // one per tag: block offset of the tag's leaf node
	totalBlocks   int64
}

// planLayout assigns block offsets to every structure and returns
// the total file size in blocks. Blocks are allocated in the
// same order Clipper's DBFCDX writes them, which keeps the layout
// diff-comparable with reference files:
//
//	block 0: file header descriptor
//	block 1: file header pool
//	block 2: tag directory root
//	block 3: tag[0] header descriptor
//	block 4: tag[0] header pool
//	block 5: tag[0] leaf
//	block 6: tag[1] header descriptor
//	... etc.
func planLayout(tags []TagSpec) buildLayout {
	l := buildLayout{tagDirRoot: 2 * BlockSize}
	next := int64(3) // next unused block index after file header + tag dir
	for range tags {
		l.tagHeaderPtrs = append(l.tagHeaderPtrs, next*BlockSize)
		l.tagLeafPtrs = append(l.tagLeafPtrs, (next+2)*BlockSize)
		next += 3
	}
	l.totalBlocks = next
	return l
}

// Build writes a complete FoxPro 2-compatible CDX file to w.
//
// The output uses MACHINE collation (the only one this package
// supports, both for reads and writes), ascending order, no FOR
// clause on any tag, and fixed encoding parameters chosen to
// match how Clipper 5.2e's DBFCDX.LIB lays out small files:
// 16-bit record numbers, 4-bit duplicate/trail counts, 3 bytes
// per compressed entry.
//
// Every tag is written as a single leaf node. Tags whose entries
// do not fit in one 512-byte page after compression are rejected
// with an explanatory error rather than silently split. Tree
// splitting is a natural extension (a Writer.Split method that
// promotes overfull leaves to a two-level tree) but is not part
// of Phase 1.
func Build(w io.Writer, tags []TagSpec) error {
	if len(tags) == 0 {
		return fmt.Errorf("cdx: at least one tag required")
	}
	// Normalise: uppercase tag names, sort tags for the tag
	// directory (MACHINE = byte order, ascending).
	sorted := make([]TagSpec, len(tags))
	copy(sorted, tags)
	for i := range sorted {
		sorted[i].Name = strings.ToUpper(strings.TrimSpace(sorted[i].Name))
		if sorted[i].Name == "" || len(sorted[i].Name) > 10 {
			return fmt.Errorf("cdx: tag name %q invalid (empty or > 10 chars)", sorted[i].Name)
		}
		if sorted[i].KeyLen == 0 || sorted[i].KeyLen > 240 {
			return fmt.Errorf("cdx: tag %q has invalid key length %d", sorted[i].Name, sorted[i].KeyLen)
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	layout := planLayout(sorted)
	out := make([]byte, layout.totalBlocks*BlockSize)

	// Block 0: file header. Root of the tag directory sits at
	// offset 2*BlockSize.
	writeCompoundHeader(out[0:BlockSize], layout.tagDirRoot, 0, "")
	// Block 1: file header's expression pool. Empty for the
	// tag directory (which has no user-visible key expression),
	// but the length words at 510 and 506 read as 1 in
	// DBFCDX output, meaning "one NUL byte", so we mirror that.
	writeEmptyPool(out[BlockSize : 2*BlockSize])
	patchPoolLengths(out[0:BlockSize], 1, 1)

	// Block 2: tag directory root. It is an exterior/leaf node
	// with keyLen = 10, one entry per tag, recNo = tag header
	// block offset.
	tagDirEntries := make([]Entry, len(sorted))
	for i, t := range sorted {
		key := padKey([]byte(t.Name), 10)
		tagDirEntries[i] = Entry{Key: key, RecNo: uint32(layout.tagHeaderPtrs[i])}
	}
	if err := writeLeaf(out[layout.tagDirRoot:layout.tagDirRoot+BlockSize],
		tagDirEntries, 10); err != nil {
		return fmt.Errorf("cdx: tag directory: %w", err)
	}

	// Blocks 3..: per-tag descriptor, pool, leaf.
	for i, t := range sorted {
		hdrOff := layout.tagHeaderPtrs[i]
		poolOff := hdrOff + BlockSize
		leafOff := layout.tagLeafPtrs[i]

		writeCompoundHeader(out[hdrOff:hdrOff+BlockSize], leafOff, t.KeyLen, t.KeyExpr)
		writePoolWithKeyExpr(out[poolOff:poolOff+BlockSize], t.KeyExpr)
		patchPoolLengths(out[hdrOff:hdrOff+BlockSize], uint16(len(t.KeyExpr)+1), 1)

		normalised, err := normaliseEntries(t.Entries, t.KeyLen)
		if err != nil {
			return fmt.Errorf("cdx: tag %q: %w", t.Name, err)
		}
		if err := writeLeaf(out[leafOff:leafOff+BlockSize], normalised, t.KeyLen); err != nil {
			return fmt.Errorf("cdx: tag %q leaf: %w", t.Name, err)
		}
	}

	_, err := w.Write(out)
	return err
}

// writeCompoundHeader lays down the fixed portions of a 512-byte
// header block: root pointer, free list = -1, key length, options
// (compact + compound-header), signature = 0, ascending flag.
// keyExpr is only used to satisfy the ascending flag placement;
// the actual pool content goes in the block that follows via
// writeEmptyPool or writePoolWithKeyExpr.
func writeCompoundHeader(block []byte, rootPtr int64, keyLen uint16, keyExpr string) {
	// bytes 0-3: root pointer
	binary.LittleEndian.PutUint32(block[0:4], uint32(rootPtr))
	// bytes 4-7: free-node list, -1 when unused
	binary.LittleEndian.PutUint32(block[4:8], 0xFFFFFFFF)
	// bytes 8-11: "reserved for internal use" per aa975346;
	// leave zero.
	// bytes 12-13: key length
	binary.LittleEndian.PutUint16(block[12:14], keyLen)
	// byte 14: options — compact + compound-header
	block[14] = optCompact | optCompoundHdr
	// byte 15: signature — 0 (MACHINE collation)
	block[15] = 0
	// bytes 502-503: ascending
	binary.LittleEndian.PutUint16(block[502:504], orderAscending)
	// Pool length fields at 506-507 (FOR) and 510-511 (key) are
	// patched by patchPoolLengths after the caller writes the
	// pool block, so that the length matches what was actually
	// written.
}

// patchPoolLengths sets the FOR and key-expression pool length
// fields in a header block.
func patchPoolLengths(block []byte, keyLen, forLen uint16) {
	binary.LittleEndian.PutUint16(block[506:508], forLen)
	binary.LittleEndian.PutUint16(block[510:512], keyLen)
}

// writeEmptyPool writes a 512-byte pool block containing a single
// NUL followed by zeros. Clipper's tag-directory pool has this
// shape; we mirror it for byte-similarity with reference files.
func writeEmptyPool(block []byte) {
	for i := range block {
		block[i] = 0
	}
}

// writePoolWithKeyExpr writes a pool block whose leading bytes
// carry the key expression as ASCII, NUL-terminated. Any remainder
// is zeroed.
func writePoolWithKeyExpr(block []byte, keyExpr string) {
	for i := range block {
		block[i] = 0
	}
	copy(block, []byte(keyExpr))
	// NUL after the expression is already in place (zeroed).
}

// normaliseEntries validates and right-pads each entry's key to
// keyLen, returning a fresh slice that Build can consume without
// mutating the caller's data.
func normaliseEntries(entries []Entry, keyLen uint16) ([]Entry, error) {
	out := make([]Entry, len(entries))
	for i, e := range entries {
		if len(e.Key) > int(keyLen) {
			return nil, fmt.Errorf("entry %d key length %d exceeds tag key length %d",
				i, len(e.Key), keyLen)
		}
		out[i] = Entry{
			Key:   padKey(e.Key, int(keyLen)),
			RecNo: e.RecNo,
		}
		if i > 0 {
			// Guard the sortedness precondition rather than
			// silently produce a malformed index.
			if compareKeys(out[i-1].Key, out[i].Key) > 0 {
				return nil, fmt.Errorf("entry %d key not >= previous", i)
			}
		}
	}
	return out, nil
}

// compareKeys is a small wrapper so we can pass it around without
// pulling in bytes.
func compareKeys(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	} else if len(a) > len(b) {
		return 1
	}
	return 0
}
