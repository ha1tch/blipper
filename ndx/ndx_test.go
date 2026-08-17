package ndx

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
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
		grow := make([]byte, end)
		copy(grow, m.data)
		m.data = grow
	}
	copy(m.data[m.pos:end], p)
	m.pos = end
	return len(p), nil
}

func (m *memFile) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		m.pos = off
	case io.SeekCurrent:
		m.pos += off
	case io.SeekEnd:
		m.pos = int64(len(m.data)) + off
	}
	return m.pos, nil
}

func loadFixture(t *testing.T, name string) *memFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return &memFile{data: data}
}

func padKey(s string, width int) []byte {
	k := make([]byte, width)
	for i := range k {
		k[i] = ' '
	}
	copy(k, s)
	return k
}

// TestReadsClipperCharacterIndex is the oracle test. BYCODE.NDX
// was written by Clipper 5.2e's DBFNDX driver over a table whose
// records were appended in deliberately unsorted order, so an
// implementation that merely preserved append order would fail
// here.
func TestReadsClipperCharacterIndex(t *testing.T) {
	ix, err := Open(loadFixture(t, "BYCODE.NDX"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Header values, checked against a hand decode of the file.
	if got := ix.KeySize(); got != 10 {
		t.Errorf("KeySize = %d, want 10", got)
	}
	if got := ix.KeyType(); got != KeyTypeCharacter {
		t.Errorf("KeyType = %d, want character", got)
	}
	if got := ix.RecordSize(); got != 20 {
		t.Errorf("RecordSize = %d, want 20 (4+4+12)", got)
	}
	if got := ix.KeysPerPage(); got != 25 {
		t.Errorf("KeysPerPage = %d, want 25", got)
	}
	if got := ix.KeyExpr(); got != "CODE" {
		t.Errorf("KeyExpr = %q, want CODE", got)
	}
	if ix.Unique() {
		t.Error("Unique = true, want false")
	}

	// Entries must come back in key order with the record numbers
	// Clipper assigned, not in insertion order.
	entries, err := ix.Entries()
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	want := []struct {
		key   string
		recno uint32
	}{
		{"ALPHA", 2},
		{"BRAVO", 4},
		{"CHARLIE", 3},
		{"DELTA", 1},
		{"ECHO", 5},
	}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(entries), len(want))
	}
	for i, w := range want {
		gotKey := string(bytes.TrimRight(entries[i].Key, " "))
		if gotKey != w.key {
			t.Errorf("entry %d key = %q, want %q", i, gotKey, w.key)
		}
		if entries[i].Recno != w.recno {
			t.Errorf("entry %d recno = %d, want %d", i, entries[i].Recno, w.recno)
		}
	}
}

// TestReadsClipperNumericIndex covers the other key type. Numeric
// keys are IEEE-754 doubles; an implementation comparing them as
// bytes would order them wrongly, which is why both fixtures
// exist rather than just the character one.
func TestReadsClipperNumericIndex(t *testing.T) {
	ix, err := Open(loadFixture(t, "BYNUM.NDX"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := ix.KeyType(); got != KeyTypeNumeric {
		t.Errorf("KeyType = %d, want numeric", got)
	}
	if got := ix.KeySize(); got != 8 {
		t.Errorf("KeySize = %d, want 8", got)
	}
	if got := ix.RecordSize(); got != 16 {
		t.Errorf("RecordSize = %d, want 16 (4+4+8)", got)
	}
	if got := ix.KeyExpr(); got != "NUM" {
		t.Errorf("KeyExpr = %q, want NUM", got)
	}

	entries, err := ix.Entries()
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	want := []struct {
		value float64
		recno uint32
	}{
		{10, 2}, {20, 4}, {30, 3}, {40, 1}, {50, 5},
	}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(entries), len(want))
	}
	for i, w := range want {
		v, err := DecodeNumericKey(entries[i].Key)
		if err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
		if v != w.value {
			t.Errorf("entry %d value = %v, want %v", i, v, w.value)
		}
		if entries[i].Recno != w.recno {
			t.Errorf("entry %d recno = %d, want %d", i, entries[i].Recno, w.recno)
		}
	}
}

// TestKeyRecordSizingRule checks the sizing rule across widths,
// including the two the oracle confirmed.
func TestKeyRecordSizingRule(t *testing.T) {
	cases := map[uint16]uint32{
		1:  12, // 4+4+4
		4:  12,
		5:  16, // rounds up to 8
		8:  16, // oracle: numeric index
		10: 20, // oracle: character index
		12: 20,
		13: 24,
	}
	for keySize, want := range cases {
		if got := keyRecordSize(keySize); got != want {
			t.Errorf("keyRecordSize(%d) = %d, want %d", keySize, got, want)
		}
	}
}

