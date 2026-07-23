package ntx

import (
	"bytes"
	"fmt"
	"io"
)

// Create writes a new, empty NTX index to rw.
//
// Create does not close rw; the caller retains ownership of the
// underlying file.
func Create(rw io.ReadWriteSeeker, opts Options) (*Index, error) {
	if opts.KeySize == 0 || opts.KeySize > MaxKeySize {
		return nil, fmt.Errorf(
			"key size must be 1..%d, have %d",
			MaxKeySize,
			opts.KeySize,
		)
	}

	if len(opts.KeyExpr) >= maxKeyExpr {
		return nil, fmt.Errorf(
			"key expression exceeds %d bytes",
			maxKeyExpr-1,
		)
	}

	itemSize := opts.KeySize + 8
	maxItem := computeMaxItem(itemSize)

	if maxItem < 2 {
		return nil, fmt.Errorf(
			"key size %d leaves fewer than 2 keys per page",
			opts.KeySize,
		)
	}

	ix := &Index{
		rw:       rw,
		keyExpr:  opts.KeyExpr,
		keySize:  opts.KeySize,
		decimals: opts.Decimals,
		unique:   opts.Unique,
		itemSize: itemSize,
		maxItem:  maxItem,
		halfPage: maxItem / 2,
		root:     pageSize,
		nextFree: 0,
		fileSize: 2 * pageSize,
	}

	if err := ix.writeHeader(); err != nil {
		return nil, err
	}

	// Empty root leaf.
	root := &node{offset: ix.root, leaf: true}

	if err := ix.writeNode(root); err != nil {
		return nil, err
	}

	return ix, nil
}

// Open reads the header of an existing NTX index.
//
// Open does not close rw; the caller retains ownership of the
// underlying file.
func Open(rw io.ReadWriteSeeker) (*Index, error) {
	size, err := rw.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}

	if size < 2*pageSize {
		return nil, fmt.Errorf(
			"file is %d bytes; an NTX index is at least %d",
			size,
			2*pageSize,
		)
	}

	ix := &Index{
		rw:       rw,
		fileSize: size,
	}

	if err := ix.readHeader(); err != nil {
		return nil, err
	}

	return ix, nil
}

// KeyExpr returns the key expression stored in the header.
func (ix *Index) KeyExpr() string {
	return ix.keyExpr
}

// KeySize returns the fixed key length in bytes.
func (ix *Index) KeySize() uint16 {
	return ix.keySize
}

// Unique reports whether the index keeps only the first record for
// each key value.
func (ix *Index) Unique() bool {
	return ix.unique
}

// compareEntries orders (key, recno) pairs: key bytes first, record
// number as tiebreak.
func compareEntries(aKey []byte, aRecno uint32, bKey []byte, bRecno uint32) int {
	if c := bytes.Compare(aKey, bKey); c != 0 {
		return c
	}

	switch {
	case aRecno < bRecno:
		return -1
	case aRecno > bRecno:
		return 1
	default:
		return 0
	}
}

// findInNode returns the position of the first item in n that is not
// below (key, recno).
func findInNode(n *node, key []byte, recno uint32) int {
	lo, hi := 0, len(n.items)

	for lo < hi {
		mid := (lo + hi) / 2

		it := n.items[mid]

		if compareEntries(it.key, it.recno, key, recno) < 0 {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	return lo
}

// normalizeKey pads a short key with trailing spaces to the index key
// size, mirroring the fixed-width keys Clipper produces. Longer keys
// are an error.
func (ix *Index) normalizeKey(key []byte) ([]byte, error) {
	if len(key) > int(ix.keySize) {
		return nil, fmt.Errorf(
			"key is %d bytes, index key size is %d",
			len(key),
			ix.keySize,
		)
	}

	if len(key) == int(ix.keySize) {
		return key, nil
	}

	padded := make([]byte, ix.keySize)
	copy(padded, key)

	for i := len(key); i < len(padded); i++ {
		padded[i] = ' '
	}

	return padded, nil
}

// ContainsKey reports whether any entry has exactly the given key,
// after space padding to the key size.
func (ix *Index) ContainsKey(key []byte) (bool, error) {
	key, err := ix.normalizeKey(key)
	if err != nil {
		return false, err
	}

	offset := ix.root

	for {
		n, err := ix.readNode(offset)
		if err != nil {
			return false, err
		}

		// Position of the first entry with key >= wanted,
		// regardless of record number.
		i := findInNode(n, key, 0)

		if i < len(n.items) && bytes.Equal(n.items[i].key, key) {
			return true, nil
		}

		if n.leaf {
			return false, nil
		}

		offset = n.childAt(i)
	}
}
