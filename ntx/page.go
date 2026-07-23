package ntx

import (
	"encoding/binary"
	"fmt"
	"io"
)

// item is one index entry as stored in a page: the file offset of the
// child page holding keys less than this key (0 in leaves), the DBF
// record number, and the key bytes.
type item struct {
	child int64
	recno uint32
	key   []byte
}

// node is the in-memory form of one 1024-byte NTX page.
//
// A branch node with n items has n+1 children: items[i].child holds
// keys below items[i].key, and right holds keys above the last key.
// Leaf nodes have zero children throughout.
type node struct {
	offset int64
	items  []item
	right  int64
	leaf   bool
}

// childAt returns the i-th child pointer, treating the rightmost
// pointer as position len(items).
func (n *node) childAt(i int) int64 {
	if i == len(n.items) {
		return n.right
	}
	return n.items[i].child
}

// setChildAt updates the i-th child pointer, treating the rightmost
// pointer as position len(items).
func (n *node) setChildAt(i int, offset int64) {
	if i == len(n.items) {
		n.right = offset
		return
	}
	n.items[i].child = offset
}

// readNode reads and decodes the page at the given file offset.
//
// The on-disk offset array is followed, so pages written by Clipper
// with permuted item slots decode correctly.
func (ix *Index) readNode(offset int64) (*node, error) {
	if offset <= 0 || offset%pageSize != 0 {
		return nil, fmt.Errorf("bad page offset %d", offset)
	}

	var raw [pageSize]byte

	if _, err := ix.rw.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}

	if _, err := io.ReadFull(ix.rw, raw[:]); err != nil {
		return nil, fmt.Errorf("reading page at %d: %w", offset, err)
	}

	count := int(binary.LittleEndian.Uint16(raw[0:2]))

	if count > int(ix.maxItem) {
		return nil, fmt.Errorf(
			"page at %d claims %d items, max is %d",
			offset,
			count,
			ix.maxItem,
		)
	}

	n := &node{
		offset: offset,
		items:  make([]item, count),
		leaf:   true,
	}

	readItem := func(slot int) (int64, uint32, []byte, error) {
		itemOff := int(binary.LittleEndian.Uint16(raw[2+2*slot:]))

		if itemOff < 2 || itemOff+int(ix.itemSize) > pageSize {
			return 0, 0, nil, fmt.Errorf(
				"page at %d: slot %d points outside the page",
				offset,
				slot,
			)
		}

		child := int64(binary.LittleEndian.Uint32(raw[itemOff:]))
		recno := binary.LittleEndian.Uint32(raw[itemOff+4:])

		key := make([]byte, ix.keySize)
		copy(key, raw[itemOff+8:itemOff+8+int(ix.keySize)])

		return child, recno, key, nil
	}

	for i := 0; i < count; i++ {
		child, recno, key, err := readItem(i)
		if err != nil {
			return nil, err
		}

		n.items[i] = item{child: child, recno: recno, key: key}

		if child != 0 {
			n.leaf = false
		}
	}

	// The slot beyond the last key carries the rightmost child
	// pointer of a branch page.
	child, _, _, err := readItem(count)
	if err != nil {
		return nil, err
	}

	n.right = child

	if child != 0 {
		n.leaf = false
	}

	return n, nil
}

// writeNode encodes and writes a node to its page.
//
// Items are written at canonical positions (slot i at the i-th item
// area position); readers follow the offset array, so canonical and
// permuted layouts are interchangeable on disk.
func (ix *Index) writeNode(n *node) error {
	if len(n.items) > int(ix.maxItem) {
		return fmt.Errorf(
			"node at %d has %d items, max is %d",
			n.offset,
			len(n.items),
			ix.maxItem,
		)
	}

	var raw [pageSize]byte

	binary.LittleEndian.PutUint16(raw[0:2], uint16(len(n.items)))

	itemArea := 2 + 2*(int(ix.maxItem)+1)

	// Identity permutation for every slot, used or not.
	for slot := 0; slot <= int(ix.maxItem); slot++ {
		itemOff := itemArea + slot*int(ix.itemSize)
		binary.LittleEndian.PutUint16(raw[2+2*slot:], uint16(itemOff))
	}

	for i, it := range n.items {
		itemOff := itemArea + i*int(ix.itemSize)

		binary.LittleEndian.PutUint32(raw[itemOff:], uint32(it.child))
		binary.LittleEndian.PutUint32(raw[itemOff+4:], it.recno)

		if len(it.key) != int(ix.keySize) {
			return fmt.Errorf(
				"node at %d: item %d key is %d bytes, want %d",
				n.offset,
				i,
				len(it.key),
				ix.keySize,
			)
		}

		copy(raw[itemOff+8:], it.key)
	}

	// Rightmost child pointer in the slot beyond the last key.
	rightOff := itemArea + len(n.items)*int(ix.itemSize)
	binary.LittleEndian.PutUint32(raw[rightOff:], uint32(n.right))

	if _, err := ix.rw.Seek(n.offset, io.SeekStart); err != nil {
		return err
	}

	if _, err := ix.rw.Write(raw[:]); err != nil {
		return fmt.Errorf("writing page at %d: %w", n.offset, err)
	}

	return nil
}

// allocPage returns the offset of a page to use, popping the free
// list when possible and extending the file otherwise.
func (ix *Index) allocPage() (int64, error) {
	if ix.nextFree != 0 {
		offset := ix.nextFree

		var raw [4]byte

		if _, err := ix.rw.Seek(offset, io.SeekStart); err != nil {
			return 0, err
		}

		if _, err := io.ReadFull(ix.rw, raw[:]); err != nil {
			return 0, fmt.Errorf("reading free page at %d: %w", offset, err)
		}

		ix.nextFree = int64(binary.LittleEndian.Uint32(raw[:]))

		return offset, nil
	}

	offset := ix.fileSize
	ix.fileSize += pageSize

	return offset, nil
}

// freePage pushes a page onto the free list.
func (ix *Index) freePage(offset int64) error {
	var raw [pageSize]byte

	binary.LittleEndian.PutUint32(raw[0:4], uint32(ix.nextFree))

	if _, err := ix.rw.Seek(offset, io.SeekStart); err != nil {
		return err
	}

	if _, err := ix.rw.Write(raw[:]); err != nil {
		return fmt.Errorf("freeing page at %d: %w", offset, err)
	}

	ix.nextFree = offset

	return nil
}
