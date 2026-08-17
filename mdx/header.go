package mdx

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// File header, verified 2026-07-23/24 against ACCT_REC.MDX
// (docs/INDEX_FORMATS.md, cross-checked byte-for-byte):
//
//	0       version
//	1-3     creation date YYMMDD
//	4-19    data file name, NUL-padded
//	20-21   block size (observed value 2; meaning not established —
//	        NOT the page size, which is fixed 512 regardless)
//	22-23   block size adder
//	24      production index flag
//	25      tag table entry count (observed 48, the maximum)
//	26      tag table entry length (observed 32)
//	27      reserved
//	28-29   tags in use
//	30-31   reserved
//	32-35   pages in file (file size = pages * 512)
//	36-39   first free page
//	40-43   blocks available
//	44-46   last update YYMMDD
//	47      reserved
//	544-    tag table, 32 bytes per entry
const (
	offVersion  = 0
	offDataFile = 4
	offProd     = 24
	offTagCount = 28
	offPages    = 32
	offFree     = 36
)

func readHeader(r io.ReadSeeker) (*File, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	// The tag table starts at 544 and can run to 544+48*32=2080,
	// past the first 512-byte page.
	raw := make([]byte, tagTableOff+maxTags*tagEntrySize)
	if _, err := io.ReadFull(r, raw); err != nil {
		return nil, fmt.Errorf("mdx: read header: %w", err)
	}
	dataFile := string(bytes.TrimRight(raw[offDataFile:offDataFile+16], "\x00"))
	tagsInUse := binary.LittleEndian.Uint16(raw[offTagCount:])
	pages := binary.LittleEndian.Uint32(raw[offPages:])
	if tagsInUse > maxTags {
		return nil, fmt.Errorf("%w: %d tags exceeds maximum %d", ErrInvalidHeader, tagsInUse, maxTags)
	}

	f := &File{dataFile: dataFile, pages: pages}

	for i := uint16(0); i < tagsInUse; i++ {
		o := tagTableOff + int(i)*tagEntrySize
		e := raw[o : o+tagEntrySize]
		page := binary.LittleEndian.Uint32(e[0:4])
		name := string(bytes.TrimRight(e[4:15], "\x00"))
		ktype := e[20]

		th, err := readTagHeader(r, page)
		if err != nil {
			return nil, fmt.Errorf("mdx: tag %q header: %w", name, err)
		}
		f.tags = append(f.tags, Tag{
			Name: name, KeyType: ktype,
			KeySize: th.keySize, ItemSize: th.itemSize,
			Unique: th.unique, headerPage: page,
		})
	}
	return f, nil
}

type tagHeaderFields struct {
	root     uint32
	keySize  uint16
	itemSize uint16
	unique   bool
}

// Tag header, 24 bytes at the tag's page:
//
//	0-3    root page pointer
//	4-7    file size in pages (this tag's subtree)
//	8      key format
//	9      key type
//	10-11  reserved
//	12-13  index key length (ikl)
//	14-15  max keys per page
//	16-17  secondary key type
//	18-19  index key item length — the real per-entry stride,
//	       found empirically: align4(4 + ikl). The file does not
//	       document the rounding; it was derived by decoding
//	       leaf entries until the stride that produced correctly
//	       sorted keys was found.
//	20-22  reserved
//	23     unique flag
func readTagHeader(r io.ReadSeeker, page uint32) (*tagHeaderFields, error) {
	off := int64(page) * pageSize
	if _, err := r.Seek(off, io.SeekStart); err != nil {
		return nil, err
	}
	var raw [24]byte
	if _, err := io.ReadFull(r, raw[:]); err != nil {
		return nil, err
	}
	return &tagHeaderFields{
		root:     binary.LittleEndian.Uint32(raw[0:4]),
		keySize:  binary.LittleEndian.Uint16(raw[12:14]),
		itemSize: binary.LittleEndian.Uint16(raw[18:20]),
		unique:   raw[23] != 0,
	}, nil
}

// headerPages is the fixed number of 512-byte pages reserved for
// the file header and tag table, verified against ACCT_REC.MDX:
// its first tag begins at page 4 regardless of tagsInUse (3),
// which only makes sense if the reserved region is a fixed 4
// pages rather than sized to the declared 48-tag capacity (which
// would need 5 pages: ceil((544+48*32)/512)). The 48-capacity
// field is a declared maximum, not a physical allocation.
const headerPages = 4

func writeHeader(w io.WriteSeeker, dataFile string, tags []Tag, pages uint32) error {
	raw := make([]byte, headerPages*pageSize)
	raw[offVersion] = 2
	copy(raw[offDataFile:offDataFile+16], dataFile)
	raw[offProd] = 1
	raw[25] = maxTags
	raw[26] = tagEntrySize
	binary.LittleEndian.PutUint16(raw[offTagCount:], uint16(len(tags)))
	binary.LittleEndian.PutUint32(raw[offPages:], pages)
	binary.LittleEndian.PutUint32(raw[offFree:], 0)

	for i, t := range tags {
		o := tagTableOff + i*tagEntrySize
		binary.LittleEndian.PutUint32(raw[o:o+4], t.headerPage)
		copy(raw[o+4:o+15], t.Name)
		raw[o+15] = 0x10 // key format: data field
		raw[o+20] = t.KeyType
	}

	if _, err := w.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err := w.Write(raw[:])
	return err
}

func writeTagHeader(w io.WriteSeeker, page uint32, t Tag, root uint32, filesizePages uint32) error {
	var raw [pageSize]byte // full page; only first 24 bytes meaningful
	binary.LittleEndian.PutUint32(raw[0:4], root)
	binary.LittleEndian.PutUint32(raw[4:8], filesizePages)
	raw[8] = 0x10
	raw[9] = t.KeyType
	binary.LittleEndian.PutUint16(raw[12:14], t.KeySize)
	binary.LittleEndian.PutUint16(raw[18:20], t.ItemSize)
	if t.Unique {
		raw[23] = 1
	}
	if _, err := w.Seek(int64(page)*pageSize, io.SeekStart); err != nil {
		return err
	}
	_, err := w.Write(raw[:])
	return err
}
