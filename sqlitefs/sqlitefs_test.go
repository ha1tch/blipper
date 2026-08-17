package sqlitefs_test

import (
	"bytes"
	"database/sql"
	"errors"
	"io"
	"math/rand"
	"path/filepath"
	"testing"

	"github.com/ha1tch/blipper/sqlitefs"
	_ "modernc.org/sqlite"
)

func newFS(t *testing.T, opts ...sqlitefs.Option) *sqlitefs.FS {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	fs, err := sqlitefs.Open(path, opts...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { fs.Close() })
	return fs
}

func TestDefaultChunkSizeIsMeasuredValue(t *testing.T) {
	fs := newFS(t)
	if got := fs.ChunkSize(); got != sqlitefs.DefaultChunkSize {
		t.Errorf("ChunkSize = %d, want %d", got, sqlitefs.DefaultChunkSize)
	}
	if sqlitefs.DefaultChunkSize != 32*1024 {
		t.Errorf("DefaultChunkSize = %d, want 32768 (see bench/chunksize)",
			sqlitefs.DefaultChunkSize)
	}
}

func TestChunkSizeIsConfigurable(t *testing.T) {
	fs := newFS(t, sqlitefs.WithChunkSize(4096))
	if got := fs.ChunkSize(); got != 4096 {
		t.Errorf("ChunkSize = %d, want 4096", got)
	}
}

func TestRejectsUndersizedChunkSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.db")
	if _, err := sqlitefs.Open(path, sqlitefs.WithChunkSize(64)); err == nil {
		t.Error("Open with chunk size 64 succeeded; want rejection")
	}
}

// TestRoundTripAcrossChunkBoundaries writes payloads chosen to sit
// either side of the chunk boundary and reads each back.
func TestRoundTripAcrossChunkBoundaries(t *testing.T) {
	const cs = 1024
	fs := newFS(t, sqlitefs.WithChunkSize(cs))

	cases := map[string]int{
		"EMPTY.DAT":   0,
		"TINY.DAT":    1,
		"UNDER.DAT":   cs - 1,
		"EXACT.DAT":   cs,
		"OVER.DAT":    cs + 1,
		"MULTI.DAT":   cs*3 + 17,
		"BIGGISH.DAT": cs * 40,
	}
	rng := rand.New(rand.NewSource(3))
	payloads := map[string][]byte{}

	for name, size := range cases {
		data := make([]byte, size)
		rng.Read(data)
		payloads[name] = data

		f, err := fs.Create(name)
		if err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
		if _, err := f.Write(data); err != nil {
			t.Fatalf("Write(%s): %v", name, err)
		}
	}
	if err := fs.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	for name, want := range payloads {
		f, err := fs.Open(name)
		if err != nil {
			t.Errorf("Open(%s): %v", name, err)
			continue
		}
		got, err := io.ReadAll(f)
		if err != nil {
			t.Errorf("ReadAll(%s): %v", name, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: got %d bytes, want %d", name, len(got), len(want))
		}
		size, err := fs.Stat(name)
		if err != nil {
			t.Errorf("Stat(%s): %v", name, err)
			continue
		}
		if size != int64(len(want)) {
			t.Errorf("%s: Stat = %d, want %d", name, size, len(want))
		}
	}
}

// TestSizeAgreesWithChunks is the invariant the register calls
// for. files.size can drift from the chunks it describes — a
// failure mode the denormalised single-table shape did not have —
// so it is checked directly against the stored data after every
// operation that can change a length.
func TestSizeAgreesWithChunks(t *testing.T) {
	const cs = 512
	dir := t.TempDir()
	path := filepath.Join(dir, "sizes.db")
	fs, err := sqlitefs.Open(path, sqlitefs.WithChunkSize(cs))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	check := func(stage string) {
		t.Helper()
		if err := fs.Flush(); err != nil {
			t.Fatalf("%s: Flush: %v", stage, err)
		}
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatalf("%s: open for verification: %v", stage, err)
		}
		defer db.Close()

		rows, err := db.Query(`
			SELECT f.name, f.size, COUNT(c.idx), COALESCE(SUM(LENGTH(c.data)), 0)
			FROM files f LEFT JOIN chunks c ON c.file_id = f.id
			GROUP BY f.id`)
		if err != nil {
			t.Fatalf("%s: query: %v", stage, err)
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			var size, count, stored int64
			if err := rows.Scan(&name, &size, &count, &stored); err != nil {
				t.Fatalf("%s: scan: %v", stage, err)
			}
			// The chunks hold at least the logical size and at
			// most one chunk more, since the final chunk is
			// stored whole.
			minStored := size
			maxStored := count * int64(cs)
			if stored < minStored || stored > maxStored {
				t.Errorf("%s: %s has size=%d but %d bytes across %d chunks (expected %d..%d)",
					stage, name, size, stored, count, minStored, maxStored)
			}
			// A file's chunk count must cover its size.
			wantChunks := (size + int64(cs) - 1) / int64(cs)
			if count < wantChunks {
				t.Errorf("%s: %s has size=%d needing %d chunks but only %d present",
					stage, name, size, wantChunks, count)
			}
		}
	}

	f, _ := fs.Create("A.DAT")
	f.Write(bytes.Repeat([]byte("x"), 1500))
	check("after initial write")

	// A truncating Create must reset size and drop chunks.
	f2, _ := fs.Create("A.DAT")
	check("after truncating Create")
	if size, _ := fs.Stat("A.DAT"); size != 0 {
		t.Errorf("after truncating Create, size = %d, want 0", size)
	}

	// A write extending the last chunk must grow size.
	f2.Write(bytes.Repeat([]byte("y"), 700))
	check("after write extending last chunk")
	if size, _ := fs.Stat("A.DAT"); size != 700 {
		t.Errorf("size = %d, want 700", size)
	}

	// An in-place overwrite must not change size.
	f3, _ := fs.Open("A.DAT")
	f3.Seek(100, io.SeekStart)
	f3.Write([]byte("zzzz"))
	check("after in-place overwrite")
	if size, _ := fs.Stat("A.DAT"); size != 700 {
		t.Errorf("after in-place overwrite, size = %d, want 700", size)
	}

	fs.Close()
}

