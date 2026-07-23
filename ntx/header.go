package ntx

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

const (
	pageSize = 1024

	// Header field offsets within page 0.
	sigOff      = 0  // uint16: 3 = Clipper 87, 6 = Clipper 5.x
	versionOff  = 2  // uint16: indexing version
	rootOff     = 4  // uint32: file offset of the root page
	nextFreeOff = 8  // uint32: head of the free page list, 0 = none
	itemSizeOff = 12 // uint16: key size + 8
	keySizeOff  = 14 // uint16
	keyDecOff   = 16 // uint16
	maxItemOff  = 18 // uint16
	halfPageOff = 20 // uint16
	keyExprOff  = 22 // NUL-terminated, at most maxKeyExpr bytes
	uniqueOff   = 278

	maxKeyExpr = 256

	sigClipper87 = 0x0003
	sigClipper5  = 0x0006
	// NTXLOCK2.OBJ sets bit 5 of the Clipper 5.x signature.
	sigClipper5Lock2 = 0x0026

	indexVersion = 1

	// MaxKeySize is the largest key this package accepts, matching
	// the Clipper limit of 250 bytes.
	MaxKeySize = 250
)

func (ix *Index) writeHeader() error {
	var raw [pageSize]byte

	binary.LittleEndian.PutUint16(raw[sigOff:], sigClipper5)
	binary.LittleEndian.PutUint16(raw[versionOff:], indexVersion)
	binary.LittleEndian.PutUint32(raw[rootOff:], uint32(ix.root))
	binary.LittleEndian.PutUint32(raw[nextFreeOff:], uint32(ix.nextFree))
	binary.LittleEndian.PutUint16(raw[itemSizeOff:], ix.itemSize)
	binary.LittleEndian.PutUint16(raw[keySizeOff:], ix.keySize)
	binary.LittleEndian.PutUint16(raw[keyDecOff:], ix.decimals)
	binary.LittleEndian.PutUint16(raw[maxItemOff:], ix.maxItem)
	binary.LittleEndian.PutUint16(raw[halfPageOff:], ix.halfPage)

	copy(raw[keyExprOff:keyExprOff+maxKeyExpr-1], ix.keyExpr)

	if ix.unique {
		raw[uniqueOff] = 1
	}

	if _, err := ix.rw.Seek(0, io.SeekStart); err != nil {
		return err
	}

	if _, err := ix.rw.Write(raw[:]); err != nil {
		return fmt.Errorf("writing NTX header: %w", err)
	}

	return nil
}

func (ix *Index) readHeader() error {
	var raw [pageSize]byte

	if _, err := ix.rw.Seek(0, io.SeekStart); err != nil {
		return err
	}

	if _, err := io.ReadFull(ix.rw, raw[:]); err != nil {
		return fmt.Errorf("reading NTX header: %w", err)
	}

	sig := binary.LittleEndian.Uint16(raw[sigOff:])

	switch sig {
	case sigClipper87, sigClipper5, sigClipper5Lock2:
		// Accepted.
	default:
		return fmt.Errorf("unsupported NTX signature 0x%04X", sig)
	}

	ix.root = int64(binary.LittleEndian.Uint32(raw[rootOff:]))
	ix.nextFree = int64(binary.LittleEndian.Uint32(raw[nextFreeOff:]))
	ix.itemSize = binary.LittleEndian.Uint16(raw[itemSizeOff:])
	ix.keySize = binary.LittleEndian.Uint16(raw[keySizeOff:])
	ix.decimals = binary.LittleEndian.Uint16(raw[keyDecOff:])
	ix.maxItem = binary.LittleEndian.Uint16(raw[maxItemOff:])
	ix.halfPage = binary.LittleEndian.Uint16(raw[halfPageOff:])

	expr := raw[keyExprOff : keyExprOff+maxKeyExpr]
	if i := bytes.IndexByte(expr, 0); i >= 0 {
		expr = expr[:i]
	}
	ix.keyExpr = string(expr)

	ix.unique = raw[uniqueOff] != 0

	if ix.keySize == 0 || ix.keySize > MaxKeySize {
		return fmt.Errorf("bad key size %d", ix.keySize)
	}

	if ix.itemSize != ix.keySize+8 {
		return fmt.Errorf(
			"item size %d disagrees with key size %d",
			ix.itemSize,
			ix.keySize,
		)
	}

	if want := computeMaxItem(ix.itemSize); ix.maxItem == 0 || ix.maxItem > want {
		return fmt.Errorf(
			"max item %d does not fit a %d byte page",
			ix.maxItem,
			pageSize,
		)
	}

	if ix.root == 0 || ix.root%pageSize != 0 {
		return fmt.Errorf("bad root page offset %d", ix.root)
	}

	return nil
}

// computeMaxItem returns the largest key count per page:
// count field + (maxItem+1) offset slots + (maxItem+1) items must fit.
func computeMaxItem(itemSize uint16) uint16 {
	return (pageSize-2)/(itemSize+2) - 1
}
