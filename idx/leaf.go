package idx

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sort"

	"github.com/ha1tch/blipper/cdx"
)

// decodeLeaf decodes a compact leaf page. Mirrors
// cdx.decodeExterior's algorithm exactly — bit-packed triples
// growing forward from offset 24, key text packed backward from
// the page end, front-compression against the previous key via
// dup/trail counts. Kept standalone rather than calling into cdx
// because that decoder is coupled to cdx's internal node type.
func decodeLeaf(raw []byte, keyLen uint16) ([]Entry, error) {
	if len(raw) != PageSize {
		return nil, fmt.Errorf("%w: leaf page is %d bytes, want %d", ErrCorrupt, len(raw), PageSize)
	}
	nKeys := binary.LittleEndian.Uint16(raw[2:4])

	recMask := binary.LittleEndian.Uint32(raw[14:18])
	dupMask := uint64(raw[18])
	trailMask := uint64(raw[19])
	recBits := uint(raw[20])
	dupBits := uint(raw[21])
	trailBits := uint(raw[22])
	bpe := int(raw[23])

	if bpe <= 0 || bpe > 8 || recBits+dupBits+trailBits > uint(bpe)*8 {
		return nil, fmt.Errorf("%w: malformed leaf entry header", ErrCorrupt)
	}
	entriesStart := 24
	entriesEnd := entriesStart + int(nKeys)*bpe
	if entriesEnd > PageSize {
		return nil, fmt.Errorf("%w: %d entries of %d bytes overruns the page", ErrCorrupt, nKeys, bpe)
	}

	keyEnd := PageSize
	prev := make([]byte, keyLen)
	out := make([]Entry, nKeys)
	for i := 0; i < int(nKeys); i++ {
		off := entriesStart + i*bpe
		var packed uint64
		for j := bpe - 1; j >= 0; j-- {
			packed = (packed << 8) | uint64(raw[off+j])
		}
		recNo := uint32(packed & uint64(recMask))
		dup := (packed >> recBits) & dupMask
		trail := (packed >> (recBits + dupBits)) & trailMask
		if dup > uint64(keyLen) || trail > uint64(keyLen) {
			return nil, fmt.Errorf("%w: dup/trail exceeds key length", ErrCorrupt)
		}
		nChars := int(uint64(keyLen) - dup - trail)
		if nChars < 0 || nChars > int(keyLen) {
			return nil, fmt.Errorf("%w: bad character count", ErrCorrupt)
		}
		keyEnd -= nChars
		if keyEnd < 0 {
			return nil, fmt.Errorf("%w: key text underruns the page", ErrCorrupt)
		}
		key := make([]byte, keyLen)
		copy(key, prev[:dup])
		copy(key[dup:], raw[keyEnd:keyEnd+nChars])
		for k := int(dup) + nChars; k < int(keyLen); k++ {
			key[k] = ' ' // trailing-space reconstruction
		}
		out[i] = Entry{Key: key, RecNo: recNo}
		prev = key
	}
	return out, nil
}

// Open reads an existing compact .IDX index.
func Open(rw io.ReadWriteSeeker) (*Index, error) {
	ix, err := readHeader(rw)
	if err != nil {
		return nil, err
	}
	ix.rw = rw
	return ix, nil
}

// Create writes an empty compact-format .IDX index.
func Create(rw io.ReadWriteSeeker, opts Options) (*Index, error) {
	if opts.KeySize == 0 || opts.KeySize > 240 {
		return nil, fmt.Errorf("%w: key size %d out of range", ErrInvalidHeader, opts.KeySize)
	}
	ix := &Index{
		rw:      rw,
		keyExpr: opts.KeyExpr,
		keySize: opts.KeySize,
		opts:    OptCompact,
	}
	if opts.Unique {
		ix.opts |= OptUnique
	}
	if err := ix.writeHeader(); err != nil {
		return nil, err
	}
	return ix, nil
}

// Build writes a single-leaf compact index from a set of entries.
// Matches the CDX Phase 1 scope this package shares: entries must
// fit one 512-byte leaf page. A table small enough to be indexed
// by a single INDEX ON in the oracle's own test cases fits this
// by construction; a larger one returns ErrTooManyKeys rather
// than silently truncating.
func (ix *Index) Build(entries []Entry) error {
	for _, e := range entries {
		if len(e.Key) != int(ix.keySize) {
			return fmt.Errorf("%w: got %d bytes, want %d", ErrKeySize, len(e.Key), ix.keySize)
		}
	}
	sorted := append([]Entry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if c := bytes.Compare(sorted[i].Key, sorted[j].Key); c != 0 {
			return c < 0
		}
		return sorted[i].RecNo < sorted[j].RecNo
	})
	if ix.opts&OptUnique != 0 {
		d := sorted[:0]
		for i, e := range sorted {
			if i > 0 && bytes.Equal(sorted[i-1].Key, e.Key) {
				continue
			}
			d = append(d, e)
		}
		sorted = d
	}

	block := make([]byte, PageSize)
	if err := cdx.WriteLeaf(block, sorted, ix.keySize); err != nil {
		if len(sorted) == 0 {
			// An empty leaf is legitimate; WriteLeaf may reject a
			// zero-entry block for reasons unrelated to capacity.
			binary.LittleEndian.PutUint16(block[2:4], 0)
		} else {
			return fmt.Errorf("%w: %v", ErrTooManyKeys, err)
		}
	}

	const leafOffset = PageSize // header is page 0; leaf goes at byte offset 512
	if _, err := ix.rw.Seek(leafOffset, io.SeekStart); err != nil {
		return err
	}
	if _, err := ix.rw.Write(block); err != nil {
		return err
	}
	ix.root = leafOffset
	ix.eof = leafOffset + PageSize
	return ix.writeHeader()
}

// Entries returns every entry in key order.
func (ix *Index) Entries() ([]Entry, error) {
	if ix.root == 0 {
		return nil, nil
	}
	if _, err := ix.rw.Seek(int64(ix.root), io.SeekStart); err != nil {
		return nil, err
	}
	raw := make([]byte, PageSize)
	if _, err := io.ReadFull(ix.rw, raw); err != nil {
		return nil, err
	}
	return decodeLeaf(raw, ix.keySize)
}

// Seek returns record numbers whose key equals the one given.
func (ix *Index) Seek(key []byte) ([]uint32, error) {
	if len(key) != int(ix.keySize) {
		return nil, fmt.Errorf("%w: got %d bytes, want %d", ErrKeySize, len(key), ix.keySize)
	}
	all, err := ix.Entries()
	if err != nil {
		return nil, err
	}
	var out []uint32
	for _, e := range all {
		if bytes.Equal(e.Key, key) {
			out = append(out, e.RecNo)
		}
	}
	return out, nil
}
