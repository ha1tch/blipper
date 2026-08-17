package dbf

import (
	"encoding/binary"
	"fmt"
	"io"
)

// FPT is the FoxPro 2 memo file format. It differs from the
// dBASE III+ .DBT format in three ways that matter:
//
//   - The block size is configurable and stored in the header at
//     offset 6 as a big-endian uint16. FoxPro's own default is 512;
//     Clipper 5.2e's DBFCDX driver defaults to 64.
//   - Each memo entry begins with an 8-byte header carrying the
//     memo type (big-endian uint32 at offset 0) and the exact
//     content length in bytes (big-endian uint32 at offset 4). No
//     0x1A terminator scan is needed — the length is explicit.
//   - The header's next-free-block field is a big-endian uint32
//     at offset 0. The rest of the 512-byte header block after
//     offset 8 is unused and left zero.
//
// The version byte in an FPT-bearing DBF is 0xF5. Blipper handles
// the version-byte dispatch at the caller's level: dbf.Open
// accepts 0xF5 tables, and the caller opens either DBT via
// OpenMemo or FPT via OpenFPT depending on that byte.
//
// Format reference: MS Learn aa975374 and the Apollo API
// documentation. Oracle: Clipper 5.2e's DBFCDX.LIB, which writes
// FoxPro-compatible FPT natively (per the Clipper 5.x Drivers
// Guide chapter 4). Same toolchain as CDX.

const (
	// FPTHeaderSize is the fixed size of an FPT header block. The
	// header always fits in exactly one 512-byte page regardless
	// of the file's own block size (a 32-byte-block FPT still has
	// a 512-byte header page — offset 0 of the first memo entry
	// is 512, not 32).
	FPTHeaderSize = 512

	// FPTDefaultBlockSize is Clipper DBFCDX's default block size.
	// Matches what the oracle produces byte-for-byte for small
	// files. FoxPro's own default is 512; callers who want that
	// pass 512 to CreateFPT explicitly.
	FPTDefaultBlockSize = 64

	// FPTMinBlockSize matches FoxPro's own lower bound. Values
	// below this are refused on Create and rejected on Open as
	// malformed.
	FPTMinBlockSize = 32

	// FPTMaxBlockSize matches FoxPro's own upper bound.
	FPTMaxBlockSize = 1024

	// dbfVersionFPT is the version-byte value that marks an
	// FPT-bearing table. Distinct from dbfVersionMemo (0x83),
	// which marks a DBT-bearing dBASE III+ table.
	dbfVersionFPT = 0xF5
)

// MemoFormat identifies which memo file format a Table expects.
// Returned by Table.MemoFormat so callers can dispatch to the
// right constructor (OpenMemo for DBT, OpenFPT for FPT).
type MemoFormat int

const (
	// MemoFormatNone means the table has no accompanying memo
	// file: the version byte is 0x03 (dBASE III+ without memo).
	MemoFormatNone MemoFormat = iota

	// MemoFormatDBT means the table expects a sibling .DBT
	// (dBASE III+ format, 0x83 version byte). Open via OpenMemo.
	MemoFormatDBT

	// MemoFormatFPT means the table expects a sibling .FPT
	// (FoxPro format, 0xF5 version byte). Open via OpenFPT.
	MemoFormatFPT

	// MemoFormatDBaseIV means the table expects a sibling .DBT
	// in dBASE IV/5.0's own layout (0x8B version byte) — a
	// genuinely different physical format from MemoFormatDBT
	// despite sharing the .DBT extension. dBASE III+'s blocks
	// are headerless; dBASE IV/5's carry an 8-byte per-block
	// header (a constant 4-byte marker, then a 4-byte
	// little-endian length field that is header-inclusive, not
	// content-only) — found via a live write-oracle, source S13,
	// docs/DBASE_FORMAT.md. Open via OpenDBaseIVMemo, not
	// OpenMemo: the latter would silently misread these blocks,
	// scanning the 8 header bytes as if they were the start of
	// text.
	MemoFormatDBaseIV
)

