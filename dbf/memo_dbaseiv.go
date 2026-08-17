package dbf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// dBASE IV/5.0's own memo file format — physically distinct from
// dBASE III+'s (dbf/memo.go), despite sharing the .DBT extension.
// Accompanies a table whose version byte is 0x8B (dbfVersionDBaseIV).
//
// Found via a live write-oracle (source S13, docs/DBASE_FORMAT.md:
// real dBASE 5.0 for DOS, run by the project's user under DOSBox)
// and cross-checked against three real 1994 vendor specimens —
// CLIENT.DBT, CONTENTS.DBT, ORDERS.DBT at dbf/testdata/dbase5/full/
// — all four files agreeing on every field below exactly.
//
// Block 0 is the header, and unlike dBASE III+'s, it is
// self-describing:
//
//	offset  size  contents
//	0-3     4     next free block, little-endian uint32
//	4-7     4     reserved, zero in every specimen checked
//	8-15    8     the table's own name, NUL-padded/terminated,
//	              up to 8 characters — DOS 8.3 convention
//	16-19   4     unidentified; constant "00 00 02 01" across
//	              every specimen checked, not decoded further
//	20-21   2     block size, little-endian uint16 — 512 in every
//	              specimen seen; S12 (Borland's own confidential
//	              manuscript) states SET BLOCKSIZE can change this,
//	              though no non-default specimen has been found to
//	              confirm variable-size behaviour is read correctly
//	22-511  —     unidentified, not decoded
//
// Every subsequent block carries its own 8-byte header:
//
//	offset  size  contents
//	0-3     4     constant marker, observed as bytes FF FF 08 00
//	              in every block checked
//	4-7     4     length, little-endian uint32 — header-inclusive
//	              (8 + text length), not content-only like FPT's
//	8-      —     text, NUL-padded to fill the rest of the block
//
// Multi-block memos (text longer than one block minus 8 bytes)
// are verified, not just assumed: CLIENT.DBT's third record
// carries a 580-byte memo — longer than one 512-byte block's
// ~504 usable bytes — and decodes correctly, the header appearing
// once at the start with text continuing seamlessly across the
// block boundary. The same convention FPT uses.
//
// Write support (T-37, added after T-31's initial read-only
// release) is CreateDBaseIVMemo/Append below. Untested against
// real dBASE 5.0 re-opening a file blipper wrote — no such round
// trip has been attempted, so treat this as internally consistent
// (round-trips through blipper's own reader correctly) rather
// than confirmed compatible with the original product.

const (
	// dbaseIVMemoHeaderPrefixSize is how many header bytes are
	// read unconditionally at Open, before the block size (which
	// the rest of the file's layout depends on) is even known.
	dbaseIVMemoHeaderPrefixSize = 22

	// dbaseIVBlockHeaderSize is the fixed 8-byte per-block header
	// present on every block after block 0.
	dbaseIVBlockHeaderSize = 8
)

// dbaseIVBlockMarker is the constant 4 bytes opening every memo
// block after block 0. Never observed to vary across four
// independent files (three real, one write-oracle).
var dbaseIVBlockMarker = [4]byte{0xFF, 0xFF, 0x08, 0x00}

// dbaseIVMemoTableNameSize is the width of the table-name field
// in a dBASE IV/5.0 memo header — 8 bytes (DOS 8.3 convention),
// distinct from MaxFieldNameLength (11), which governs DBF field
// descriptors and would be the wrong size here.
const dbaseIVMemoTableNameSize = 8

// encodeDBaseIVMemoTableName writes name into dst, truncated if
// longer than 8 characters and NUL-padded if shorter.
func encodeDBaseIVMemoTableName(dst []byte, name string) {
	if len(dst) != dbaseIVMemoTableNameSize {
		panic("encodeDBaseIVMemoTableName: destination must be exactly 8 bytes")
	}
	clear(dst)
	copy(dst, name)
}

