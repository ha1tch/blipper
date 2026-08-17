package ndx

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// page is a decoded index page: a count of live entries followed
// by an array of fixed-size key records.
//
// Every record carries a lower-level page pointer, so an interior
// node and a leaf differ only in whether those pointers are zero.
// There is no node-type byte in the format; a page is a leaf
// because its entries point nowhere, which is worth stating since
// several sibling formats do carry an explicit attribute.
type page struct {
	number  uint32
	entries []pageEntry
}

// pageEntry is one key record.
type pageEntry struct {
	lower uint32 // page number of the subtree below this key, 0 if none
	recno uint32 // record number in the DBF
	key   []byte // fixed-width key bytes
}

// readPage decodes one page.
func (ix *Index) readPage(number uint32) (*page, error) {
	if number == 0 {
		return nil, fmt.Errorf("%w: page 0 is the header, not an index page", ErrCorrupt)
	}
	if _, err := ix.rw.Seek(pageOffset(number), io.SeekStart); err != nil {
		return nil, fmt.Errorf("ndx: seek to page %d: %w", number, err)
	}
	var raw [PageSize]byte
	if _, err := io.ReadFull(ix.rw, raw[:]); err != nil {
		return nil, fmt.Errorf("ndx: read page %d: %w", number, err)
	}

	count := binary.LittleEndian.Uint32(raw[0:4])
	if count > uint32(ix.keysPerPage) {
		return nil, fmt.Errorf("%w: page %d claims %d entries, capacity is %d",
			ErrCorrupt, number, count, ix.keysPerPage)
	}

	p := &page{number: number, entries: make([]pageEntry, 0, count)}
	off := uint32(4)
	for i := uint32(0); i < count; i++ {
		e := pageEntry{
			lower: binary.LittleEndian.Uint32(raw[off:]),
			recno: binary.LittleEndian.Uint32(raw[off+4:]),
			key:   append([]byte(nil), raw[off+8:off+8+uint32(ix.keySize)]...),
		}
		p.entries = append(p.entries, e)
		off += ix.recordSize
	}
	return p, nil
}

// writePage encodes one page.
func (ix *Index) writePage(p *page) error {
	if len(p.entries) > int(ix.keysPerPage) {
		return fmt.Errorf("ndx: %d entries exceeds page capacity %d",
			len(p.entries), ix.keysPerPage)
	}
	var raw [PageSize]byte
	binary.LittleEndian.PutUint32(raw[0:4], uint32(len(p.entries)))

	off := uint32(4)
	for _, e := range p.entries {
		binary.LittleEndian.PutUint32(raw[off:], e.lower)
		binary.LittleEndian.PutUint32(raw[off+4:], e.recno)
		copy(raw[off+8:off+8+uint32(ix.keySize)], e.key)
		off += ix.recordSize
	}

	if _, err := ix.rw.Seek(pageOffset(p.number), io.SeekStart); err != nil {
		return fmt.Errorf("ndx: seek to page %d: %w", p.number, err)
	}
	if _, err := ix.rw.Write(raw[:]); err != nil {
		return fmt.Errorf("ndx: write page %d: %w", p.number, err)
	}
	return nil
}

// compareKeys orders two keys according to the index's key type.
//
// Character keys compare as unsigned bytes, which is what
// bytes.Compare does. Numeric keys are IEEE-754 doubles stored
// plainly — no byte reversal, no bit inversion — so they must be
// decoded before comparison rather than compared as bytes.
//
// That last point is the one that would silently corrupt an
// implementation carrying an assumption across from CDX, where the
// stored form *is* byte-comparable by design.
func (ix *Index) compareKeys(a, b []byte) int {
	if ix.keyType == KeyTypeNumeric {
		return compareDoubles(a, b)
	}
	return bytes.Compare(a, b)
}

// compareDoubles orders two 8-byte IEEE-754 keys.
func compareDoubles(a, b []byte) int {
	if len(a) < 8 || len(b) < 8 {
		return bytes.Compare(a, b)
	}
	x := math.Float64frombits(binary.LittleEndian.Uint64(a))
	y := math.Float64frombits(binary.LittleEndian.Uint64(b))
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	default:
		return 0
	}
}

// EncodeNumericKey produces the stored form of a numeric key.
//
// Provided because a caller deriving keys from records needs to
// produce the same bytes the format expects, and the encoding
// differs from the CDX convention in a way that is easy to get
// wrong.
func EncodeNumericKey(v float64) []byte {
	out := make([]byte, 8)
	binary.LittleEndian.PutUint64(out, math.Float64bits(v))
	return out
}

// DecodeNumericKey reads a stored numeric key.
func DecodeNumericKey(key []byte) (float64, error) {
	if len(key) < 8 {
		return 0, fmt.Errorf("%w: numeric key is %d bytes, want 8", ErrKeySize, len(key))
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(key)), nil
}

// walk visits every entry in key order, calling fn for each.
//
// The traversal is an explicit stack rather than recursion, and
// carries a visited set: a corrupt file can contain a page that
// points back into its own ancestry, and recursion there would
// exhaust the stack rather than report the problem.
func (ix *Index) walk(fn func(Entry) error) error {
	if ix.root == 0 {
		return nil
	}
	visited := map[uint32]bool{}
	return ix.walkPage(ix.root, visited, fn)
}

func (ix *Index) walkPage(number uint32, visited map[uint32]bool, fn func(Entry) error) error {
	if number == 0 {
		return nil
	}
	if visited[number] {
		return fmt.Errorf("%w: page %d revisited during traversal", ErrCorrupt, number)
	}
	visited[number] = true

	p, err := ix.readPage(number)
	if err != nil {
		return err
	}
	for _, e := range p.entries {
		// In-order: everything below a key comes before it.
		if e.lower != 0 {
			if err := ix.walkPage(e.lower, visited, fn); err != nil {
				return err
			}
		}
		// A record number of zero marks a separator entry that
		// carries only a subtree pointer, with no record of its
		// own.
		if e.recno != 0 {
			if err := fn(Entry{Key: append([]byte(nil), e.key...), Recno: e.recno}); err != nil {
				return err
			}
		}
	}
	return nil
}
