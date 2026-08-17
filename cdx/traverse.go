package cdx

import (
	"bytes"
	"fmt"
)

// Entry is a single (key, record-number) pair yielded by Traverse.
// Key is exactly the tag's key length in bytes, right-padded with
// spaces the way DBFCDX.LIB stores it. RecNo is one-based.
type Entry struct {
	Key   []byte
	RecNo uint32
}

// Traverse walks a tag's leaf entries in index order, calling fn
// for each one. Returning a non-nil error from fn stops the walk
// and Traverse returns that error.
//
// The traversal descends only into leaf nodes; interior traversal
// happens as a byproduct of descending the tree. Sibling links
// (left/right at offsets 4-11 of each node) are used to walk
// leaves horizontally rather than re-descending for each leaf,
// which matches how FoxPro itself traverses.
func (f *File) Traverse(t *Tag, fn func(Entry) error) error {
	if t == nil || t.perTagHdr == nil {
		return fmt.Errorf("cdx: tag not loaded")
	}
	keyLen := t.perTagHdr.keyLen

	// Descend to the leftmost leaf, then walk .right until -1.
	offset := int64(t.perTagHdr.rootOffset)
	for {
		n, err := readNode(f.rw, offset, keyLen)
		if err != nil {
			return err
		}
		if n.isLeaf() {
			// Walk this leaf and its right siblings.
			for {
				for _, e := range n.entries {
					if err := fn(Entry{Key: e.key, RecNo: e.recNo}); err != nil {
						return err
					}
				}
				if n.right < 0 {
					return nil
				}
				n, err = readNode(f.rw, int64(n.right), keyLen)
				if err != nil {
					return err
				}
			}
		}
		if len(n.entries) == 0 {
			return fmt.Errorf("cdx: empty interior node at %d", offset)
		}
		offset = int64(n.entries[0].recNo)
	}
}

// Seek finds the first entry whose key is greater than or equal to
// the supplied key, using the tag's key length as the comparison
// width (input keys shorter than that are right-padded with
// spaces to match Clipper's convention).
//
// Seek returns the entry it landed on, whether an exact match was
// found, and an error. A non-exact match is not an error;
// ErrTagNotFound is reserved for the tag lookup itself.
func (f *File) Seek(t *Tag, key []byte) (Entry, bool, error) {
	if t == nil || t.perTagHdr == nil {
		return Entry{}, false, fmt.Errorf("cdx: tag not loaded")
	}
	keyLen := t.perTagHdr.keyLen
	target := padKey(key, int(keyLen))

	// Descend to the correct leaf by comparing against the first
	// key of each interior child, taking the rightmost child
	// whose first key is <= target.
	offset := int64(t.perTagHdr.rootOffset)
	for {
		n, err := readNode(f.rw, offset, keyLen)
		if err != nil {
			return Entry{}, false, err
		}
		if n.isLeaf() {
			for _, e := range n.entries {
				c := bytes.Compare(e.key, target)
				if c == 0 {
					return Entry{Key: e.key, RecNo: e.recNo}, true, nil
				}
				if c > 0 {
					return Entry{Key: e.key, RecNo: e.recNo}, false, nil
				}
			}
			// Walk right until we find one or exhaust the leaves.
			for n.right >= 0 {
				n, err = readNode(f.rw, int64(n.right), keyLen)
				if err != nil {
					return Entry{}, false, err
				}
				for _, e := range n.entries {
					c := bytes.Compare(e.key, target)
					if c == 0 {
						return Entry{Key: e.key, RecNo: e.recNo}, true, nil
					}
					if c > 0 {
						return Entry{Key: e.key, RecNo: e.recNo}, false, nil
					}
				}
			}
			return Entry{}, false, nil
		}
		// Interior node: pick the child whose first key is <=
		// target. entries are sorted; take the rightmost one
		// that qualifies.
		child := n.entries[0]
		for _, e := range n.entries {
			if bytes.Compare(e.key, target) <= 0 {
				child = e
			} else {
				break
			}
		}
		offset = int64(child.recNo)
	}
}

// padKey right-pads to the given width with 0x20, or truncates.
// Matches how DBFCDX.LIB and FoxPro 2.x compose their key values.
func padKey(k []byte, width int) []byte {
	out := make([]byte, width)
	n := len(k)
	if n > width {
		n = width
	}
	copy(out, k[:n])
	for i := n; i < width; i++ {
		out[i] = ' '
	}
	return out
}
