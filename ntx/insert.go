package ntx

// Insert adds an entry to the index.
//
// Short keys are space padded to the index key size. For a unique
// index, an entry whose key is already present is silently skipped
// and Insert returns false, matching Clipper's INDEX ON ... UNIQUE
// semantics of keeping only the first record per key value.
func (ix *Index) Insert(key []byte, recno uint32) (bool, error) {
	key, err := ix.normalizeKey(key)
	if err != nil {
		return false, err
	}

	if ix.unique {
		exists, err := ix.ContainsKey(key)
		if err != nil {
			return false, err
		}
		if exists {
			return false, nil
		}
	}

	root, err := ix.readNode(ix.root)
	if err != nil {
		return false, err
	}

	// A full root splits into a fresh root with one key, growing the
	// tree by one level. Splitting on the way down means no split
	// ever propagates back up.
	if len(root.items) == int(ix.maxItem) {
		newOffset, err := ix.allocPage()
		if err != nil {
			return false, err
		}

		newRoot := &node{
			offset: newOffset,
			leaf:   false,
			right:  root.offset,
		}

		if err := ix.splitChild(newRoot, 0, root); err != nil {
			return false, err
		}

		ix.root = newOffset
		root = newRoot
	}

	if err := ix.insertNonFull(root, key, recno); err != nil {
		return false, err
	}

	if err := ix.writeHeader(); err != nil {
		return false, err
	}

	return true, nil
}

// insertNonFull inserts into the subtree rooted at n, which must not
// be full. Full children are split before descending into them.
func (ix *Index) insertNonFull(n *node, key []byte, recno uint32) error {
	i := findInNode(n, key, recno)

	if n.leaf {
		n.items = append(n.items, item{})
		copy(n.items[i+1:], n.items[i:])
		n.items[i] = item{child: 0, recno: recno, key: key}

		return ix.writeNode(n)
	}

	child, err := ix.readNode(n.childAt(i))
	if err != nil {
		return err
	}

	if len(child.items) == int(ix.maxItem) {
		if err := ix.splitChild(n, i, child); err != nil {
			return err
		}

		// The median now sits at n.items[i]; entries above it belong
		// in the new right sibling.
		median := n.items[i]

		if compareEntries(key, recno, median.key, median.recno) > 0 {
			i++
		}

		child, err = ix.readNode(n.childAt(i))
		if err != nil {
			return err
		}
	}

	return ix.insertNonFull(child, key, recno)
}

// splitChild splits the full child at parent position i around its
// median key. The child keeps the lower half, a new sibling takes the
// upper half, and the median moves into the parent at position i.
func (ix *Index) splitChild(parent *node, i int, child *node) error {
	m := len(child.items) / 2
	median := child.items[m]

	rightOffset, err := ix.allocPage()
	if err != nil {
		return err
	}

	right := &node{
		offset: rightOffset,
		leaf:   child.leaf,
		right:  child.right,
	}

	right.items = append(right.items, child.items[m+1:]...)

	child.items = child.items[:m]
	// Keys between the child's new last key and the median live in
	// the median's old left subtree.
	child.right = median.child

	// The median enters the parent pointing left at the lower half;
	// the pointer that used to reach the unsplit child now reaches
	// the new sibling.
	parent.items = append(parent.items, item{})
	copy(parent.items[i+1:], parent.items[i:])
	parent.items[i] = item{
		child: child.offset,
		recno: median.recno,
		key:   median.key,
	}

	parent.setChildAt(i+1, rightOffset)

	if err := ix.writeNode(child); err != nil {
		return err
	}

	if err := ix.writeNode(right); err != nil {
		return err
	}

	return ix.writeNode(parent)
}
