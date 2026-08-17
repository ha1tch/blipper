package mdx

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
)

// Leaf page, verified against ACCT_REC.MDX's three tags:
//
//	0-3   entry count
//	4-7   reserved / sibling pointer (0 observed; single-leaf trees
//	      here, so this was never exercised as a real pointer)
//	8-    entries, `ItemSize` bytes each: 4-byte recno (LE) then
//	      KeySize bytes of key data, the remainder of ItemSize
//	      being padding.
//
// This differs from CDX/IDX's compact bit-packed leaf and from
// NDX's fixed layout — it is its own third scheme, plain and
// unpacked but with a stride wider than the raw field count.
func readLeaf(r io.ReadSeeker, page uint32, keySize, itemSize uint16) ([]TagEntry, error) {
	if _, err := r.Seek(int64(page)*pageSize, io.SeekStart); err != nil {
		return nil, err
	}
	var raw [pageSize]byte
	if _, err := io.ReadFull(r, raw[:]); err != nil {
		return nil, err
	}
	count := binary.LittleEndian.Uint32(raw[0:4])
	if int(count)*int(itemSize)+8 > pageSize {
		return nil, fmt.Errorf("%w: %d entries of %d bytes overruns the page", ErrCorrupt, count, itemSize)
	}
	out := make([]TagEntry, count)
	p := 8
	for i := uint32(0); i < count; i++ {
		recno := binary.LittleEndian.Uint32(raw[p : p+4])
		key := append([]byte(nil), raw[p+4:p+4+int(keySize)]...)
		out[i] = TagEntry{Key: key, RecNo: recno}
		p += int(itemSize)
	}
	return out, nil
}

func writeLeaf(w io.WriteSeeker, page uint32, entries []TagEntry, keySize, itemSize uint16) error {
	if 8+len(entries)*int(itemSize) > pageSize {
		return ErrTooManyKeys
	}
	var raw [pageSize]byte
	binary.LittleEndian.PutUint32(raw[0:4], uint32(len(entries)))
	p := 8
	for _, e := range entries {
		binary.LittleEndian.PutUint32(raw[p:p+4], e.RecNo)
		copy(raw[p+4:p+4+int(keySize)], e.Key)
		p += int(itemSize)
	}
	if _, err := w.Seek(int64(page)*pageSize, io.SeekStart); err != nil {
		return err
	}
	_, err := w.Write(raw[:])
	return err
}

// Open reads an existing .MDX file and its tag directory.
func Open(rw io.ReadWriteSeeker) (*File, error) {
	f, err := readHeader(rw)
	if err != nil {
		return nil, err
	}
	f.rw = rw
	return f, nil
}

// Create writes an empty .MDX file for the given data file name
// (no extension, matching the header's own convention).
func Create(rw io.ReadWriteSeeker, dataFile string) (*File, error) {
	f := &File{rw: rw, dataFile: dataFile, pages: headerPages}
	if err := writeHeader(rw, dataFile, nil, f.pages); err != nil {
		return nil, err
	}
	return f, nil
}

// AddTag builds a single-leaf tag from entries and appends it to
// the file, updating the tag directory.
//
// Numeric keys must be produced by EncodeNumericKey — 12 bytes,
// matching the width observed in the oracle specimens (KeySize
// 12, ItemSize 16). Comparison for Numeric tags decodes each key
// rather than comparing bytes, because the encoding is not
// byte-comparable: see compareKeys.
//
// Scope matches idx/cdx Phase 1: entries must fit one 512-byte
// leaf. A table small enough for a single INDEX ON/TAG ON in the
// oracle's own reference cases fits this by construction.
func (f *File) AddTag(name string, keyType byte, keySize uint16, entries []TagEntry, unique bool) error {
	if len(f.tags) >= maxTags {
		return fmt.Errorf("mdx: tag table full (max %d)", maxTags)
	}
	for _, e := range entries {
		if len(e.Key) != int(keySize) {
			return fmt.Errorf("mdx: entry key is %d bytes, want %d", len(e.Key), keySize)
		}
	}
	itemSize := uint16(align4(4 + int(keySize)))

	sorted := append([]TagEntry(nil), entries...)
	var sortErr error
	sort.SliceStable(sorted, func(i, j int) bool {
		if sortErr != nil {
			return false
		}
		c, err := compareKeys(keyType, sorted[i].Key, sorted[j].Key)
		if err != nil {
			sortErr = err
			return false
		}
		if c != 0 {
			return c < 0
		}
		return sorted[i].RecNo < sorted[j].RecNo
	})
	if sortErr != nil {
		return fmt.Errorf("mdx: sorting entries: %w", sortErr)
	}
	if unique {
		d := sorted[:0]
		for i, e := range sorted {
			if i > 0 && bytes.Equal(sorted[i-1].Key, e.Key) {
				continue
			}
			d = append(d, e)
		}
		sorted = d
	}
	if 8+len(sorted)*int(itemSize) > pageSize {
		return ErrTooManyKeys
	}

	headerPage := f.pages
	leafPage := f.pages + 1
	if err := writeTagHeader(f.rw, headerPage, Tag{KeyType: keyType, KeySize: keySize, ItemSize: itemSize, Unique: unique}, leafPage, 2); err != nil {
		return err
	}
	if err := writeLeaf(f.rw, leafPage, sorted, keySize, itemSize); err != nil {
		return err
	}
	f.pages += 2
	f.tags = append(f.tags, Tag{
		Name: name, KeyType: keyType, KeySize: keySize, ItemSize: itemSize,
		Unique: unique, headerPage: headerPage,
	})
	return writeHeader(f.rw, f.dataFile, f.tags, f.pages)
}

// TagEntries returns a tag's entries in key order, including
// Numeric tags — decode individual values with DecodeNumericKey.
func (f *File) TagEntries(name string) ([]TagEntry, error) {
	t, ok := f.Tag(name)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrTagNotFound, name)
	}
	th, err := readTagHeader(f.rw, t.headerPage)
	if err != nil {
		return nil, err
	}
	return readLeaf(f.rw, th.root, t.KeySize, t.ItemSize)
}