// TestSeekAndPartialWrite covers the read-modify-write path for
// ranges that do not align to chunk boundaries.
func TestSeekAndPartialWrite(t *testing.T) {
	const cs = 512
	fs := newFS(t, sqlitefs.WithChunkSize(cs))

	original := bytes.Repeat([]byte("a"), cs*3)
	f, _ := fs.Create("P.DAT")
	f.Write(original)

	// Overwrite a range straddling two chunk boundaries.
	f2, _ := fs.Open("P.DAT")
	if _, err := f2.Seek(cs-5, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	patch := bytes.Repeat([]byte("b"), 10)
	if _, err := f2.Write(patch); err != nil {
		t.Fatalf("Write: %v", err)
	}
	fs.Flush()

	want := make([]byte, len(original))
	copy(want, original)
	copy(want[cs-5:], patch)

	f3, _ := fs.Open("P.DAT")
	got, err := io.ReadAll(f3)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Error("partial write across a chunk boundary did not round trip")
	}
}

// TestRemoveCascades verifies that deleting a file removes its
// chunks, which is what ON DELETE CASCADE plus foreign_keys=ON
// buys.
func TestRemoveCascades(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rm.db")
	fs, err := sqlitefs.Open(path, sqlitefs.WithChunkSize(512))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	f, _ := fs.Create("GONE.DAT")
	f.Write(bytes.Repeat([]byte("x"), 4000))
	fs.Flush()

	if err := fs.Remove("GONE.DAT"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	fs.Flush()

	if fs.Exists("GONE.DAT") {
		t.Error("file still present after Remove")
	}
	fs.Close()

	db, _ := sql.Open("sqlite", path)
	defer db.Close()
	var orphans int
	if err := db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&orphans); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	if orphans != 0 {
		t.Errorf("%d orphaned chunks after Remove; ON DELETE CASCADE did not fire", orphans)
	}
}

// TestCaseInsensitiveNames verifies the COLLATE NOCASE behaviour
// that matches DOS-era conventions.
func TestCaseInsensitiveNames(t *testing.T) {
	fs := newFS(t)
	f, _ := fs.Create("DATA.DBF")
	f.Write([]byte("content"))
	fs.Flush()

	for _, variant := range []string{"data.dbf", "Data.Dbf", "DATA.DBF"} {
		if !fs.Exists(variant) {
			t.Errorf("Exists(%q) = false, want true", variant)
		}
	}
}

func TestNotFoundIsTyped(t *testing.T) {
	fs := newFS(t)
	if _, err := fs.Open("NOSUCH.DAT"); !errors.Is(err, sqlitefs.ErrNotFound) {
		t.Errorf("Open of missing file: err = %v, want ErrNotFound", err)
	}
}

// TestFlushIsTheCommitBoundary verifies the property this package
// exists for: writes are not durable until Flush, and a set of
// files written together commits together.
func TestFlushIsTheCommitBoundary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commit.db")
	fs, err := sqlitefs.Open(path, sqlitefs.WithChunkSize(512))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Write three files without flushing.
	for _, name := range []string{"A.DBF", "A.CDX", "A.FPT"} {
		f, err := fs.Create(name)
		if err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
		if _, err := f.Write([]byte("payload")); err != nil {
			t.Fatalf("Write(%s): %v", name, err)
		}
	}

	// A separate connection must see none of them yet.
	db, _ := sql.Open("sqlite", path)
	var before int
	if err := db.QueryRow("SELECT COUNT(*) FROM files").Scan(&before); err != nil {
		t.Fatalf("count before flush: %v", err)
	}
	db.Close()
	if before != 0 {
		t.Errorf("%d files visible before Flush, want 0", before)
	}

	if err := fs.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	db2, _ := sql.Open("sqlite", path)
	defer db2.Close()
	var after int
	if err := db2.QueryRow("SELECT COUNT(*) FROM files").Scan(&after); err != nil {
		t.Fatalf("count after flush: %v", err)
	}
	if after != 3 {
		t.Errorf("%d files visible after Flush, want 3", after)
	}
	fs.Close()
}
