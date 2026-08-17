package ndx

import (
	"fmt"
	"io"
	"sort"
)

// Open reads an existing NDX index.
func Open(rw io.ReadWriteSeeker) (*Index, error) {
	ix, err := readHeader(rw)
	if err != nil {
		return nil, err
	}
	ix.rw = rw
	return ix, nil
}

// Create writes an empty NDX index.
//
// An empty index has a header and no root page; the root pointer
// stays zero until the first entry is written. That matches what
// the format allows and keeps Create cheap, since a caller who
// immediately calls Build would otherwise pay for a page that is
// about to be replaced.
func Create(rw io.ReadWriteSeeker, opts Options) (*Index, error) {
	if opts.KeySize == 0 {
		return nil, fmt.Errorf("%w: key size must be non-zero", ErrInvalidHeader)
	}
	if opts.KeySize > MaxKeySize {
		return nil, fmt.Errorf("%w: key size %d exceeds %d",
			ErrInvalidHeader, opts.KeySize, MaxKeySize)
	}
	switch opts.KeyType {
	case KeyTypeCharacter:
	case KeyTypeNumeric:
		if opts.KeySize != 8 {
			return nil, fmt.Errorf("%w: numeric keys are 8 bytes, got %d",
				ErrInvalidHeader, opts.KeySize)
		}
	default:
		return nil, fmt.Errorf("%w: unknown key type %d", ErrInvalidHeader, opts.KeyType)
	}
	if len(opts.KeyExpr) >= MaxKeyExpr {
		return nil, fmt.Errorf("%w: key expression exceeds %d bytes",
			ErrInvalidHeader, MaxKeyExpr-1)
	}

	recordSize := keyRecordSize(opts.KeySize)
	perPage := keysPerPageFor(recordSize)
	if perPage < 2 {
		// A page holding fewer than two entries cannot form a
		// tree; the key is too wide for the page size.
		return nil, fmt.Errorf("%w: key size %d leaves room for %d entries per page",
			ErrInvalidHeader, opts.KeySize, perPage)
	}

	ix := &Index{
		rw:          rw,
		keyExpr:     opts.KeyExpr,
		keySize:     opts.KeySize,
		keyType:     opts.KeyType,
		unique:      opts.Unique,
		keysPerPage: perPage,
		recordSize:  recordSize,
		root:        0,
		pageCount:   1, // the header itself
	}
	if err := ix.writeHeader(); err != nil {
		return nil, err
	}
	return ix, nil
}

// Build writes a complete index from a set of entries.
//
// Entries are sorted and packed into pages, which produces a
// balanced tree in one pass. This is how dBASE's own INDEX ON
// command behaves — it reads the table and writes the index whole
// — and it is far cheaper than inserting one key at a time, which
// would rebalance repeatedly.
//
// Any existing content is replaced.
func (ix *Index) Build(entries []Entry) error {
	for _, e := range entries {
		if len(e.Key) != int(ix.keySize) {
			return fmt.Errorf("%w: key %q is %d bytes, index expects %d",
				ErrKeySize, e.Key, len(e.Key), ix.keySize)
		}
	}

	sorted := make([]Entry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool {
		if c := ix.compareKeys(sorted[i].Key, sorted[j].Key); c != 0 {
			return c < 0
		}
		// Record number breaks ties, so an index over a
		// non-unique key still has a deterministic order.
		return sorted[i].Recno < sorted[j].Recno
	})

	if ix.unique {
		deduped := sorted[:0]
		for i, e := range sorted {
			if i > 0 && ix.compareKeys(sorted[i-1].Key, e.Key) == 0 {
				continue
			}
			deduped = append(deduped, e)
		}
		sorted = deduped
	}

	if len(sorted) == 0 {
		ix.root = 0
		ix.pageCount = 1
		return ix.writeHeader()
	}

	// Leaf level: pack entries into pages in order.
	perPage := int(ix.keysPerPage)
	var leaves []*page
	next := uint32(1)
	for i := 0; i < len(sorted); i += perPage {
		end := i + perPage
		if end > len(sorted) {
			end = len(sorted)
		}
		p := &page{number: next}
		for _, e := range sorted[i:end] {
			p.entries = append(p.entries, pageEntry{
				lower: 0,
				recno: e.Recno,
				key:   append([]byte(nil), e.Key...),
			})
		}
		leaves = append(leaves, p)
		next++
	}

	// allPages accumulates every page written, at every level.
	allPages := append([]*page(nil), leaves...)

	// Interior levels: one separator per child, carrying that
	// child's highest key. Repeat until a single page remains.
	level := leaves
	for len(level) > 1 {
		var parents []*page
		for i := 0; i < len(level); i += perPage {
			end := i + perPage
			if end > len(level) {
				end = len(level)
			}
			p := &page{number: next}
			next++
			for _, child := range level[i:end] {
				last := child.entries[len(child.entries)-1]
				p.entries = append(p.entries, pageEntry{
					lower: child.number,
					// A separator carries no record of its own;
					// the walk skips entries with recno zero.
					recno: 0,
					key:   append([]byte(nil), last.key...),
				})
			}
			parents = append(parents, p)
		}
		allPages = append(allPages, parents...)
		level = parents
	}

	for _, p := range allPages {
		if err := ix.writePage(p); err != nil {
			return err
		}
	}

	ix.root = level[0].number
	ix.pageCount = next
	return ix.writeHeader()
}

// Traverse visits every entry in key order.
func (ix *Index) Traverse(fn func(Entry) error) error {
	return ix.walk(fn)
}

// Entries returns every entry in key order.
//
// Convenience over Traverse for callers that want the whole set;
// an index large enough for this to be a problem wants Traverse.
func (ix *Index) Entries() ([]Entry, error) {
	var out []Entry
	err := ix.walk(func(e Entry) error {
		out = append(out, e)
		return nil
	})
	return out, err
}

// Count returns the number of entries in the index.
func (ix *Index) Count() (int, error) {
	n := 0
	err := ix.walk(func(Entry) error {
		n++
		return nil
	})
	return n, err
}

// Seek returns the record numbers whose key equals the one given,
// in the order they appear in the index.
//
// A descent would be faster, but the tree has no separator
// convention this package can rely on for early termination on a
// non-unique index: equal keys may span pages. Walking is correct
// for every shape the format permits, and correctness comes first
// until there is a measured reason otherwise.
func (ix *Index) Seek(key []byte) ([]uint32, error) {
	if len(key) != int(ix.keySize) {
		return nil, fmt.Errorf("%w: key is %d bytes, index expects %d",
			ErrKeySize, len(key), ix.keySize)
	}
	var out []uint32
	err := ix.walk(func(e Entry) error {
		if ix.compareKeys(e.Key, key) == 0 {
			out = append(out, e.Recno)
		}
		return nil
	})
	return out, err
}

// First returns the lowest entry, and whether the index has one.
func (ix *Index) First() (Entry, bool, error) {
	var first Entry
	found := false
	err := ix.walk(func(e Entry) error {
		if !found {
			first = e
			found = true
		}
		return nil
	})
	return first, found, err
}

// Last returns the highest entry, and whether the index has one.
func (ix *Index) Last() (Entry, bool, error) {
	var last Entry
	found := false
	err := ix.walk(func(e Entry) error {
		last = e
		found = true
		return nil
	})
	return last, found, err
}
