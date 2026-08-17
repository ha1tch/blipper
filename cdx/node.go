package cdx

import (
	"encoding/binary"
	"fmt"
	"io"
)

// entry is one key/recno pair in a node, reconstructed from the
// packed exterior encoding or read directly from an interior node.
type entry struct {
	key   []byte // exactly h.keyLen bytes, right-padded with spaces
	recNo uint32 // record number (leaf) or intra-index page pointer (interior)
}

// node is a fully decoded 512-byte page. Interior and exterior
// nodes are represented uniformly: interior nodes store the
// child-page pointer in entry.recNo, leaf nodes store the DBF
// record number.
type node struct {
	attr    uint16
	nKeys   uint16
	left    int32 // sibling on the same level, -1 if absent
	right   int32
	entries []entry
}

// isLeaf reports whether the node is an exterior (leaf) node.
// A single-page tree carries attr = nodeRoot | nodeLeaf.
func (n *node) isLeaf() bool { return n.attr&nodeLeaf != 0 }

// readNode reads and decodes the node at the given file offset.
// keyLen is required to reconstruct exterior-node keys, whose
// stored bytes are only the non-shared, non-trailing-blank chunk.
func readNode(rw io.ReadSeeker, offset int64, keyLen uint16) (*node, error) {
	if offset < 0 || offset%BlockSize != 0 {
		return nil, fmt.Errorf("cdx: node offset %d not block-aligned", offset)
	}
	if _, err := rw.Seek(offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("cdx: seek to node: %w", err)
	}
	var raw [BlockSize]byte
	if _, err := io.ReadFull(rw, raw[:]); err != nil {
		return nil, fmt.Errorf("cdx: read node: %w", err)
	}

	n := &node{
		attr:  binary.LittleEndian.Uint16(raw[0:2]),
		nKeys: binary.LittleEndian.Uint16(raw[2:4]),
		left:  int32(binary.LittleEndian.Uint32(raw[4:8])),
		right: int32(binary.LittleEndian.Uint32(raw[8:12])),
	}
	if n.nKeys == 0 {
		return n, nil
	}

	if n.isLeaf() {
		return decodeExterior(n, raw[:], keyLen)
	}
	return decodeInterior(n, raw[:], keyLen)
}

// decodeInterior reads keyCount entries of (key || 4-byte pointer)
// from the fixed 12..511 area. The pointer is an intra-index block
// offset (into the same CDX file), not a DBF record number.
func decodeInterior(n *node, raw []byte, keyLen uint16) (*node, error) {
	entrySize := int(keyLen) + 4
	if entrySize <= 0 {
		return nil, ErrMalformedNode
	}
	area := raw[12:BlockSize]
	need := int(n.nKeys) * entrySize
	if need > len(area) {
		return nil, ErrMalformedNode
	}
	n.entries = make([]entry, n.nKeys)
	for i := 0; i < int(n.nKeys); i++ {
		off := i * entrySize
		key := make([]byte, keyLen)
		copy(key, area[off:off+int(keyLen)])
		ptr := binary.LittleEndian.Uint32(area[off+int(keyLen) : off+entrySize])
		n.entries[i] = entry{key: key, recNo: ptr}
	}
	return n, nil
}

// decodeExterior unpacks the bit-packed record/dup/trail encoding
// documented in aa975346's "Compact Index Exterior Node Record"
// table. Key text is stored at the logical end of the node,
// working backwards, so entry 0's characters occupy the highest
// available offsets and entry i takes bytes preceding entry i-1.
//
// Each stored entry is `bytesPerEntry` bytes read little-endian
// into a uint64, from which recNo, dup and trail counts are
// extracted using the masks in the node header (offsets 14-19)
// and the bit widths at 20-22.
func decodeExterior(n *node, raw []byte, keyLen uint16) (*node, error) {
	// Node header for exterior/leaf.
	// bytes 12-13: available free space (informational)
	recMask := binary.LittleEndian.Uint32(raw[14:18])
	dupMask := uint64(raw[18])
	trailMask := uint64(raw[19])
	recBits := uint(raw[20])
	dupBits := uint(raw[21])
	trailBits := uint(raw[22])
	bytesPerEntry := int(raw[23])

	if bytesPerEntry <= 0 || bytesPerEntry > 8 {
		return nil, ErrMalformedNode
	}
	// Total width across the three fields must fit within
	// bytesPerEntry*8; otherwise the header is inconsistent.
	if recBits+dupBits+trailBits > uint(bytesPerEntry)*8 {
		return nil, ErrMalformedNode
	}

	entriesStart := 24
	entriesEnd := entriesStart + int(n.nKeys)*bytesPerEntry
	if entriesEnd > BlockSize {
		return nil, ErrMalformedNode
	}

	// Key text is packed at the end of the node, filling
	// backwards. keyEnd tracks the exclusive end of the *next*
	// chunk to read; it decreases as we process each entry.
	keyEnd := BlockSize
	prevKey := make([]byte, keyLen)

	n.entries = make([]entry, n.nKeys)
	for i := 0; i < int(n.nKeys); i++ {
		off := entriesStart + i*bytesPerEntry
		// Little-endian pack into a 64-bit word, then extract.
		var packed uint64
		for j := bytesPerEntry - 1; j >= 0; j-- {
			packed = (packed << 8) | uint64(raw[off+j])
		}
		recNo := uint32(packed & uint64(recMask))
		dup := (packed >> recBits) & dupMask
		trail := (packed >> (recBits + dupBits)) & trailMask

		if dup > uint64(keyLen) || trail > uint64(keyLen) {
			return nil, ErrMalformedNode
		}
		nChars := int(uint64(keyLen) - dup - trail)
		if nChars < 0 || nChars > int(keyLen) {
			return nil, ErrMalformedNode
		}

		chunkStart := keyEnd - nChars
		if chunkStart < entriesEnd {
			return nil, ErrMalformedNode
		}
		chunk := raw[chunkStart:keyEnd]

		key := make([]byte, keyLen)
		copy(key, prevKey[:int(dup)])
		copy(key[int(dup):], chunk)
		for j := int(dup) + nChars; j < int(keyLen); j++ {
			key[j] = ' '
		}
		n.entries[i] = entry{key: key, recNo: recNo}

		prevKey = key
		keyEnd = chunkStart
	}
	return n, nil
}
