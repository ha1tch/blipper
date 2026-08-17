// Package idx implements FoxPro-compatible .IDX index files.
//
// blipper implements the *compact* IDX layout, not the plain
// uncompressed one also described in docs/INDEX_FORMATS.md. This
// was a scope decision forced by the oracle: the only available
// generator, Clipper's DBFCDX driver, produces compact-format
// .IDX files (index-options byte 0x20) even for a plain
// INDEX ON with no compound tag. No generator for the
// uncompressed layout was found — see docs/RESEARCH_NOTES.md.
//
// The compact leaf layout is identical to CDX's: bit-packed
// record-number / duplicate-count / trailing-count triples,
// growing forward from offset 24, with key text packed backward
// from the page end. This package reuses cdx.WriteLeaf for
// encoding and its own decoder (mirroring cdx's decodeExterior)
// for reading, since CDX's decoder is coupled to CDX's internal
// node type.
//
// Scope, matching cdx's own Phase 1: single-leaf trees only (root
// + one leaf page, 512 bytes). DBFCDX-generated single-tag
// indexes over small tables fit this by construction. Multi-page
// IDX is not implemented.
package idx

import (
	"errors"
	"io"

	"github.com/ha1tch/blipper/cdx"
)

// PageSize is the fixed page size, shared with CDX.
const PageSize = cdx.BlockSize // 512

// Index-options bits, from docs/INDEX_FORMATS.md.
const (
	OptUnique  = 0x01
	OptFor     = 0x08
	OptCompact = 0x20 // the layout this package implements
)

// Errors returned by this package.
var (
	ErrInvalidHeader = errors.New("idx: invalid index header")
	ErrCorrupt       = errors.New("idx: corrupt index structure")
	ErrNotCompact    = errors.New("idx: not a compact-format .IDX (index-options bit 0x20 not set); the uncompressed layout is unimplemented")
	ErrTooManyKeys   = errors.New("idx: too many entries for a single-leaf index")
	ErrKeySize       = errors.New("idx: key length does not match index")
)

// Entry reuses cdx's shape: a key and the record number it points
// at. Kept as an alias rather than a new type since the compact
// leaf encoder/decoder is shared between the two formats.
type Entry = cdx.Entry

// Options describes a new index.
type Options struct {
	// KeyExpr is stored in the header for documentation; not
	// evaluated by this package.
	KeyExpr string

	// KeySize is the fixed key length in bytes.
	KeySize uint16

	// Unique rejects duplicate keys, keeping the first.
	Unique bool
}

// Index represents an open compact .IDX file.
type Index struct {
	rw io.ReadWriteSeeker

	keyExpr string
	keySize uint16
	opts    byte

	root uint32
	free uint32
	eof  uint32
}

// KeyExpr returns the key expression recorded in the header.
func (ix *Index) KeyExpr() string { return ix.keyExpr }

// KeySize returns the fixed key width in bytes.
func (ix *Index) KeySize() uint16 { return ix.keySize }

// Unique reports whether the index rejects duplicate keys.
func (ix *Index) Unique() bool { return ix.opts&OptUnique != 0 }
