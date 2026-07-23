package ntx

// NewCursor returns a cursor that is not yet positioned; call First
// or Seek before Next.
func (ix *Index) NewCursor() *Cursor {
	return &Cursor{index: ix}
}

// First positions the cursor before the smallest entry.
func (c *Cursor) First() {
	c.err = nil
	c.stack = c.stack[:0]
	c.push(c.index.root, 0, false)
}

// Seek positions the cursor before the first entry whose key is
// greater than or equal to the given key.
//
// Short keys are space padded to the index key size, which makes Seek
// with a short key a prefix seek: Seek([]byte("AB")) lands on the
// first key starting at or above "AB".
func (c *Cursor) Seek(key []byte) {
	c.err = nil
	c.stack = c.stack[:0]

	key, err := c.index.normalizeKey(key)
	if err != nil {
		c.err = err
		return
	}

	offset := c.index.root

	for {
		n, err := c.index.readNode(offset)
		if err != nil {
			c.err = err
			return
		}

		i := findInNode(n, key, 0)

		if n.leaf {
			c.pushNode(n, i, false)
			return
		}

		// The subtree left of items[i] holds smaller keys that may
		// still be >= the wanted key; descend with the frame marked
		// entered so items[i] is emitted after the subtree.
		c.pushNode(n, i, true)

		offset = n.childAt(i)
	}
}

// Next advances the cursor to the next entry in key order.
//
// It returns false when the traversal is exhausted or an error
// occurs; Err distinguishes the two.
func (c *Cursor) Next() bool {
	if c.err != nil {
		return false
	}

	for len(c.stack) > 0 {
		top := &c.stack[len(c.stack)-1]

		n, err := c.index.readNode(top.offset)
		if err != nil {
			c.err = err
			return false
		}

		if n.leaf {
			if top.index < len(n.items) {
				it := n.items[top.index]
				top.index++
				c.current = Entry{Key: it.key, Recno: it.recno}
				return true
			}

			c.stack = c.stack[:len(c.stack)-1]
			continue
		}

		if !top.entered {
			top.entered = true
			c.push(n.childAt(top.index), 0, false)
			continue
		}

		if top.index < len(n.items) {
			it := n.items[top.index]
			top.index++
			top.entered = false
			c.current = Entry{Key: it.key, Recno: it.recno}
			return true
		}

		c.stack = c.stack[:len(c.stack)-1]
	}

	return false
}

// Entry returns the entry at the current cursor position.
//
// It is only valid after a call to Next that returned true.
func (c *Cursor) Entry() Entry {
	return c.current
}

// Err returns the first error encountered while positioning or
// advancing the cursor.
func (c *Cursor) Err() error {
	return c.err
}

func (c *Cursor) push(offset int64, index int, entered bool) {
	c.stack = append(c.stack, frame{
		offset:  offset,
		index:   index,
		entered: entered,
	})
}

func (c *Cursor) pushNode(n *node, index int, entered bool) {
	c.push(n.offset, index, entered)
}