// String returns a short human-readable form of the format.
func (f MemoFormat) String() string {
	switch f {
	case MemoFormatDBT:
		return "DBT"
	case MemoFormatFPT:
		return "FPT"
	case MemoFormatDBaseIV:
		return "DBT (dBASE IV/5.0)"
	default:
		return "none"
	}
}

// MemoType enumerates the memo content types FPT supports.
// FoxPro reserves values 0-2; higher values are undefined and
// blipper preserves them on read as-is so a caller can distinguish
// unknown-type-from-a-real-file from missing.
type MemoType uint32

const (
	// MemoPicture holds picture data (binary).
	MemoPicture MemoType = 0

	// MemoText holds text data. Most .FPT memos are of this type.
	MemoText MemoType = 1

	// MemoObject holds OLE object data (binary).
	MemoObject MemoType = 2
)

// FPTFile is an open FoxPro-format memo file.
//
// Like MemoFile and Table, FPTFile operates on a stream it does
// not own. It carries the block size read from the header and
// tracks the next free block for appends.
type FPTFile struct {
	rw io.ReadWriteSeeker

	blockSize uint16
	nextFree  uint32
}

// OpenFPT reads the header of an existing FPT file.
func OpenFPT(rw io.ReadWriteSeeker) (*FPTFile, error) {
	if _, err := rw.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	var head [FPTHeaderSize]byte
	if _, err := io.ReadFull(rw, head[:]); err != nil {
		return nil, fmt.Errorf("reading FPT header: %w", err)
	}

	next := binary.BigEndian.Uint32(head[0:4])
	blockSize := binary.BigEndian.Uint16(head[6:8])

	if blockSize < FPTMinBlockSize || blockSize > FPTMaxBlockSize {
		return nil, fmt.Errorf("%w: FPT block size %d out of range [%d..%d]",
			ErrInvalidRecord, blockSize, FPTMinBlockSize, FPTMaxBlockSize)
	}
	if blockSize%32 != 0 {
		// FoxPro constrains block size to a multiple of 32.
		return nil, fmt.Errorf("%w: FPT block size %d not a multiple of 32",
			ErrInvalidRecord, blockSize)
	}
	// The first usable memo block is FPTHeaderSize/blockSize; a
	// next-free value below that would point inside the header
	// block and is corrupt.
	firstUsable := uint32(FPTHeaderSize) / uint32(blockSize)
	if next < firstUsable {
		return nil, fmt.Errorf("%w: FPT next-free %d points inside header (first usable = %d)",
			ErrInvalidRecord, next, firstUsable)
	}

	return &FPTFile{rw: rw, blockSize: blockSize, nextFree: next}, nil
}

// CreateFPT writes a new, empty FPT file with the given block
// size to rw. Passing 0 uses FPTDefaultBlockSize (64).
func CreateFPT(rw io.ReadWriteSeeker, blockSize uint16) (*FPTFile, error) {
	if blockSize == 0 {
		blockSize = FPTDefaultBlockSize
	}
	if blockSize < FPTMinBlockSize || blockSize > FPTMaxBlockSize {
		return nil, fmt.Errorf("FPT block size %d out of range [%d..%d]",
			blockSize, FPTMinBlockSize, FPTMaxBlockSize)
	}
	if blockSize%32 != 0 {
		return nil, fmt.Errorf("FPT block size %d not a multiple of 32", blockSize)
	}

	if _, err := rw.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	// The header block is always 512 bytes regardless of the
	// file's block size. Block 1 (the first usable memo block)
	// begins at byte offset 512.
	firstUsable := uint32(FPTHeaderSize) / uint32(blockSize)
	block := make([]byte, FPTHeaderSize)
	binary.BigEndian.PutUint32(block[0:4], firstUsable)
	binary.BigEndian.PutUint16(block[6:8], blockSize)

	if _, err := rw.Write(block); err != nil {
		return nil, fmt.Errorf("writing FPT header: %w", err)
	}

	return &FPTFile{rw: rw, blockSize: blockSize, nextFree: firstUsable}, nil
}

// BlockSize returns the FPT file's block size.
func (f *FPTFile) BlockSize() uint16 { return f.blockSize }

// NextFree returns the block number at which the next memo will
// be appended.
func (f *FPTFile) NextFree() uint32 { return f.nextFree }

