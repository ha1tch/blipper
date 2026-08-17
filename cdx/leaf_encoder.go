package cdx

import (
	"encoding/binary"
	"fmt"
)

// Fixed encoding parameters chosen to match Clipper's DBFCDX
// output for small trees. Callers who need larger record numbers
// or key widths will need a Writer with adaptive parameters,
// which is a future extension.
const (
	writeRecBits       = 16
	writeDupBits       = 4
	writeTrailBits     = 4
	writeBytesPerEntry = 3
	writeRecMask       = (1 << writeRecBits) - 1
	writeDupMask       = (1 << writeDupBits) - 1
	writeTrailMask     = (1 << writeTrailBits) - 1
)

// writeLeaf encodes entries into a single exterior/leaf node,
// filling `block` (exactly BlockSize bytes). It applies the same
// bit-packing and back-filled-key-text layout that the reader in
// node.go decodes.
//
// Constraints on Phase 1:
//   - single-leaf trees only (root + leaf, attr = 0x03)
//   - fixed encoding parameters (3 bytes per entry)
//   - up to writeRecMask (65535) as the maximum record number
//   - key length up to 15 (dup/trail fit in 4 bits each)
//
// If entries do not fit — either because there are too many for
// the fixed 24-byte header + 3-bytes-per-entry area, or because
// the packed key text would overrun the entries area — writeLeaf
// returns an error rather than truncate silently.
// WriteLeaf is the exported form of writeLeaf, for packages that
// need the same compact-leaf encoding CDX uses — currently idx,
// since FoxPro's compact .IDX format shares this exact leaf layout
// with CDX. See idx/leaf.go.
func WriteLeaf(block []byte, entries []Entry, keyLen uint16) error {
	return writeLeaf(block, entries, keyLen)
}

func writeLeaf(block []byte, entries []Entry, keyLen uint16) error {
	if len(block) != BlockSize {
		return fmt.Errorf("cdx: writeLeaf: block must be %d bytes", BlockSize)
	}
	if keyLen == 0 || keyLen > (writeDupMask) {
		return fmt.Errorf("cdx: writeLeaf: key length %d out of range (1..%d)",
			keyLen, writeDupMask)
	}

	for i := range block {
		block[i] = 0
	}

	// Node header:
	//   bytes 0-1  attr = root + leaf (0x03)
	//   bytes 2-3  nkeys
	//   bytes 4-7  left sibling  = -1
	//   bytes 8-11 right sibling = -1
	//   bytes 12-13 free space (patched at the end)
	//   bytes 14-17 recNo mask
	//   byte  18   dup mask
	//   byte  19   trail mask
	//   byte  20   recNo bit width
	//   byte  21   dup bit width
	//   byte  22   trail bit width
	//   byte  23   bytes per compressed entry
	binary.LittleEndian.PutUint16(block[0:2], nodeRoot|nodeLeaf)
	binary.LittleEndian.PutUint16(block[2:4], uint16(len(entries)))
	binary.LittleEndian.PutUint32(block[4:8], 0xFFFFFFFF)
	binary.LittleEndian.PutUint32(block[8:12], 0xFFFFFFFF)
	binary.LittleEndian.PutUint32(block[14:18], uint32(writeRecMask))
	block[18] = writeDupMask
	block[19] = writeTrailMask
	block[20] = writeRecBits
	block[21] = writeDupBits
	block[22] = writeTrailBits
	block[23] = writeBytesPerEntry

	entriesStart := 24
	entriesEnd := entriesStart + len(entries)*writeBytesPerEntry
	if entriesEnd > BlockSize {
		return fmt.Errorf("cdx: writeLeaf: %d entries exceed single-leaf capacity", len(entries))
	}

	// Key text grows backwards from BlockSize toward entriesEnd.
	// Track keyEnd as the exclusive end of the next chunk to be
	// written.
	keyEnd := BlockSize
	var prev []byte

	for i, e := range entries {
		if e.RecNo > writeRecMask {
			return fmt.Errorf("cdx: writeLeaf: entry %d recNo %d exceeds %d",
				i, e.RecNo, writeRecMask)
		}
		dup := sharedPrefix(prev, e.Key)
		trail := trailingSpaces(e.Key)
		// Suffix cannot overlap with the shared prefix. If a key
		// is all spaces, dup and trail can together exceed
		// keyLen; clip the trail to what remains after dup.
		if int(dup)+int(trail) > int(keyLen) {
			trail = int(keyLen) - int(dup)
			if trail < 0 {
				trail = 0
			}
		}
		chunkLen := int(keyLen) - int(dup) - trail
		if chunkLen < 0 {
			chunkLen = 0
		}
		if dup > writeDupMask || trail > writeTrailMask {
			return fmt.Errorf("cdx: writeLeaf: entry %d dup=%d trail=%d exceeds mask width",
				i, dup, trail)
		}

		chunkStart := keyEnd - chunkLen
		if chunkStart < entriesEnd {
			return fmt.Errorf("cdx: writeLeaf: key text at entry %d would overrun entries area", i)
		}
		copy(block[chunkStart:keyEnd], e.Key[int(dup):int(dup)+chunkLen])

		// Pack (recNo, dup, trail) into a single uint64 and
		// write bytesPerEntry little-endian bytes.
		packed := uint64(e.RecNo & writeRecMask)
		packed |= uint64(dup&writeDupMask) << writeRecBits
		packed |= uint64(trail&writeTrailMask) << (writeRecBits + writeDupBits)
		off := entriesStart + i*writeBytesPerEntry
		for j := 0; j < writeBytesPerEntry; j++ {
			block[off+j] = byte(packed >> (uint(j) * 8))
		}

		prev = e.Key
		keyEnd = chunkStart
	}

	// Free space between entries area and key text.
	free := keyEnd - entriesEnd
	if free < 0 {
		free = 0
	}
	binary.LittleEndian.PutUint16(block[12:14], uint16(free))
	return nil
}

// sharedPrefix returns the number of leading bytes common to a
// and b, capped at 255 (dup is stored in a single byte, though
// our writeDupBits limit is tighter).
func sharedPrefix(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// trailingSpaces returns the count of trailing 0x20 bytes.
func trailingSpaces(k []byte) int {
	n := 0
	for i := len(k) - 1; i >= 0 && k[i] == ' '; i-- {
		n++
	}
	return n
}
