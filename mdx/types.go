// Package mdx implements dBASE IV/5 .MDX multi-tag index files.
//
// Scope, forced by what the available specimens actually contain
// (dbf/testdata/dbase5/full/, 15 files, none exceeding a few
// hundred keys): single-leaf per-tag trees only, matching the
// Phase 1 scope already established in cdx and idx. A tag whose
// leaf would overflow one page is out of scope.
//
// Character, Date, and Numeric key comparison are all implemented
// and verified. Character/Date compare as bytes, matching the
// format directly. Numeric keys use a third, distinct encoding —
// not NDX's plain IEEE double, not CDX/IDX's byte-reversed
// transformed double, but a normalized BCD floating-point form:
// biased decimal exponent, a constant marker byte with the sign
// bit, and four significant digits nibble-packed. It was derived
// empirically by cross-referencing every numeric-tagged .MDX
// specimen against its paired .DBF's actual field text, and
// verified against all 44 available keys with zero exceptions.
// See mdx/numeric.go and docs/RESEARCH_NOTES.md for the full
// derivation and its stated limits — values needing more than 4
// significant digits are refused rather than silently rounded.
package mdx

import (
	"errors"
	"io"
)

const (
	pageSize     = 512
	tagTableOff  = 544
	tagEntrySize = 32
	maxTags      = 48
)

var (
	ErrInvalidHeader      = errors.New("mdx: invalid header")
	ErrCorrupt            = errors.New("mdx: corrupt structure")
	ErrTagNotFound        = errors.New("mdx: tag not found")
	ErrTooManyKeys        = errors.New("mdx: too many entries for a single-leaf tag")
	ErrNumericUnsupported = errors.New("mdx: value needs more than 4 significant digits; encoding unverified beyond that")
)

// KeyType codes, from the tag table / tag header.
const (
	KeyCharacter = 'C'
	KeyNumeric   = 'N'
	KeyDate      = 'D'
)

// TagEntry is one key/record pair.
type TagEntry struct {
	Key   []byte
	RecNo uint32
}

// Tag describes one index within the MDX file.
type Tag struct {
	Name     string
	KeyType  byte
	KeySize  uint16 // ikl: index key length
	ItemSize uint16 // stride: 4 (recno) + KeySize, rounded up to a
	// multiple of 4 — found empirically; the file
	// header does not state the rounding rule.
	Unique     bool
	headerPage uint32 // byte-offset/512, i.e. a page number (unlike idx's byte-offset root)
}

// File is an open .MDX file.
type File struct {
	rw       io.ReadWriteSeeker
	dataFile string
	tags     []Tag
	pages    uint32 // total pages, for appending new tags
}

// DataFile returns the associated table name (no extension), as
// recorded in the header.
func (f *File) DataFile() string { return f.dataFile }

// Tags lists the tags in directory order.
func (f *File) Tags() []Tag { return f.tags }

// Tag returns the named tag's descriptor.
func (f *File) Tag(name string) (Tag, bool) {
	for _, t := range f.tags {
		if t.Name == name {
			return t, true
		}
	}
	return Tag{}, false
}

func align4(n int) int { return (n + 3) &^ 3 }