// TestBuildRoundTrip writes an index and reads it back.
func TestBuildRoundTrip(t *testing.T) {
	f := &memFile{}
	ix, err := Create(f, Options{KeyExpr: "NAME", KeySize: 8, KeyType: KeyTypeCharacter})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Deliberately unsorted input.
	in := []Entry{
		{Key: padKey("ZULU", 8), Recno: 1},
		{Key: padKey("ALFA", 8), Recno: 2},
		{Key: padKey("MIKE", 8), Recno: 3},
		{Key: padKey("BRAVO", 8), Recno: 4},
	}
	if err := ix.Build(in); err != nil {
		t.Fatalf("Build: %v", err)
	}

	f.pos = 0
	reopened, err := Open(f)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	entries, err := reopened.Entries()
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	want := []string{"ALFA", "BRAVO", "MIKE", "ZULU"}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(entries), len(want))
	}
	for i, w := range want {
		got := string(bytes.TrimRight(entries[i].Key, " "))
		if got != w {
			t.Errorf("entry %d = %q, want %q", i, got, w)
		}
	}
}

// TestBuildMultiLevelTree forces more entries than one page holds,
// so the interior-node path is exercised rather than only the
// single-root case the oracle fixtures cover.
func TestBuildMultiLevelTree(t *testing.T) {
	f := &memFile{}
	ix, err := Create(f, Options{KeyExpr: "N", KeySize: 8, KeyType: KeyTypeCharacter})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	perPage := int(ix.KeysPerPage())

	// Three pages' worth, so the tree needs at least one interior
	// level.
	n := perPage*3 + 7
	in := make([]Entry, n)
	for i := range in {
		in[i] = Entry{Key: padKey(fmt.Sprintf("K%06d", i), 8), Recno: uint32(i + 1)}
	}
	if err := ix.Build(in); err != nil {
		t.Fatalf("Build: %v", err)
	}

	f.pos = 0
	reopened, _ := Open(f)
	if reopened.root == 0 {
		t.Fatal("root pointer is zero after building a populated index")
	}
	entries, err := reopened.Entries()
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != n {
		t.Fatalf("got %d entries, want %d", len(entries), n)
	}
	// Order must hold across page boundaries, which is where a
	// tree-building bug would show.
	for i := 1; i < len(entries); i++ {
		if bytes.Compare(entries[i-1].Key, entries[i].Key) > 0 {
			t.Fatalf("entries out of order at %d: %q then %q",
				i, entries[i-1].Key, entries[i].Key)
		}
	}
}

// TestNumericOrderingIsNotByteOrdering is the guard against
// carrying the CDX assumption across. CDX stores numbers
// transformed so byte comparison yields numeric order; NDX stores
// plain IEEE doubles, where it does not.
func TestNumericOrderingIsNotByteOrdering(t *testing.T) {
	f := &memFile{}
	ix, err := Create(f, Options{KeyExpr: "V", KeySize: 8, KeyType: KeyTypeNumeric})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	values := []float64{-100, -1.5, 0, 1.5, 100}
	in := make([]Entry, len(values))
	for i, v := range values {
		in[i] = Entry{Key: EncodeNumericKey(v), Recno: uint32(i + 1)}
	}
	// Shuffle by reversing, so sorting has work to do.
	for i, j := 0, len(in)-1; i < j; i, j = i+1, j-1 {
		in[i], in[j] = in[j], in[i]
	}
	if err := ix.Build(in); err != nil {
		t.Fatalf("Build: %v", err)
	}

	f.pos = 0
	reopened, _ := Open(f)
	entries, err := reopened.Entries()
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	for i, want := range values {
		got, err := DecodeNumericKey(entries[i].Key)
		if err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
		if got != want {
			t.Errorf("entry %d = %v, want %v", i, got, want)
		}
	}

	// And demonstrate concretely why the keys must be decoded
	// rather than compared as bytes.
	//
	// Byte comparison of little-endian doubles is not simply
	// wrong — it is unreliable, which is worse, because it agrees
	// with numeric order often enough to survive casual testing.
	// For -100 against 100 it disagrees: the sign bit sits in the
	// most significant byte, which little-endian storage places
	// last, so the comparison is decided by earlier bytes that
	// carry no sign information.
	neg := EncodeNumericKey(-100)
	pos := EncodeNumericKey(100)
	if bytes.Compare(neg, pos) < 0 {
		t.Error("this pair no longer demonstrates the hazard; pick values " +
			"where little-endian byte order disagrees with numeric order")
	}
	if compareDoubles(neg, pos) >= 0 {
		t.Error("compareDoubles(-100, 100) should be negative")
	}
}

