package cdx

import (
	"fmt"
	"io"
	"strings"
)

// File is an open CDX. It holds the top-level file header and a
// map of tag name → per-tag root-block pointer.
//
// The file operates on a stream it does not own; the caller closes
// the underlying io.Reader/Seeker.
type File struct {
	rw     io.ReadSeeker
	header *header
	tags   []Tag
}

// Tag names one index within a compound CDX along with the
// per-tag header offset (in bytes from the start of the file)
// and the per-tag header itself, lazily loaded.
type Tag struct {
	Name      string
	HeaderPtr uint32 // block offset in bytes to the tag's own header
	perTagHdr *header
}

// Open reads and validates a CDX and enumerates its tags. It does
// not descend into the per-tag key indexes; call Tag(name) or
// Traverse for that.
//
// Every CDX has a top-level "tag directory": a compact index whose
// keys are 10-byte right-space-padded tag names and whose leaf
// recNo values are block-offset pointers to per-tag headers.
func Open(rw io.ReadSeeker) (*File, error) {
	h, err := readHeader(rw)
	if err != nil {
		return nil, err
	}
	f := &File{rw: rw, header: h}
	if err := f.loadTags(); err != nil {
		return nil, err
	}
	return f, nil
}

// Tags returns the tag directory as read from disk. Tags are
// returned in the order the directory yields them (i.e. sorted by
// name under MACHINE collation).
func (f *File) Tags() []Tag { return f.tags }

// TagNames returns just the tag names, convenient for tests and
// diagnostics.
func (f *File) TagNames() []string {
	out := make([]string, len(f.tags))
	for i, t := range f.tags {
		out[i] = t.Name
	}
	return out
}

// Tag looks up a tag by name and returns its resolved entry
// including the per-tag header. The lookup is case-sensitive as
// CDX tag names are stored uppercase by convention.
func (f *File) Tag(name string) (*Tag, error) {
	want := strings.ToUpper(name)
	for i := range f.tags {
		if strings.TrimSpace(f.tags[i].Name) == want {
			if f.tags[i].perTagHdr == nil {
				sub, err := f.loadPerTagHeader(uint32(f.tags[i].HeaderPtr))
				if err != nil {
					return nil, fmt.Errorf("cdx: load tag %q header: %w", want, err)
				}
				f.tags[i].perTagHdr = sub
			}
			return &f.tags[i], nil
		}
	}
	return nil, ErrTagNotFound
}

// KeyExpr returns the key expression string of a resolved tag.
// The expression is uncompiled text, as originally stored by the
// writer (e.g. Clipper's INDEX ON <expr> TAG <name>).
func (t *Tag) KeyExpr() string {
	if t.perTagHdr == nil {
		return ""
	}
	return t.perTagHdr.keyExpr
}

// FORExpr returns the FOR-clause expression of a resolved tag, or
// "" if the tag has no FOR clause.
func (t *Tag) FORExpr() string {
	if t.perTagHdr == nil {
		return ""
	}
	return t.perTagHdr.forExpr
}

// Descending reports whether the tag was created with the DESCEND
// option. Blipper honours this at traversal time.
func (t *Tag) Descending() bool {
	if t.perTagHdr == nil {
		return false
	}
	return t.perTagHdr.descending
}

// loadTags walks the top-level tag-directory tree, collecting
// every (tag-name, tag-header-pointer) leaf entry. The tag
// directory's key length is 10, matching the tag-name width used
// by DBFCDX.LIB and FoxPro 2.x.
func (f *File) loadTags() error {
	const tagDirKeyLen = 10
	// The tag directory's root is at header.rootOffset. Leaves of
	// this tree carry tag headers-pointers in their recNo field.
	var walk func(offset int64) error
	walk = func(offset int64) error {
		n, err := readNode(f.rw, offset, tagDirKeyLen)
		if err != nil {
			return err
		}
		if n.isLeaf() {
			for _, e := range n.entries {
				name := strings.TrimRight(string(e.key), " ")
				f.tags = append(f.tags, Tag{Name: name, HeaderPtr: e.recNo})
			}
			return nil
		}
		for _, e := range n.entries {
			// Interior entries carry the child page's byte offset.
			if err := walk(int64(e.recNo)); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(int64(f.header.rootOffset))
}

// loadPerTagHeader reads a per-tag header at the given block
// offset. Per-tag headers use the same descriptor layout as the
// top-level file header AND carry the compound-header flag —
// verified against a Clipper-generated CDX; the option bits at
// offset 14 are 0x60 (compact + compound-header) on both the
// top-level and per-tag headers.
//
// The key-expression pool sits at descriptor-relative bytes
// 512-1023, i.e. the block immediately after the descriptor.
// We therefore read 2*BlockSize bytes here rather than just one
// block.
func (f *File) loadPerTagHeader(offset uint32) (*header, error) {
	if _, err := f.rw.Seek(int64(offset), io.SeekStart); err != nil {
		return nil, fmt.Errorf("cdx: seek to tag header: %w", err)
	}
	var raw [2 * BlockSize]byte
	if _, err := io.ReadFull(f.rw, raw[:]); err != nil {
		return nil, err
	}

	h := &header{}
	h.rootOffset = int32(uint32(raw[0]) | uint32(raw[1])<<8 |
		uint32(raw[2])<<16 | uint32(raw[3])<<24)
	h.keyLen = uint16(raw[12]) | uint16(raw[13])<<8
	h.options = raw[14]
	h.signature = raw[15]
	h.descending = (uint16(raw[502]) | uint16(raw[503])<<8) != orderAscending

	keyPoolLen := int(uint16(raw[510]) | uint16(raw[511])<<8)
	forPoolLen := int(uint16(raw[506]) | uint16(raw[507])<<8)
	if keyPoolLen > 0 && 512+keyPoolLen <= len(raw) {
		h.keyExpr = trimNul(raw[512 : 512+keyPoolLen])
	}
	if forPoolLen > 0 && 512+keyPoolLen+forPoolLen <= len(raw) {
		h.forExpr = trimNul(raw[512+keyPoolLen : 512+keyPoolLen+forPoolLen])
	}
	return h, nil
}

// KeyLen returns the tag's fixed key width in bytes.
func (t *Tag) KeyLen() uint16 {
	if t.perTagHdr == nil {
		return 0
	}
	return t.perTagHdr.keyLen
}
