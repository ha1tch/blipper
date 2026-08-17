package blipperfs

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ha1tch/blipper/cdx"
	"github.com/ha1tch/blipper/dbf"
	"github.com/ha1tch/blipper/fatfs"
)

// loadFATImage decompresses the fixture image into a writable
// in-memory buffer.
func loadFATImage(t *testing.T) *memFile {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "fat16.img.gz"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	defer zr.Close()
	data, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return &memFile{name: "fat16.img", data: data}
}

// TestFATImageAsTablespace is the point of the whole exercise: a
// complete xBase dataset — table, memo, catalogue, index — living
// inside a single FAT disk image, created and reopened through
// the ordinary blipperfs API with nothing FAT-specific in the
// calling code beyond the constructor.
func TestFATImageAsTablespace(t *testing.T) {
	img := loadFATImage(t)

	fs, err := FATImageRW(img)
	if err != nil {
		t.Fatalf("FATImageRW: %v", err)
	}
	s := NewSession(fs)

	spec := TableSpec{
		Schema: dbf.Schema{Fields: []dbf.Field{
			{Name: "CUSTOMER_C", Type: dbf.Character, Length: 10},
			{Name: "NOTES", Type: dbf.Memo, Length: 10},
		}},
		MemoFormat:    dbf.MemoFormatFPT,
		FPTBlockSize:  64,
		TableLongName: "customers",
		LongNames:     map[string]string{"CUSTOMER_C": "customer_code"},
		Tags: []cdx.TagSpec{{
			Name:    "BYCODE",
			KeyExpr: "CUSTOMER_C",
			KeyLen:  10,
			Entries: []cdx.Entry{
				{Key: []byte("ALPHA"), RecNo: 1},
				{Key: []byte("BRAVO"), RecNo: 2},
			},
		}},
	}

	area, err := s.CreateTable("CUST", "CUSTOMER", spec)
	if err != nil {
		t.Fatalf("CreateTable into FAT image: %v", err)
	}

	// Write a record with a memo through the ordinary Area API.
	rec := dbf.NewRecord(area.Table().Schema())
	rec.Set(area.Table().Schema(), "CUSTOMER_C", "ALPHA")
	if _, err := area.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := area.GoTop(); err != nil {
		t.Fatalf("GoTop: %v", err)
	}
	memo := []byte("a memo stored inside a FAT image")
	if err := area.MemoSet("NOTES", memo); err != nil {
		t.Fatalf("MemoSet: %v", err)
	}
	if err := area.Table().Flush(); err != nil {
		t.Fatalf("table Flush: %v", err)
	}

	// Commit the container. Without this the FAT and directory
	// updates live only in fatfs's cache.
	fl, ok := fs.(Flusher)
	if !ok {
		t.Fatal("FAT-backed FileSet does not implement Flusher")
	}
	if err := fl.Flush(); err != nil {
		t.Fatalf("FileSet Flush: %v", err)
	}

	// Every file landed inside the image.
	for _, want := range []string{"CUSTOMER.DBF", "CUSTOMER.FPT", "CUSTOMER.DBC", "CUSTOMER.CDX"} {
		if !fs.Exists(want) {
			t.Errorf("%s missing from image; contents are %v", want, fs.List())
		}
	}

	// Reopen the image from its mutated bytes and read everything
	// back through a fresh session.
	reopenedFS, err := FATImage(&memFile{data: img.data})
	if err != nil {
		t.Fatalf("reopen FAT image: %v", err)
	}
	s2, err := OpenFileSet(reopenedFS)
	if err != nil {
		t.Fatalf("OpenFileSet: %v", err)
	}
	area2, err := s2.Area("CUSTOMER")
	if err != nil {
		t.Fatalf("Area(CUSTOMER): %v", err)
	}

	// Every sibling resolved automatically, from inside the image.
	if area2.Memo() == nil {
		t.Error("memo sibling not resolved from FAT image")
	}
	if area2.Catalogue() == nil {
		t.Error("catalogue sibling not resolved from FAT image")
	}
	if area2.CDX() == nil {
		t.Error("CDX sibling not resolved from FAT image")
	}
	if got := area2.CatalogueLongName("CUSTOMER_C"); got != "customer_code" {
		t.Errorf("CatalogueLongName = %q, want customer_code", got)
	}

	// And the memo content survived the round trip through FAT.
	if err := area2.GoTop(); err != nil {
		t.Fatalf("GoTop: %v", err)
	}
	got, err := area2.MemoGet("NOTES")
	if err != nil {
		t.Fatalf("MemoGet: %v", err)
	}
	if !bytes.Equal(got, memo) {
		t.Errorf("memo content = %q, want %q", got, memo)
	}
}

// TestFATImageReadOnlyRefusesCreate confirms the safety default
// carries through the adapter.
func TestFATImageReadOnlyRefusesCreate(t *testing.T) {
	img := loadFATImage(t)
	fs, err := FATImage(img)
	if err != nil {
		t.Fatalf("FATImage: %v", err)
	}
	if _, err := fs.Create("NEW.DBF"); err == nil {
		t.Error("Create on read-only FAT image succeeded; want error")
	}
}

// TestFATImageForwardsOptions guards against the session layer
// being a strictly weaker interface than the driver beneath it.
// fatfs.OpenImage has always taken options; for a while
// blipperfs.FATImage called it with none and accepted none, so
// WithLongNames was unreachable for anyone using the session
// layer that exists precisely so callers need not touch fatfs.
func TestFATImageForwardsOptions(t *testing.T) {
	img := loadFATImage(t)
	fs, err := FATImageRW(img, fatfs.WithLongNames(true))
	if err != nil {
		t.Fatalf("FATImageRW: %v", err)
	}
	const name = "Long Name Through Session.DBF"
	f, err := fs.Create(name)
	if err != nil {
		t.Fatalf("Create(%q): %v", name, err)
	}
	if _, err := f.Write([]byte("payload")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	fs.(Flusher).Flush()

	var found bool
	for _, n := range fs.List() {
		if n == name {
			found = true
		}
	}
	if !found {
		t.Errorf("long name not listed; got %v — options were not forwarded", fs.List())
	}
}

// TestFATImageDefaultsToShortNames confirms forwarding did not
// change the default.
func TestFATImageDefaultsToShortNames(t *testing.T) {
	img := loadFATImage(t)
	fs, err := FATImageRW(img)
	if err != nil {
		t.Fatalf("FATImageRW: %v", err)
	}
	if _, err := fs.Create("Long Name No Option.DBF"); err == nil {
		// An 8.3-illegal name with long names disabled should be
		// refused rather than silently mangled.
		t.Error("a long name was accepted with long names disabled")
	}
}