// DBaseIVMemoFile is an open dBASE IV/5.0 .DBT memo file.
//
// Like MemoFile, it operates on a stream it does not own.
type DBaseIVMemoFile struct {
	rw io.ReadWriteSeeker

	// nextFree mirrors the header field, for callers that want
	// to inspect it; blipper does not currently write this format.
	nextFree uint32

	// blockSize is read from the file's own header (offset
	// 20-21) rather than assumed, since SET BLOCKSIZE can vary
	// it — even though no specimen with a non-default value has
	// been found to confirm this path is exercised correctly.
	blockSize uint16

	// tableName is the 8-character name recorded in the header,
	// trimmed of NUL padding. Informational; not validated
	// against the sibling .DBF's own name.
	tableName string
}

// OpenDBaseIVMemo reads the header of an existing dBASE IV/5.0
// memo file.
func OpenDBaseIVMemo(rw io.ReadWriteSeeker) (*DBaseIVMemoFile, error) {
	if _, err := rw.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	var head [dbaseIVMemoHeaderPrefixSize]byte
	if _, err := io.ReadFull(rw, head[:]); err != nil {
		return nil, fmt.Errorf("reading dBASE IV memo header: %w", err)
	}

	next := binary.LittleEndian.Uint32(head[0:4])
	if next == 0 {
		return nil, fmt.Errorf("%w: memo header claims block 0 is free", ErrInvalidRecord)
	}

	blockSize := binary.LittleEndian.Uint16(head[20:22])
	if blockSize == 0 {
		return nil, fmt.Errorf("%w: memo header declares a zero block size", ErrInvalidRecord)
	}

	name := decodeFieldName(head[8:16])

	return &DBaseIVMemoFile{
		rw:        rw,
		nextFree:  next,
		blockSize: blockSize,
		tableName: name,
	}, nil
}

// NextFree returns the block number at which the next memo would
// be written. Informational: this package does not currently
// write this format.
func (m *DBaseIVMemoFile) NextFree() uint32 { return m.nextFree }

// BlockSize returns the block size read from the file's own
// header.
func (m *DBaseIVMemoFile) BlockSize() uint16 { return m.blockSize }

// TableName returns the 8-character table name recorded in the
// file's header, trimmed of padding.
func (m *DBaseIVMemoFile) TableName() string { return m.tableName }

// CreateDBaseIVMemo writes a new, empty dBASE IV/5.0-format
// memo file to rw.
//
// tableName is recorded in the header verbatim, truncated to 8
// characters if longer — callers should pass the sibling .DBF's
// own base name, matching what every real specimen checked this
// session does, though nothing in the reader enforces this.
// blockSize must be a positive multiple of 8 (room for at least
// the per-block header); 512 matches every specimen decoded this
// session, since no non-default SET BLOCKSIZE file has been
// found to confirm behaviour at another size.
//
// Bytes 16-19 of the header are written as the constant pattern
// observed identically across all four real files checked this
// session (the write-oracle and three 1994 vendor specimens) —
// their meaning is unidentified, but reproducing the pattern real
// dBASE 5.0 writes is the safer choice than zeroing an unknown
// field. Bytes 22 onward, genuinely unexamined beyond confirming
// they exist, are written as zero.
//
// Untested against real dBASE 5.0 re-opening a file blipper wrote
// in this format — no such round trip has been attempted. A
// caller relying on that compatibility should verify it directly
// before depending on it.
func CreateDBaseIVMemo(rw io.ReadWriteSeeker, tableName string, blockSize uint16) (*DBaseIVMemoFile, error) {
	if blockSize == 0 || blockSize%8 != 0 {
		return nil, fmt.Errorf("%w: block size %d must be a positive multiple of 8", ErrInvalidRecord, blockSize)
	}

	if _, err := rw.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	block := make([]byte, blockSize)
	binary.LittleEndian.PutUint32(block[0:4], 1)
	// bytes 4-7: reserved, zero in every specimen checked.
	encodeDBaseIVMemoTableName(block[8:16], tableName)
	copy(block[16:20], []byte{0x00, 0x00, 0x02, 0x01})
	binary.LittleEndian.PutUint16(block[20:22], blockSize)
	// bytes 22-... reserved/unidentified, left zero.

	if _, err := rw.Write(block); err != nil {
		return nil, fmt.Errorf("writing dBASE IV memo header: %w", err)
	}

	return &DBaseIVMemoFile{
		rw:        rw,
		nextFree:  1,
		blockSize: blockSize,
		tableName: decodeFieldName(block[8:16]),
	}, nil
}