// TestUniqueDropsDuplicates covers the unique flag.
func TestUniqueDropsDuplicates(t *testing.T) {
	f := &memFile{}
	ix, err := Create(f, Options{
		KeyExpr: "K", KeySize: 4, KeyType: KeyTypeCharacter, Unique: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	in := []Entry{
		{Key: padKey("AA", 4), Recno: 1},
		{Key: padKey("AA", 4), Recno: 2},
		{Key: padKey("BB", 4), Recno: 3},
	}
	if err := ix.Build(in); err != nil {
		t.Fatalf("Build: %v", err)
	}
	f.pos = 0
	reopened, _ := Open(f)
	if !reopened.Unique() {
		t.Error("Unique flag did not survive the round trip")
	}
	entries, _ := reopened.Entries()
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 after deduplication", len(entries))
	}
	// The first record for a key wins, matching dBASE.
	if entries[0].Recno != 1 {
		t.Errorf("kept record %d for AA, want 1 (first wins)", entries[0].Recno)
	}
}

// TestSeekFindsAllMatches covers non-unique lookup.
func TestSeekFindsAllMatches(t *testing.T) {
	f := &memFile{}
	ix, _ := Create(f, Options{KeyExpr: "K", KeySize: 4, KeyType: KeyTypeCharacter})
	in := []Entry{
		{Key: padKey("AA", 4), Recno: 1},
		{Key: padKey("BB", 4), Recno: 2},
		{Key: padKey("AA", 4), Recno: 3},
	}
	if err := ix.Build(in); err != nil {
		t.Fatalf("Build: %v", err)
	}
	f.pos = 0
	reopened, _ := Open(f)

	got, err := reopened.Seek(padKey("AA", 4))
	if err != nil {
		t.Fatalf("Seek: %v", err)
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Errorf("Seek(AA) = %v, want [1 3]", got)
	}
	if got, _ := reopened.Seek(padKey("ZZ", 4)); len(got) != 0 {
		t.Errorf("Seek(ZZ) = %v, want empty", got)
	}
	if _, err := reopened.Seek([]byte("toolong")); err == nil {
		t.Error("Seek with a wrong-width key succeeded; want ErrKeySize")
	}
}

// TestFirstLast covers the boundary accessors, including the
// empty case.
func TestFirstLast(t *testing.T) {
	f := &memFile{}
	ix, _ := Create(f, Options{KeyExpr: "K", KeySize: 4, KeyType: KeyTypeCharacter})

	if _, found, err := ix.First(); err != nil || found {
		t.Errorf("First on empty index: found=%v err=%v, want false/nil", found, err)
	}

	if err := ix.Build([]Entry{
		{Key: padKey("MM", 4), Recno: 2},
		{Key: padKey("AA", 4), Recno: 1},
		{Key: padKey("ZZ", 4), Recno: 3},
	}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	f.pos = 0
	reopened, _ := Open(f)

	first, found, err := reopened.First()
	if err != nil || !found {
		t.Fatalf("First: found=%v err=%v", found, err)
	}
	if got := string(bytes.TrimRight(first.Key, " ")); got != "AA" {
		t.Errorf("First = %q, want AA", got)
	}
	last, _, _ := reopened.Last()
	if got := string(bytes.TrimRight(last.Key, " ")); got != "ZZ" {
		t.Errorf("Last = %q, want ZZ", got)
	}
}

// TestCreateRejectsBadOptions covers the validation paths.
func TestCreateRejectsBadOptions(t *testing.T) {
	cases := map[string]Options{
		"zero key size":     {KeySize: 0, KeyType: KeyTypeCharacter},
		"oversize key":      {KeySize: MaxKeySize + 1, KeyType: KeyTypeCharacter},
		"numeric wrong len": {KeySize: 10, KeyType: KeyTypeNumeric},
		"unknown key type":  {KeySize: 8, KeyType: 99},
	}
	for name, opts := range cases {
		if _, err := Create(&memFile{}, opts); err == nil {
			t.Errorf("%s: Create succeeded, want rejection", name)
		}
	}
}

// TestOpenRejectsCorruptHeader verifies that a header disagreeing
// with itself is refused rather than misread. The record size is
// derivable from the key length, so a mismatch means the file is
// either corrupt or a format this package does not understand —
// reading on would misinterpret every entry.
func TestOpenRejectsCorruptHeader(t *testing.T) {
	f := loadFixture(t, "BYCODE.NDX")
	// Record size lives at offset 18; 20 is correct for a 10-byte
	// key, so 24 is not.
	f.data[18] = 24
	f.pos = 0
	if _, err := Open(f); err == nil {
		t.Error("Open accepted a header whose record size contradicts its key length")
	}
}

// TestBuildRejectsWrongWidthKeys guards the fixed-width contract.
func TestBuildRejectsWrongWidthKeys(t *testing.T) {
	f := &memFile{}
	ix, _ := Create(f, Options{KeyExpr: "K", KeySize: 8, KeyType: KeyTypeCharacter})
	err := ix.Build([]Entry{{Key: []byte("short"), Recno: 1}})
	if err == nil {
		t.Error("Build accepted a key of the wrong width")
	}
}
