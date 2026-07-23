package ntx

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
	"sort"
	"testing"
	"time"
)

// memFile is an in-memory io.ReadWriteSeeker for tests.
type memFile struct {
	data []byte
	pos  int64
}

func (m *memFile) Read(p []byte) (int, error) {
	if m.pos >= int64(len(m.data)) {
		return 0, io.EOF
	}
	n := copy(p, m.data[m.pos:])
	m.pos += int64(n)
	return n, nil
}

func (m *memFile) Write(p []byte) (int, error) {
	end := m.pos + int64(len(p))
	if end > int64(len(m.data)) {
		grown := make([]byte, end)
		copy(grown, m.data)
		m.data = grown
	}
	copy(m.data[m.pos:end], p)
	m.pos = end
	return len(p), nil
}

func (m *memFile) Seek(offset int64, whence int) (int64, error) {
	var base int64
	switch whence {
	case io.SeekStart:
		base = 0
	case io.SeekCurrent:
		base = m.pos
	case io.SeekEnd:
		base = int64(len(m.data))
	default:
		return 0, fmt.Errorf("bad whence %d", whence)
	}
	pos := base + offset
	if pos < 0 {
		return 0, fmt.Errorf("negative position")
	}
	m.pos = pos
	return pos, nil
}

func newTestIndex(t *testing.T, keySize uint16) (*Index, *memFile) {
	t.Helper()

	file := &memFile{}

	ix, err := Create(file, Options{
		KeyExpr: "TESTKEY",
		KeySize: keySize,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	return ix, file
}

// collect drains a cursor from First and returns every entry.
func collect(t *testing.T, ix *Index) []Entry {
	t.Helper()

	cursor := ix.NewCursor()
	cursor.First()

	var entries []Entry

	for cursor.Next() {
		e := cursor.Entry()

		key := make([]byte, len(e.Key))
		copy(key, e.Key)

		entries = append(entries, Entry{Key: key, Recno: e.Recno})
	}

	if err := cursor.Err(); err != nil {
		t.Fatalf("cursor: %v", err)
	}

	return entries
}

func TestHeaderStructuralOffsets(t *testing.T) {
	_, file := newTestIndex(t, 10)

	raw := file.data

	if sig := binary.LittleEndian.Uint16(raw[0:2]); sig != 0x0006 {
		t.Errorf("signature = 0x%04X, want 0x0006 (Clipper 5.x)", sig)
	}
	if root := binary.LittleEndian.Uint32(raw[4:8]); root != pageSize {
		t.Errorf("root offset = %d, want %d", root, pageSize)
	}
	if free := binary.LittleEndian.Uint32(raw[8:12]); free != 0 {
		t.Errorf("free list = %d, want 0", free)
	}
	if is := binary.LittleEndian.Uint16(raw[12:14]); is != 18 {
		t.Errorf("item size = %d, want 18", is)
	}
	if ks := binary.LittleEndian.Uint16(raw[14:16]); ks != 10 {
		t.Errorf("key size = %d, want 10", ks)
	}
	if mi := binary.LittleEndian.Uint16(raw[18:20]); mi != 50 {
		t.Errorf("max item = %d, want 50 for 10 byte keys", mi)
	}
	if hp := binary.LittleEndian.Uint16(raw[20:22]); hp != 25 {
		t.Errorf("half page = %d, want 25", hp)
	}

	expr := raw[22:30]
	if string(expr[:7]) != "TESTKEY" || expr[7] != 0 {
		t.Errorf("key expression bytes = %q", expr)
	}

	if raw[278] != 0 {
		t.Errorf("unique byte = %d, want 0", raw[278])
	}

	if len(file.data) != 2*pageSize {
		t.Errorf("empty index is %d bytes, want %d", len(file.data), 2*pageSize)
	}
}

func TestOpenRereadsHeader(t *testing.T) {
	_, file := newTestIndex(t, 10)

	ix, err := Open(file)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if ix.KeyExpr() != "TESTKEY" {
		t.Errorf("KeyExpr = %q", ix.KeyExpr())
	}
	if ix.KeySize() != 10 {
		t.Errorf("KeySize = %d", ix.KeySize())
	}
	if ix.maxItem != 50 || ix.halfPage != 25 {
		t.Errorf("maxItem/halfPage = %d/%d", ix.maxItem, ix.halfPage)
	}
}

func TestOpenRejectsBadSignature(t *testing.T) {
	_, file := newTestIndex(t, 10)

	file.data[0] = 0x99

	if _, err := Open(file); err == nil {
		t.Fatalf("expected signature error")
	}
}

func TestNodeRoundTrip(t *testing.T) {
	ix, _ := newTestIndex(t, 4)

	offset, err := ix.allocPage()
	if err != nil {
		t.Fatalf("allocPage: %v", err)
	}

	n := &node{
		offset: offset,
		leaf:   false,
		right:  3 * pageSize,
		items: []item{
			{child: pageSize, recno: 11, key: []byte("AAAA")},
			{child: 2 * pageSize, recno: 22, key: []byte("BBBB")},
		},
	}

	if err := ix.writeNode(n); err != nil {
		t.Fatalf("writeNode: %v", err)
	}

	got, err := ix.readNode(offset)
	if err != nil {
		t.Fatalf("readNode: %v", err)
	}

	if got.leaf {
		t.Errorf("node decoded as leaf")
	}
	if got.right != n.right {
		t.Errorf("right = %d, want %d", got.right, n.right)
	}
	if len(got.items) != 2 {
		t.Fatalf("items = %d, want 2", len(got.items))
	}
	for i := range n.items {
		if got.items[i].child != n.items[i].child ||
			got.items[i].recno != n.items[i].recno ||
			!bytes.Equal(got.items[i].key, n.items[i].key) {
			t.Errorf("item %d = %+v, want %+v", i, got.items[i], n.items[i])
		}
	}
}

// TestNodeReadFollowsPermutedSlots verifies that a page whose offset
// array is permuted, the way Clipper leaves pages after in-place
// edits, decodes by slot order rather than physical order.
func TestNodeReadFollowsPermutedSlots(t *testing.T) {
	ix, file := newTestIndex(t, 4)

	offset, err := ix.allocPage()
	if err != nil {
		t.Fatalf("allocPage: %v", err)
	}

	n := &node{
		offset: offset,
		leaf:   true,
		items: []item{
			{recno: 1, key: []byte("AAAA")},
			{recno: 2, key: []byte("BBBB")},
		},
	}

	if err := ix.writeNode(n); err != nil {
		t.Fatalf("writeNode: %v", err)
	}

	// Swap the first two slot pointers and the two items' physical
	// positions: logical order must be unchanged.
	raw := file.data[offset : offset+pageSize]

	s0 := binary.LittleEndian.Uint16(raw[2:4])
	s1 := binary.LittleEndian.Uint16(raw[4:6])

	item0 := make([]byte, ix.itemSize)
	item1 := make([]byte, ix.itemSize)
	copy(item0, raw[s0:s0+ix.itemSize])
	copy(item1, raw[s1:s1+ix.itemSize])

	copy(raw[s0:], item1)
	copy(raw[s1:], item0)

	binary.LittleEndian.PutUint16(raw[2:4], s1)
	binary.LittleEndian.PutUint16(raw[4:6], s0)

	got, err := ix.readNode(offset)
	if err != nil {
		t.Fatalf("readNode: %v", err)
	}

	if string(got.items[0].key) != "AAAA" || string(got.items[1].key) != "BBBB" {
		t.Fatalf("permuted page decoded out of order: %q, %q",
			got.items[0].key, got.items[1].key)
	}
}

func TestInsertAndOrderedTraversal(t *testing.T) {
	ix, _ := newTestIndex(t, 8)

	names := []string{"MANGO", "APPLE", "PEAR", "BANANA", "CHERRY", "FIG"}

	for i, name := range names {
		added, err := ix.Insert([]byte(name), uint32(i+1))
		if err != nil {
			t.Fatalf("Insert %s: %v", name, err)
		}
		if !added {
			t.Fatalf("Insert %s reported not added", name)
		}
	}

	entries := collect(t, ix)

	if len(entries) != len(names) {
		t.Fatalf("traversed %d entries, want %d", len(entries), len(names))
	}

	sorted := append([]string(nil), names...)
	sort.Strings(sorted)

	for i, want := range sorted {
		got := string(bytes.TrimRight(entries[i].Key, " "))
		if got != want {
			t.Errorf("position %d = %q, want %q", i, got, want)
		}
	}
}

func TestInsertSplitsPages(t *testing.T) {
	// Key size 200 gives maxItem 3, so splits happen almost at once.
	ix, _ := newTestIndex(t, 200)

	if ix.maxItem >= 8 {
		t.Fatalf("maxItem = %d; test wants a tiny fan-out", ix.maxItem)
	}

	const n = 100

	for i := 0; i < n; i++ {
		key := []byte(fmt.Sprintf("K%06d", i))

		if _, err := ix.Insert(key, uint32(i+1)); err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
	}

	entries := collect(t, ix)

	if len(entries) != n {
		t.Fatalf("traversed %d entries, want %d", len(entries), n)
	}

	for i := 1; i < len(entries); i++ {
		if bytes.Compare(entries[i-1].Key, entries[i].Key) >= 0 {
			t.Fatalf("entries %d and %d out of order", i-1, i)
		}
	}
}

func TestDuplicateKeysOrderedByRecno(t *testing.T) {
	ix, _ := newTestIndex(t, 5)

	for _, recno := range []uint32{30, 10, 20} {
		if _, err := ix.Insert([]byte("SAME"), recno); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	entries := collect(t, ix)

	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}

	for i, want := range []uint32{10, 20, 30} {
		if entries[i].Recno != want {
			t.Errorf("position %d recno = %d, want %d", i, entries[i].Recno, want)
		}
	}
}

func TestUniqueIndexSkipsDuplicates(t *testing.T) {
	file := &memFile{}

	ix, err := Create(file, Options{KeyExpr: "K", KeySize: 5, Unique: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	added, err := ix.Insert([]byte("DUP"), 1)
	if err != nil || !added {
		t.Fatalf("first insert: added=%v err=%v", added, err)
	}

	added, err = ix.Insert([]byte("DUP"), 2)
	if err != nil {
		t.Fatalf("second insert: %v", err)
	}
	if added {
		t.Fatalf("unique index accepted a duplicate key")
	}

	entries := collect(t, ix)

	if len(entries) != 1 || entries[0].Recno != 1 {
		t.Fatalf("unique index kept %+v, want the first record only", entries)
	}
}

func TestSeekPrefix(t *testing.T) {
	ix, _ := newTestIndex(t, 6)

	for i, name := range []string{"ALPHA", "BETA", "BRAVO", "GAMMA"} {
		if _, err := ix.Insert([]byte(name), uint32(i+1)); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	cursor := ix.NewCursor()
	cursor.Seek([]byte("B"))

	var got []string

	for cursor.Next() {
		got = append(got,
			string(bytes.TrimRight(cursor.Entry().Key, " ")))
	}

	if err := cursor.Err(); err != nil {
		t.Fatalf("cursor: %v", err)
	}

	want := []string{"BETA", "BRAVO", "GAMMA"}

	if len(got) != len(want) {
		t.Fatalf("Seek(B) yielded %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Seek(B) yielded %v, want %v", got, want)
		}
	}
}

func TestDeleteLeafAndNotFound(t *testing.T) {
	ix, _ := newTestIndex(t, 5)

	ix.Insert([]byte("ONE"), 1)
	ix.Insert([]byte("TWO"), 2)

	found, err := ix.Delete([]byte("ONE"), 1)
	if err != nil || !found {
		t.Fatalf("Delete: found=%v err=%v", found, err)
	}

	found, err = ix.Delete([]byte("ONE"), 1)
	if err != nil {
		t.Fatalf("Delete again: %v", err)
	}
	if found {
		t.Fatalf("Delete found an already deleted entry")
	}

	// Same key, wrong recno: not found.
	found, _ = ix.Delete([]byte("TWO"), 99)
	if found {
		t.Fatalf("Delete matched the wrong record number")
	}

	entries := collect(t, ix)
	if len(entries) != 1 || entries[0].Recno != 2 {
		t.Fatalf("remaining entries = %+v", entries)
	}
}

// TestRandomisedAgainstModel drives the index with random inserts and
// deletes and checks the full traversal against a reference model
// after every operation batch.
func TestRandomisedAgainstModel(t *testing.T) {
	// Small fan-out so splits, borrows, merges and root collapse all
	// happen within a few hundred operations.
	ix, _ := newTestIndex(t, 200)

	rng := rand.New(rand.NewSource(20260723))

	type modelEntry struct {
		key   string
		recno uint32
	}

	var model []modelEntry

	verify := func(step int) {
		entries := collect(t, ix)

		sorted := append([]modelEntry(nil), model...)
		sort.Slice(sorted, func(a, b int) bool {
			if sorted[a].key != sorted[b].key {
				return sorted[a].key < sorted[b].key
			}
			return sorted[a].recno < sorted[b].recno
		})

		if len(entries) != len(sorted) {
			t.Fatalf("step %d: index has %d entries, model has %d",
				step, len(entries), len(sorted))
		}

		for i := range sorted {
			gotKey := string(entries[i].Key)
			if gotKey != sorted[i].key || entries[i].Recno != sorted[i].recno {
				t.Fatalf(
					"step %d, position %d: index (%q, %d), model (%q, %d)",
					step, i,
					gotKey, entries[i].Recno,
					sorted[i].key, sorted[i].recno,
				)
			}
		}
	}

	nextRecno := uint32(0)

	const steps = 60
	const opsPerStep = 20

	for step := 0; step < steps; step++ {
		for op := 0; op < opsPerStep; op++ {
			deleting := len(model) > 0 && rng.Intn(100) < 40

			if deleting {
				i := rng.Intn(len(model))
				victim := model[i]

				found, err := ix.Delete([]byte(victim.key), victim.recno)
				if err != nil {
					t.Fatalf("Delete(%q, %d): %v", victim.key, victim.recno, err)
				}
				if !found {
					t.Fatalf("Delete(%q, %d): not found, model says present",
						victim.key, victim.recno)
				}

				model = append(model[:i], model[i+1:]...)
				continue
			}

			nextRecno++

			// A small key space forces duplicate keys.
			key := string(CharKey(fmt.Sprintf("K%03d", rng.Intn(150)), 200))

			if _, err := ix.Insert([]byte(key), nextRecno); err != nil {
				t.Fatalf("Insert: %v", err)
			}

			model = append(model, modelEntry{key: key, recno: nextRecno})
		}

		verify(step)
	}

	// Drain to empty: exercises merges and root collapse fully.
	for len(model) > 0 {
		victim := model[len(model)-1]
		model = model[:len(model)-1]

		found, err := ix.Delete([]byte(victim.key), victim.recno)
		if err != nil || !found {
			t.Fatalf("drain Delete(%q, %d): found=%v err=%v",
				victim.key, victim.recno, found, err)
		}
	}

	if entries := collect(t, ix); len(entries) != 0 {
		t.Fatalf("drained index still has %d entries", len(entries))
	}
}

func TestFreedPagesAreReused(t *testing.T) {
	ix, file := newTestIndex(t, 200)

	for i := 0; i < 60; i++ {
		ix.Insert([]byte(fmt.Sprintf("K%04d", i)), uint32(i+1))
	}

	grown := len(file.data)

	for i := 0; i < 60; i++ {
		ix.Delete([]byte(fmt.Sprintf("K%04d", i)), uint32(i+1))
	}

	for i := 0; i < 60; i++ {
		ix.Insert([]byte(fmt.Sprintf("R%04d", i)), uint32(i+1))
	}

	if len(file.data) > grown {
		t.Fatalf(
			"file grew from %d to %d bytes; free pages were not reused",
			grown,
			len(file.data),
		)
	}
}

func TestKeyHelpers(t *testing.T) {
	if got := string(CharKey("AB", 4)); got != "AB  " {
		t.Errorf("CharKey = %q", got)
	}
	if got := string(CharKey("ABCDE", 3)); got != "ABC" {
		t.Errorf("CharKey truncation = %q", got)
	}

	d := time.Date(2026, time.July, 23, 0, 0, 0, 0, time.UTC)
	if got := string(DateKey(d)); got != "20260723" {
		t.Errorf("DateKey = %q", got)
	}
	if got := string(DateKey(time.Time{})); got != "        " {
		t.Errorf("DateKey zero = %q", got)
	}

	if got := string(LogicalKey(true)); got != "T" {
		t.Errorf("LogicalKey = %q", got)
	}

	key, err := NumericKey(42, 6, 0)
	if err != nil {
		t.Fatalf("NumericKey: %v", err)
	}
	if string(key) != "    42" {
		t.Errorf("NumericKey = %q", string(key))
	}

	key, err = NumericKey(3.5, 6, 2)
	if err != nil {
		t.Fatalf("NumericKey: %v", err)
	}
	if string(key) != "  3.50" {
		t.Errorf("NumericKey decimals = %q", string(key))
	}

	// Numeric ordering under byte comparison.
	nine, _ := NumericKey(9, 4, 0)
	ten, _ := NumericKey(10, 4, 0)
	if bytes.Compare(nine, ten) >= 0 {
		t.Errorf("NumericKey collation broken: %q >= %q", nine, ten)
	}

	if _, err := NumericKey(-1, 6, 0); err == nil {
		t.Errorf("NumericKey accepted a negative value (T-01)")
	}

	if _, err := NumericKey(1234567, 4, 0); err == nil {
		t.Errorf("NumericKey accepted an overflowing value")
	}
}
