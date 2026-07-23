package ntx

// Point queries.
//
// These support record-pointer navigation in ordered views: a caller
// can hold only its current (key, recno) position and move one entry
// at a time without keeping a cursor open across writes.

// Min returns the smallest entry in the index.
func (ix *Index) Min() (Entry, bool, error) {
	offset := ix.root

	for {
		n, err := ix.readNode(offset)
		if err != nil {
			return Entry{}, false, err
		}

		if n.leaf {
			if len(n.items) == 0 {
				return Entry{}, false, nil
			}

			it := n.items[0]

			return Entry{Key: it.key, Recno: it.recno}, true, nil
		}

		offset = n.childAt(0)
	}
}

// Max returns the largest entry in the index.
func (ix *Index) Max() (Entry, bool, error) {
	offset := ix.root

	for {
		n, err := ix.readNode(offset)
		if err != nil {
			return Entry{}, false, err
		}

		if n.leaf {
			if len(n.items) == 0 {
				return Entry{}, false, nil
			}

			it := n.items[len(n.items)-1]

			return Entry{Key: it.key, Recno: it.recno}, true, nil
		}

		offset = n.right
	}
}

// FirstGE returns the smallest entry whose key is greater than or
// equal to the given key, after space padding to the key size.
//
// With a short key this is a prefix seek, exactly like Cursor.Seek.
func (ix *Index) FirstGE(key []byte) (Entry, bool, error) {
	key, err := ix.normalizeKey(key)
	if err != nil {
		return Entry{}, false, err
	}

	return ix.descendFirst(key, 0)
}

// Successor returns the smallest entry strictly after (key, recno).
//
// Short keys are space padded to the index key size.
func (ix *Index) Successor(key []byte, recno uint32) (Entry, bool, error) {
	key, err := ix.normalizeKey(key)
	if err != nil {
		return Entry{}, false, err
	}

	var (
		best  Entry
		found bool
	)

	offset := ix.root

	for {
		n, err := ix.readNode(offset)
		if err != nil {
			return Entry{}, false, err
		}

		i := findAbove(n, key, recno)

		// Deeper candidates are always smaller than this level's, so
		// each level may overwrite the running best.
		if i < len(n.items) {
			it := n.items[i]
			best = Entry{Key: it.key, Recno: it.recno}
			found = true
		}

		if n.leaf {
			return best, found, nil
		}

		offset = n.childAt(i)
	}
}

// Predecessor returns the largest entry strictly before (key, recno).
//
// Short keys are space padded to the index key size.
func (ix *Index) Predecessor(key []byte, recno uint32) (Entry, bool, error) {
	key, err := ix.normalizeKey(key)
	if err != nil {
		return Entry{}, false, err
	}

	var (
		best  Entry
		found bool
	)

	offset := ix.root

	for {
		n, err := ix.readNode(offset)
		if err != nil {
			return Entry{}, false, err
		}

		i := findInNode(n, key, recno)

		// Deeper candidates are always larger than this level's, so
		// each level may overwrite the running best.
		if i > 0 {
			it := n.items[i-1]
			best = Entry{Key: it.key, Recno: it.recno}
			found = true
		}

		if n.leaf {
			return best, found, nil
		}

		offset = n.childAt(i)
	}
}

// descendFirst returns the smallest entry at or above (key, recno).
func (ix *Index) descendFirst(key []byte, recno uint32) (Entry, bool, error) {
	var (
		best  Entry
		found bool
	)

	offset := ix.root

	for {
		n, err := ix.readNode(offset)
		if err != nil {
			return Entry{}, false, err
		}

		i := findInNode(n, key, recno)

		if i < len(n.items) {
			it := n.items[i]
			best = Entry{Key: it.key, Recno: it.recno}
			found = true
		}

		if n.leaf {
			return best, found, nil
		}

		offset = n.childAt(i)
	}
}

// findAbove returns the position of the first item in n strictly
// above (key, recno).
func findAbove(n *node, key []byte, recno uint32) int {
	lo, hi := 0, len(n.items)

	for lo < hi {
		mid := (lo + hi) / 2

		it := n.items[mid]

		if compareEntries(it.key, it.recno, key, recno) <= 0 {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	return lo
}