// blockOffset converts a memo block number to its byte offset
// in the file.
//
// FoxPro's memo pointer is the block number counted in block-size
// units from byte 0 of the file — not the N-th memo block after
// the header. The header occupies exactly FPTHeaderSize (512)
// bytes, so the first usable memo block is FPTHeaderSize /
// blockSize: block 1 for a 512-byte-block file, block 8 for a
// 64-byte-block file (Clipper DBFCDX's default). This is verified
// against a Clipper-generated FPT where a small memo landed at
// block 8, not block 1.
//
// firstMemoBlock returns that boundary.
func (f *FPTFile) blockOffset(block uint32) int64 {
	return int64(block) * int64(f.blockSize)
}

// firstMemoBlock returns the block number of the first usable
// memo block, i.e. FPTHeaderSize / blockSize.
func (f *FPTFile) firstMemoBlock() uint32 {
	return uint32(FPTHeaderSize) / uint32(f.blockSize)
}

// Get returns the content and type of the memo starting at the
// given block. Block 0 is not a valid memo pointer; callers
// should use ParseMemoPointer to distinguish absent from present.
func (f *FPTFile) Get(block uint32) ([]byte, MemoType, error) {
	if block == 0 {
		return nil, 0, fmt.Errorf("%w: block 0 is not a valid memo pointer", ErrInvalidRecord)
	}

	off := f.blockOffset(block)
	if _, err := f.rw.Seek(off, io.SeekStart); err != nil {
		return nil, 0, err
	}

	var head [8]byte
	if _, err := io.ReadFull(f.rw, head[:]); err != nil {
		return nil, 0, fmt.Errorf("reading FPT entry header at block %d: %w", block, err)
	}

	memoType := MemoType(binary.BigEndian.Uint32(head[0:4]))
	length := binary.BigEndian.Uint32(head[4:8])

	if length == 0 {
		return []byte{}, memoType, nil
	}

	buf := make([]byte, length)
	if _, err := io.ReadFull(f.rw, buf); err != nil {
		return nil, 0, fmt.Errorf("reading FPT content at block %d: %w", block, err)
	}
	return buf, memoType, nil
}

// Append writes a memo of the given type to the next free block,
// updates the header's next-free counter, and returns the block
// number the caller should store in the DBF record.
//
// Storage rounds up to a whole number of blocks. Overwriting an
// existing memo is not supported by this API: callers who need
// to update a memo should Append a new one and rewrite the DBF
// record's memo pointer; the old blocks become orphaned. FoxPro
// itself behaves this way, and compaction is a PACK-scoped
// operation (T-03 territory).
func (f *FPTFile) Append(content []byte, memoType MemoType) (uint32, error) {
	entryLen := 8 + len(content)
	blocksNeeded := uint32((entryLen + int(f.blockSize) - 1) / int(f.blockSize))
	if blocksNeeded == 0 {
		// Zero-length memo still consumes one block for its
		// header.
		blocksNeeded = 1
	}

	block := f.nextFree
	off := f.blockOffset(block)

	buf := make([]byte, int(blocksNeeded)*int(f.blockSize))
	binary.BigEndian.PutUint32(buf[0:4], uint32(memoType))
	binary.BigEndian.PutUint32(buf[4:8], uint32(len(content)))
	copy(buf[8:], content)

	if _, err := f.rw.Seek(off, io.SeekStart); err != nil {
		return 0, err
	}
	if _, err := f.rw.Write(buf); err != nil {
		return 0, fmt.Errorf("writing FPT memo at block %d: %w", block, err)
	}

	f.nextFree += blocksNeeded
	if err := f.writeHeader(); err != nil {
		return 0, err
	}
	return block, nil
}

// writeHeader updates the header block's next-free field.
func (f *FPTFile) writeHeader() error {
	var head [8]byte
	binary.BigEndian.PutUint32(head[0:4], f.nextFree)
	binary.BigEndian.PutUint16(head[6:8], f.blockSize)
	if _, err := f.rw.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := f.rw.Write(head[:]); err != nil {
		return fmt.Errorf("updating FPT header: %w", err)
	}
	return nil
}
