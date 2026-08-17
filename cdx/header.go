package cdx

import (
	"encoding/binary"
	"fmt"
	"io"
)

// header describes the block-0 header of a compact/compound index.
// Only fields blipper cares about are extracted; the many "reserved
// for internal use" byte ranges are left alone.
type header struct {
	rootOffset int32
	freeList   int32
	keyLen     uint16
	options    uint8
	signature  uint8
	descending bool
	keyExpr    string
	forExpr    string
}

// hasFlag reports whether the given option bit is set.
func (h *header) hasFlag(flag uint8) bool { return h.options&flag != 0 }

// readHeader reads and validates the first 512-byte block.
//
// The collation gate lives here: after v0.3.0 blipper reads only
// MACHINE-collation CDX files. The compact-index spec (aa975346)
// does not name a dedicated collation byte, and DBFCDX.LIB writes
// none of the VFP-added named collations. Any non-zero signature
// byte is treated as a collation marker of unknown provenance and
// refused rather than opened.
func readHeader(rw io.ReadSeeker) (*header, error) {
	if _, err := rw.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("cdx: seek to header: %w", err)
	}

	var raw [BlockSize]byte
	if _, err := io.ReadFull(rw, raw[:]); err != nil {
		return nil, fmt.Errorf("cdx: read header: %w", err)
	}

	h := &header{
		rootOffset: int32(binary.LittleEndian.Uint32(raw[0:4])),
		freeList:   int32(binary.LittleEndian.Uint32(raw[4:8])),
		keyLen:     binary.LittleEndian.Uint16(raw[12:14]),
		options:    raw[14],
		signature:  raw[15],
		descending: binary.LittleEndian.Uint16(raw[502:504]) != orderAscending,
	}

	// The compound header is the marker that distinguishes a .CDX
	// from a stand-alone .IDX; without it we refuse to proceed.
	if !h.hasFlag(optCompoundHdr) {
		return nil, ErrNotCDX
	}
	if !h.hasFlag(optCompact) {
		// Every compound index is compact per aa975347 §1, so the
		// absence of the compact bit alongside compound-header is
		// malformed rather than a supported legacy shape.
		return nil, ErrNotCDX
	}

	// Signature is documented "for future use" and is 0x00 on
	// everything DBFCDX.LIB writes. Anything else is refused as
	// potentially collation-carrying.
	if h.signature != 0 {
		return nil, ErrUnsupportedCollation
	}

	// Key- and FOR-expression pools are variable-length strings
	// stored at offsets 510 and 506 respectively (length words).
	// The tag directory (top-level header) uses these pools as
	// empty single-byte entries; per-tag headers carry the real
	// expression text starting at offset 512.
	keyLen := int(binary.LittleEndian.Uint16(raw[510:512]))
	forLen := int(binary.LittleEndian.Uint16(raw[506:508]))
	if keyLen < 0 || keyLen > BlockSize-2 {
		return nil, ErrMalformedNode
	}
	if forLen < 0 || forLen > BlockSize-2 {
		return nil, ErrMalformedNode
	}
	// The pool byte range is fixed to bytes 512 onwards in the
	// file, i.e. the block following the header. It only carries
	// meaningful content in per-tag headers, so read directly and
	// let empty pools return "".
	if keyLen > 0 {
		var pool [BlockSize]byte
		if _, err := io.ReadFull(rw, pool[:]); err != nil {
			return nil, fmt.Errorf("cdx: read expression pool: %w", err)
		}
		if keyLen > len(pool) {
			return nil, ErrMalformedNode
		}
		h.keyExpr = trimNul(pool[:keyLen])
		if forLen > 0 && forLen+keyLen <= len(pool) {
			h.forExpr = trimNul(pool[keyLen : keyLen+forLen])
		}
	}

	return h, nil
}

// trimNul returns the input up to the first NUL byte, converted to
// string. CDX pools are padded with NULs.
func trimNul(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