// Append writes text as a new memo and returns its starting block.
//
// The 8-byte per-block header (constant marker, then a
// header-inclusive length) is written once at the memo's start;
// text continues across as many blocks as needed with no repeated
// header, matching the continuation behaviour verified on read
// (T-31's TestDBaseIVMemoRealSpecimenMultiBlock, a real 1994
// memo spanning a block boundary).
//
// Like MemoFile.Append, blocks freed by a rewrite are not reused —
// callers needing compaction should rebuild the file.
func (m *DBaseIVMemoFile) Append(text []byte) (uint32, error) {
	block := m.nextFree
	offset := int64(block) * int64(m.blockSize)

	if _, err := m.rw.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}

	totalLen := dbaseIVBlockHeaderSize + len(text)
	blocks := (totalLen + int(m.blockSize) - 1) / int(m.blockSize)

	buf := make([]byte, blocks*int(m.blockSize))
	copy(buf[0:4], dbaseIVBlockMarker[:])
	binary.LittleEndian.PutUint32(buf[4:8], uint32(totalLen))
	copy(buf[dbaseIVBlockHeaderSize:], text)

	if _, err := m.rw.Write(buf); err != nil {
		return 0, err
	}

	m.nextFree = block + uint32(blocks)

	if err := m.writeHeader(); err != nil {
		return 0, err
	}

	return block, nil
}

// writeHeader rewrites only the next-free-block field (bytes
// 0-3), leaving the rest of block 0 — table name, block size, and
// the unidentified constant at bytes 16-19 — untouched.
func (m *DBaseIVMemoFile) writeHeader() error {
	if _, err := m.rw.Seek(0, io.SeekStart); err != nil {
		return err
	}

	var head [4]byte
	binary.LittleEndian.PutUint32(head[:], m.nextFree)

	_, err := m.rw.Write(head[:])
	return err
}

// Block 0 is not a valid memo pointer; callers should use
// ParseMemoPointer first, which reports an absent memo separately
// — the pointer field convention is identical to dBASE III+'s.
func (m *DBaseIVMemoFile) Get(block uint32) ([]byte, error) {
	if block == 0 {
		return nil, fmt.Errorf("%w: block 0 is the memo header", ErrInvalidRecord)
	}

	offset := int64(block) * int64(m.blockSize)
	if _, err := m.rw.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}

	var blockHead [dbaseIVBlockHeaderSize]byte
	if _, err := io.ReadFull(m.rw, blockHead[:]); err != nil {
		return nil, fmt.Errorf("%w: reading block %d header: %v", ErrInvalidRecord, block, err)
	}

	if !bytes.Equal(blockHead[:4], dbaseIVBlockMarker[:]) {
		return nil, fmt.Errorf(
			"%w: block %d marker = % X, want % X",
			ErrInvalidRecord, block, blockHead[:4], dbaseIVBlockMarker,
		)
	}

	totalLen := binary.LittleEndian.Uint32(blockHead[4:8])
	if totalLen < dbaseIVBlockHeaderSize {
		return nil, fmt.Errorf(
			"%w: block %d declares length %d, less than its own 8-byte header",
			ErrInvalidRecord, block, totalLen,
		)
	}

	textLen := totalLen - dbaseIVBlockHeaderSize
	text := make([]byte, textLen)
	if _, err := io.ReadFull(m.rw, text); err != nil {
		return nil, fmt.Errorf("reading block %d text (%d bytes): %w", block, textLen, err)
	}

	return text, nil
}
