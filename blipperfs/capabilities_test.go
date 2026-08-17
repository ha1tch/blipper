package blipperfs

import (
	"path/filepath"
	"testing"

	"github.com/ha1tch/blipper/blipperdb"
	"github.com/ha1tch/blipper/dbf"
)

// TestCapabilityInterfacesAreReachable is the escape-hatch test.
// A caller who went through blipperfs must be able to reach the
// concrete backend without unwinding to the driver — otherwise
// the session layer is a strictly weaker interface than what it
// wraps, which is the failure T-22 fixed in one place and T-23
// in the other.
func TestCapabilityInterfacesAreReachable(t *testing.T) {
	t.Run("OSDir is DirBacked", func(t *testing.T) {
		dir := t.TempDir()
		fs := OSDir(dir)
		d, ok := fs.(DirBacked)
		if !ok {
			t.Fatal("OSDir does not implement DirBacked")
		}
		if d.Root() != dir {
			t.Errorf("Root() = %q, want %q", d.Root(), dir)
		}
		// And it is deliberately not Flusher: the operating
		// system already provides that guarantee.
		if _, ok := fs.(Flusher); ok {
			t.Error("OSDir implements Flusher; it should not")
		}
	})

	t.Run("FAT image is FATBacked and Flusher", func(t *testing.T) {
		fs, err := FATImageRW(loadFATImage(t))
		if err != nil {
			t.Fatalf("FATImageRW: %v", err)
		}
		v, ok := fs.(FATBacked)
		if !ok {
			t.Fatal("a FAT-backed FileSet does not implement FATBacked")
		}
		// Reaching FAT-specific detail is the point.
		if v.Volume().BytesPerCluster() == 0 {
			t.Error("Volume() returned something with no cluster size")
		}
		if _, ok := fs.(Flusher); !ok {
			t.Error("a FAT-backed FileSet does not implement Flusher")
		}
	})

	t.Run("SQLite tablespace is SQLiteBacked and Flusher", func(t *testing.T) {
		fs, err := SQLiteTablespace(filepath.Join(t.TempDir(), "c.db"))
		if err != nil {
			t.Fatalf("SQLiteTablespace: %v", err)
		}
		s, ok := fs.(SQLiteBacked)
		if !ok {
			t.Fatal("a SQLite-backed FileSet does not implement SQLiteBacked")
		}
		if s.Store().ChunkSize() == 0 {
			t.Error("Store() returned something with no chunk size")
		}
		if _, ok := fs.(Flusher); !ok {
			t.Error("a SQLite-backed FileSet does not implement Flusher")
		}
	})
}

// TestAllFourAccessLevelsWork exercises the layering the package
// documentation describes, so the claim is tested rather than
// merely asserted in a comment.
func TestAllFourAccessLevelsWork(t *testing.T) {
	spec := TableSpec{Schema: dbf.Schema{Fields: []dbf.Field{
		{Name: "CODE", Type: dbf.Character, Length: 10},
	}}}

	// Level 2 first, to seed a directory: custom backend, session
	// methods, no explicit db.
	dir := t.TempDir()
	s2 := NewSession(OSDir(dir))
	if _, err := s2.CreateTable("SEED", "SEED", spec); err != nil {
		t.Fatalf("level 2 CreateTable: %v", err)
	}

	// Level 1: a directory as a database, one call.
	s1, err := OpenDir(dir)
	if err != nil {
		t.Fatalf("level 1 OpenDir: %v", err)
	}
	if _, err := s1.Select("SEED"); err != nil {
		t.Fatalf("level 1 Select: %v", err)
	}

	// Level 3: own BlipperDB, package-level function.
	db := blipperdb.New()
	if _, err := Use(db, OSDir(dir), "OWN", "SEED.DBF"); err != nil {
		t.Fatalf("level 3 Use: %v", err)
	}

	// Level 4: no blipperfs at all — the format package on a
	// bare stream.
	h, err := OSDir(dir).Open("SEED.DBF")
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	tbl, err := dbf.Open(h)
	if err != nil {
		t.Fatalf("level 4 dbf.Open: %v", err)
	}
	if tbl.RecordCount() != 0 {
		t.Errorf("RecordCount = %d, want 0", tbl.RecordCount())
	}
}
