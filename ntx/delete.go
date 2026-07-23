package ntx

import "fmt"

// Delete removes the entry with exactly the given key and record
// number. It returns false if no such entry exists.
//
// Short keys are space padded to the index key size.
func (ix *Index) Delete(key []byte, recno uint32) (bool, error) {
	key, err := ix.normalizeKey(key)
	if err != nil {
		return false, err
	}

	root, err := ix.readNode(ix.root)
	if err != nil {
		return false, err
	}

	found, _, err := ix.deleteRec(root, key, recno)
	if err != nil {
		return false, err
	}

	if !found {
		return false, nil
	}

	// A branch root left with no keys has a single child, which
	// becomes the new root, shrinking the tree by one level.
	if !root.leaf && len(root.items) == 0 {
		old := root.offset

		ix.root = root.right

		if err := ix.freePage(old); err != nil {
			return false, err
		}
	}

	if err := ix.writeHeader(); err != nil {
		return false, err
	}

	return true, nil
}

// deleteRec removes (key, recno) from the subtree rooted at n.
//
// It reports whether the entry was found and whether n underflowed;
// the caller rebalances an underflowed child.
func (ix *Index) deleteRec(n *node, key []byte, recno uint32) (bool, bool, error) {
	i := findInNode(n, key, recno)

	exact := i < len(n.items) &&
		compareEntries(n.items[i].key, n.items[i].recno, key, recno) == 0

	if n.leaf {
		if !exact {
			return false, false, nil
		}

		n.items = append(n.items[:i], n.items[i+1:]...)

		if err := ix.writeNode(n); err != nil {
			return false, false, err
		}

		return true, ix.underflowed(n), nil
	}

	if exact {
		// An internal entry is replaced by its predecessor: the
		// largest entry of its left subtree, which lives in a leaf.
		child, err := ix.readNode(n.items[i].child)
		if err != nil {
			return false, false, err
		}

		pred, childUnderflow, err := ix.deleteMax(child)
		if err != nil {
			return false, false, err
		}

		n.items[i].key = pred.Key
		n.items[i].recno = pred.Recno

		if err := ix.writeNode(n); err != nil {
			return false, false, err
		}

		if childUnderflow {
			if err := ix.fixChild(n, i, child); err != nil {
				return false, false, err
			}
		}

		return true, ix.underflowed(n), nil
	}

	child, err := ix.readNode(n.childAt(i))
	if err != nil {
		return false, false, err
	}

	found, childUnderflow, err := ix.deleteRec(child, key, recno)
	if err != nil {
		return false, false, err
	}

	if childUnderflow {
		if err := ix.fixChild(n, i, child); err != nil {
			return false, false, err
		}
	}

	return found, ix.underflowed(n), nil
}

// deleteMax removes and returns the largest entry of the subtree
// rooted at n, reporting whether n underflowed.
func (ix *Index) deleteMax(n *node) (Entry, bool, error) {
	if n.leaf {
		if len(n.items) == 0 {
			return Entry{}, false, fmt.Errorf(
				"deleteMax on empty leaf at %d: index is corrupt",
				n.offset,
			)
		}

		last := n.items[len(n.items)-1]
		n.items = n.items[:len(n.items)-1]

		if err := ix.writeNode(n); err != nil {
			return Entry{}, false, err
		}

		return Entry{Key: last.key, Recno: last.recno}, ix.underflowed(n), nil
	}

	child, err := ix.readNode(n.right)
	if err != nil {
		return Entry{}, false, err
	}

	entry, childUnderflow, err := ix.deleteMax(child)
	if err != nil {
		return Entry{}, false, err
	}

	if childUnderflow {
		if err := ix.fixChild(n, len(n.items), child); err != nil {
			return Entry{}, false, err
		}
	}

	return entry, ix.underflowed(n), nil
}

func (ix *Index) underflowed(n *node) bool {
	return len(n.items) < int(ix.halfPage)
}

// fixChild restores the minimum fill of parent's child at position i
// by borrowing from a sibling when one can spare a key, or merging
// with a sibling otherwise.
//
// The child has already been written by the caller; fixChild rewrites
// every page it modifies.
func (ix *Index) fixChild(parent *node, i int, child *node) error {
	// Borrow from the left sibling.
	if i > 0 {
		left, err := ix.readNode(parent.childAt(i - 1))
		if err != nil {
			return err
		}

		if len(left.items) > int(ix.halfPage) {
			return ix.rotateRight(parent, i, left, child)
		}
	}

	// Borrow from the right sibling.
	if i < len(parent.items) {
		right, err := ix.readNode(parent.childAt(i + 1))
		if err != nil {
			return err
		}

		if len(right.items) > int(ix.halfPage) {
			return ix.rotateLeft(parent, i, child, right)
		}

		// Neither sibling can spare a key: merge with the right one.
		return ix.mergeChildren(parent, i, child, right)
	}

	// The child is the rightmost; merge with its left sibling.
	left, err := ix.readNode(parent.childAt(i - 1))
	if err != nil {
		return err
	}

	return ix.mergeChildren(parent, i-1, left, child)
}

// rotateRight moves the largest entry of the left sibling up into the
// parent and the parent's separator down into the child's front.
func (ix *Index) rotateRight(parent *node, i int, left, child *node) error {
	sep := &parent.items[i-1]

	// The separator descends, pointing left at the subtree between
	// the left sibling's new last key and itself.
	front := item{
		child: left.right,
		recno: sep.recno,
		key:   sep.key,
	}

	child.items = append(child.items, item{})
	copy(child.items[1:], child.items)
	child.items[0] = front

	last := left.items[len(left.items)-1]
	left.items = left.items[:len(left.items)-1]
	left.right = last.child

	sep.key = last.key
	sep.recno = last.recno

	if err := ix.writeNode(left); err != nil {
		return err
	}

	if err := ix.writeNode(child); err != nil {
		return err
	}

	return ix.writeNode(parent)
}

// rotateLeft moves the smallest entry of the right sibling up into
// the parent and the parent's separator down onto the child's back.
func (ix *Index) rotateLeft(parent *node, i int, child, right *node) error {
	sep := &parent.items[i]

	// The separator descends, pointing left at what was the child's
	// rightmost subtree.
	child.items = append(child.items, item{
		child: child.right,
		recno: sep.recno,
		key:   sep.key,
	})

	first := right.items[0]
	right.items = append(right.items[:0], right.items[1:]...)
	child.right = first.child

	sep.key = first.key
	sep.recno = first.recno

	if err := ix.writeNode(right); err != nil {
		return err
	}

	if err := ix.writeNode(child); err != nil {
		return err
	}

	return ix.writeNode(parent)
}

// mergeChildren merges the children at parent positions i and i+1
// around the separator at parent.items[i], freeing the right child's
// page.
func (ix *Index) mergeChildren(parent *node, i int, left, right *node) error {
	sep := parent.items[i]

	left.items = append(left.items, item{
		child: left.right,
		recno: sep.recno,
		key:   sep.key,
	})

	left.items = append(left.items, right.items...)
	left.right = right.right

	// Remove the separator; the surviving pointer at position i must
	// reach the merged node.
	parent.items = append(parent.items[:i], parent.items[i+1:]...)
	parent.setChildAt(i, left.offset)

	if err := ix.freePage(right.offset); err != nil {
		return err
	}

	if err := ix.writeNode(left); err != nil {
		return err
	}

	return ix.writeNode(parent)
}
