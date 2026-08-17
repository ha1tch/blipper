package ndx

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// Header layout, page 0 of an NDX file. All integers are stored
// little-endian.
//
//	offset  size  contents
//	 0– 3    4    starting (root) page number
//	 4– 7    4    total page count
//	 8–11    4    reserved
//	12–13    2    key length
//	14–15    2    keys per page
//	16–17    2    key type: 0 character, 1 numeric
//	18–21    4    size of one key record
//	22       1    reserved
//	23       1    unique flag
//	24–511   —    key expression, NUL-terminated
//
// Verified field by field against Clipper 5.2e DBFNDX output for
// both a character index (key length 10, record size 20, keys per
// page 25) and a numeric one (key length 8, record size 16, keys
// per page 31).
const (
	offRoot        = 0
	offPageCount   = 4
	offKeyLength   = 12
	offKeysPerPage = 14
	offKeyType     = 16
	offRecordSize  = 18
	offUnique      = 23
	offKeyExpr     = 24
)

// readHeader decodes the anchor node.
func readHeader(r io.ReadSeeker) (*Index, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("ndx: seek to header: %w", err)
	}
	var raw [PageSize]byte
	if _, err := io.ReadFull(r, raw[:]); err != nil {
		return nil, fmt.Errorf("ndx: read header: %w", err)
	}

	ix := &Index{
		root:        binary.LittleEndian.Uint32(raw[offRoot:]),
		pageCount:   binary.LittleEndian.Uint32(raw[offPageCount:]),
		keySize:     binary.LittleEndian.Uint16(raw[offKeyLength:]),
		keysPerPage: binary.LittleEndian.Uint16(raw[offKeysPerPage:]),
		keyType:     binary.LittleEndian.Uint16(raw[offKeyType:]),
		recordSize:  binary.LittleEndian.Uint32(raw[offRecordSize:]),
		unique:      raw[offUnique] != 0,
	}

	// The key expression is NUL-terminated within the remainder of
	// the page.
	tail := raw[offKeyExpr:]
	if end := bytes.IndexByte(tail, 0); end >= 0 {
		ix.keyExpr = string(tail[:end])
	} else {
		ix.keyExpr = string(tail)
	}

	if err := ix.validate(); err != nil {
		return nil, err
	}
	return ix, nil
}

// validate checks that a decoded header describes a usable index.
//
// The checks are deliberately specific rather than a single
// catch-all: a file that fails one of these is telling us
// something, and which one it fails is worth reporting.
func (ix *Index) validate() error {
	if ix.keySize == 0 {
		return fmt.Errorf("%w: key length is zero", ErrInvalidHeader)
	}
	if ix.keySize > MaxKeySize {
		return fmt.Errorf("%w: key length %d exceeds %d",
			ErrInvalidHeader, ix.keySize, MaxKeySize)
	}
	switch ix.keyType {
	case KeyTypeCharacter:
	case KeyTypeNumeric:
		// A numeric key is an IEEE double, so any other width
		// means the file disagrees with itself.
		if ix.keySize != 8 {
			return fmt.Errorf("%w: numeric key length is %d, want 8",
				ErrInvalidHeader, ix.keySize)
		}
	default:
		return fmt.Errorf("%w: unknown key type %d", ErrInvalidHeader, ix.keyType)
	}

	// The stored record size must agree with the sizing rule. A
	// mismatch means either a format we do not understand or a
	// corrupt header, and reading on would misinterpret every
	// entry.
	if want := keyRecordSize(ix.keySize); ix.recordSize != want {
		return fmt.Errorf("%w: record size %d, expected %d for key length %d",
			ErrInvalidHeader, ix.recordSize, want, ix.keySize)
	}
	if ix.keysPerPage == 0 {
		return fmt.Errorf("%w: keys per page is zero", ErrInvalidHeader)
	}
	if uint32(ix.keysPerPage)*ix.recordSize+4 > PageSize {
		return fmt.Errorf("%w: %d keys of %d bytes will not fit a %d-byte page",
			ErrInvalidHeader, ix.keysPerPage, ix.recordSize, PageSize)
	}
	return nil
}

// writeHeader encodes the anchor node.
func (ix *Index) writeHeader() error {
	var raw [PageSize]byte

	binary.LittleEndian.PutUint32(raw[offRoot:], ix.root)
	binary.LittleEndian.PutUint32(raw[offPageCount:], ix.pageCount)
	binary.LittleEndian.PutUint16(raw[offKeyLength:], ix.keySize)
	binary.LittleEndian.PutUint16(raw[offKeysPerPage:], ix.keysPerPage)
	binary.LittleEndian.PutUint16(raw[offKeyType:], ix.keyType)
	binary.LittleEndian.PutUint32(raw[offRecordSize:], ix.recordSize)
	if ix.unique {
		raw[offUnique] = 1
	}

	expr := ix.keyExpr
	if len(expr) > MaxKeyExpr-1 {
		expr = expr[:MaxKeyExpr-1]
	}
	copy(raw[offKeyExpr:], expr)

	if _, err := ix.rw.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("ndx: seek to header: %w", err)
	}
	if _, err := ix.rw.Write(raw[:]); err != nil {
		return fmt.Errorf("ndx: write header: %w", err)
	}
	return nil
}

// pageOffset returns the byte offset of a page.
func pageOffset(page uint32) int64 {
	return int64(page) * PageSize
}
