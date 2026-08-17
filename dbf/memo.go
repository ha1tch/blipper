package dbf

import (
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
)

// dBASE III+ memo files (.DBT) accompany a table whose header version
// byte is 0x83. The layout, verified against Clipper 5.2e output (see
// docs/CLIPPER_ORACLE.md):
//
//   - The file is a sequence of 512-byte blocks.
//   - Block 0 is the header. Its first four bytes hold the number of
//     the next free block as a little-endian uint32.
//   - A memo occupies one or more consecutive blocks starting at the
//     block named by the record's memo field, and its text ends with
//     the two-byte terminator 0x1A 0x1A.
//   - The memo field in the DBF record holds the starting block
//     number as right-aligned ASCII in 10 bytes, or all spaces when
//     the memo is empty. Block 0 is the header and is never used as a
//     memo pointer, so 0 does not denote an empty memo; spaces do.
//
// Two details bite readers that assume otherwise. Memo text may
// itself contain 0x1A bytes, so the terminator is found by scanning
// for the pair, not the first occurrence. And in the final block of a
// file the second terminator byte may fall beyond end of file, so a
// lone trailing 0x1A at the end of the data must be accepted.

const (
	// MemoBlockSize is the fixed block size of a dBASE III+ memo file.
	MemoBlockSize = 512

	// memoFieldWidth is the width of a memo pointer in a DBF record.
	memoFieldWidth = 10
)

// MemoFile is an open .DBT memo file.
//
// Like Table, a MemoFile operates on a stream it does not own: it
// never opens or closes the underlying file.
type MemoFile struct {
	rw io.ReadWriteSeeker

	// nextFree is the block number at which the next memo will be
	// written, mirroring the header field.
	nextFree uint32
}

// OpenMemo reads the header of an existing memo file.
func OpenMemo(rw io.ReadWriteSeeker) (*MemoFile, error) {
	if _, err := rw.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	var head [4]byte

	if _, err := io.ReadFull(rw, head[:]); err != nil {
		return nil, fmt.Errorf("reading memo header: %w", err)
	}

	next := binary.LittleEndian.Uint32(head[:])

	// Block 0 is the header, so the first usable block is 1. A file
	// claiming otherwise is corrupt.
	if next == 0 {
		return nil, fmt.Errorf("%w: memo header claims block 0 is free", ErrInvalidRecord)
	}

	return &MemoFile{rw: rw, nextFree: next}, nil
}

// CreateMemo writes a new, empty memo file to rw.
func CreateMemo(rw io.ReadWriteSeeker) (*MemoFile, error) {
	if _, err := rw.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	block := make([]byte, MemoBlockSize)
	binary.LittleEndian.PutUint32(block[:4], 1)

	if _, err := rw.Write(block); err != nil {
		return nil, fmt.Errorf("writing memo header: %w", err)
	}

	return &MemoFile{rw: rw, nextFree: 1}, nil
}

// NextFree returns the block number at which the next memo will be
// written.
func (m *MemoFile) NextFree() uint32 {
	return m.nextFree
}

// Get returns the text of the memo starting at the given block.
//
// Block 0 is not a valid memo pointer; callers should use
// ParseMemoPointer, which reports an absent memo separately.
func (m *MemoFile) Get(block uint32) ([]byte, error) {
	if block == 0 {
		return nil, fmt.Errorf("%w: block 0 is the memo header", ErrInvalidRecord)
	}

	offset := int64(block) * MemoBlockSize

	if _, err := m.rw.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}

	var (
		text  []byte
		chunk = make([]byte, MemoBlockSize)
	)

	for {
		n, err := io.ReadFull(m.rw, chunk)

		// A short read is only acceptable at end of file, where the
		// terminator may be truncated.
		if err == io.ErrUnexpectedEOF || err == io.EOF {
			if n == 0 && len(text) == 0 {
				return nil, fmt.Errorf("%w: memo block %d is past end of file",
					ErrEOF, block)
			}

			text = append(text, chunk[:n]...)

			return trimMemoTerminator(text), nil
		}

		if err != nil {
			return nil, err
		}

		text = append(text, chunk...)

		if end := findMemoTerminator(text); end >= 0 {
			return text[:end], nil
		}
	}
}

// Append writes text as a new memo and returns its starting block.
//
// Memos are appended; blocks freed by a rewrite are not reused, which
// matches the simplest Clipper behaviour and keeps existing pointers
// valid. Callers that need compaction should rebuild the file.
func (m *MemoFile) Append(text []byte) (uint32, error) {
	block := m.nextFree
	offset := int64(block) * MemoBlockSize

	if _, err := m.rw.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}

	// Text plus the two-byte terminator, rounded up to whole blocks.
	size := len(text) + 2
	blocks := (size + MemoBlockSize - 1) / MemoBlockSize

	buf := make([]byte, blocks*MemoBlockSize)

	copy(buf, text)
	buf[len(text)] = 0x1A
	buf[len(text)+1] = 0x1A

	if _, err := m.rw.Write(buf); err != nil {
		return 0, err
	}

	m.nextFree = block + uint32(blocks)

	if err := m.writeHeader(); err != nil {
		return 0, err
	}

	return block, nil
}

func (m *MemoFile) writeHeader() error {
	if _, err := m.rw.Seek(0, io.SeekStart); err != nil {
		return err
	}

	var head [4]byte
	binary.LittleEndian.PutUint32(head[:], m.nextFree)

	_, err := m.rw.Write(head[:])

	return err
}

// ParseMemoPointer reads the block number from a record's memo field.
// The second return value reports whether a memo is present: an
// all-blank field means there is none.
func ParseMemoPointer(raw []byte) (uint32, bool, error) {
	text := trimSpaces(raw)

	if text == "" {
		return 0, false, nil
	}

	block, err := strconv.ParseUint(text, 10, 32)
	if err != nil {
		return 0, false, fmt.Errorf("%w: memo pointer %q", ErrInvalidRecord, text)
	}

	if block == 0 {
		return 0, false, nil
	}

	return uint32(block), true, nil
}

// FormatMemoPointer renders a block number for storage in a record's
// memo field: right aligned and space padded. A block of 0 renders as
// blanks, denoting no memo.
func FormatMemoPointer(block uint32) []byte {
	out := make([]byte, memoFieldWidth)

	for i := range out {
		out[i] = ' '
	}

	if block == 0 {
		return out
	}

	text := strconv.FormatUint(uint64(block), 10)

	if len(text) > memoFieldWidth {
		// Unreachable for any file a uint32 block count can address,
		// but truncating silently would corrupt the record.
		return out
	}

	copy(out[memoFieldWidth-len(text):], text)

	return out
}

// findMemoTerminator returns the index of the 0x1A 0x1A pair, or -1.
func findMemoTerminator(text []byte) int {
	for i := 0; i+1 < len(text); i++ {
		if text[i] == 0x1A && text[i+1] == 0x1A {
			return i
		}
	}

	return -1
}

// trimMemoTerminator strips the terminator from data that ran to end
// of file, tolerating a single trailing 0x1A whose partner lies past
// the end.
func trimMemoTerminator(text []byte) []byte {
	if end := findMemoTerminator(text); end >= 0 {
		return text[:end]
	}

	if n := len(text); n > 0 && text[n-1] == 0x1A {
		return text[:n-1]
	}

	return text
}
