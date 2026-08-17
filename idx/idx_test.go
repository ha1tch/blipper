package idx

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

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
		g := make([]byte, end)
		copy(g, m.data)
		m.data = g
	}
	copy(m.data[m.pos:end], p)
	m.pos = end
	return len(p), nil
}
func (m *memFile) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case 0:
		m.pos = off
	case 1:
		m.pos += off
	case 2:
		m.pos = int64(len(m.data)) + off
	}
	return m.pos, nil
}

func pad(s string, w int) []byte {
	k := make([]byte, w)
	for i := range k {
		k[i] = ' '
	}
	copy(k, s)
	return k
}

// TestReadsClipperCompactIDX is the oracle test.
func TestReadsClipperCompactIDX(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "BYCODE.IDX"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ix, err := Open(&memFile{data: data})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if ix.KeySize() != 10 {
		t.Errorf("KeySize = %d, want 10", ix.KeySize())
	}
	if ix.Unique() {
		t.Error("Unique = true, want false")
	}
	entries, err := ix.Entries()
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	want := []struct {
		key   string
		recno uint32
	}{{"ALPHA", 2}, {"BRAVO", 4}, {"CHARLIE", 3}, {"DELTA", 1}}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(entries), len(want))
	}
	for i, w := range want {
		got := string(bytes.TrimRight(entries[i].Key, " "))
		if got != w.key || entries[i].RecNo != w.recno {
			t.Errorf("entry %d = %q/%d, want %q/%d", i, got, entries[i].RecNo, w.key, w.recno)
		}
	}
}

func TestBuildRoundTrip(t *testing.T) {
	f := &memFile{}
	ix, err := Create(f, Options{KeyExpr: "NAME", KeySize: 8})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	in := []Entry{
		{Key: pad("ZULU", 8), RecNo: 1},
		{Key: pad("ALFA", 8), RecNo: 2},
		{Key: pad("MIKE", 8), RecNo: 3},
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
	want := []string{"ALFA", "MIKE", "ZULU"}
	if len(entries) != len(want) {
		t.Fatalf("got %d, want %d", len(entries), len(want))
	}
	for i, w := range want {
		if got := string(bytes.TrimRight(entries[i].Key, " ")); got != w {
			t.Errorf("entry %d = %q, want %q", i, got, w)
		}
	}
}

func TestUniqueDropsDuplicates(t *testing.T) {
	f := &memFile{}
	ix, _ := Create(f, Options{KeyExpr: "K", KeySize: 4, Unique: true})
	if err := ix.Build([]Entry{
		{Key: pad("AA", 4), RecNo: 1},
		{Key: pad("AA", 4), RecNo: 2},
		{Key: pad("BB", 4), RecNo: 3},
	}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	f.pos = 0
	r, _ := Open(f)
	if !r.Unique() {
		t.Error("Unique flag lost on round trip")
	}
	entries, _ := r.Entries()
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
}

func TestSeek(t *testing.T) {
	f := &memFile{}
	ix, _ := Create(f, Options{KeyExpr: "K", KeySize: 4})
	ix.Build([]Entry{
		{Key: pad("AA", 4), RecNo: 1},
		{Key: pad("BB", 4), RecNo: 2},
		{Key: pad("AA", 4), RecNo: 3},
	})
	f.pos = 0
	r, _ := Open(f)
	got, err := r.Seek(pad("AA", 4))
	if err != nil {
		t.Fatalf("Seek: %v", err)
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Errorf("Seek(AA) = %v, want [1 3]", got)
	}
}

func TestOpenRejectsUncompact(t *testing.T) {
	data, _ := os.ReadFile(filepath.Join("testdata", "BYCODE.IDX"))
	data[14] = 0x00 // clear compact bit
	if _, err := Open(&memFile{data: data}); err != ErrNotCompact {
		t.Errorf("Open with cleared compact bit: err = %v, want ErrNotCompact", err)
	}
}

func TestBuildRejectsWrongWidthKeys(t *testing.T) {
	f := &memFile{}
	ix, _ := Create(f, Options{KeyExpr: "K", KeySize: 8})
	if err := ix.Build([]Entry{{Key: []byte("short"), RecNo: 1}}); err == nil {
		t.Error("Build accepted a wrong-width key")
	}
}
