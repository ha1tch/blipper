package dbf

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDBaseIVMemoOracleContent reads the two memos in the live
// write-oracle's .DBT and confirms exact content — the same
// values REPLACE wrote via real dBASE 5.0 for DOS (source S13).
func TestDBaseIVMemoOracleContent(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "dbase5", "oracle", "TESTGEN.DBT"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	m, err := OpenDBaseIVMemo(f)
	if err != nil {
		t.Fatalf("OpenDBaseIVMemo: %v", err)
	}
	if got := m.TableName(); got != "TESTGEN" {
		t.Errorf("table name = %q, want TESTGEN", got)
	}
	if got := m.BlockSize(); got != 512 {
		t.Errorf("block size = %d, want 512", got)
	}

	cases := []struct {
		block uint32
		want  string
	}{
		{1, "first memo note"},
		{2, "second"},
	}
	for _, c := range cases {
		got, err := m.Get(c.block)
		if err != nil {
			t.Fatalf("Get(%d): %v", c.block, err)
		}
		if string(got) != c.want {
			t.Errorf("block %d = %q, want %q", c.block, got, c.want)
		}
	}
}

// TestDBaseIVMemoRealSpecimenMultiBlock reads a real 1994 vendor
// specimen (CLIENT.DBT) whose third record's memo is 580 bytes —
// longer than one 512-byte block's usable capacity — and confirms
// it decodes correctly across the block boundary: the header
// appears once, not per block. This is the decisive test for the
// multi-block assumption the format's doc comment describes.
func TestDBaseIVMemoRealSpecimenMultiBlock(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "dbase5", "full", "CLIENT.DBT"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	m, err := OpenDBaseIVMemo(f)
	if err != nil {
		t.Fatalf("OpenDBaseIVMemo: %v", err)
	}
	if got := m.TableName(); got != "CLIENT" {
		t.Errorf("table name = %q, want CLIENT", got)
	}

	// Record 3's memo starts at block 3 and is 580 bytes —
	// verified by direct inspection of the real file.
	text, err := m.Get(3)
	if err != nil {
		t.Fatalf("Get(3): %v", err)
	}
	if len(text) != 580 {
		t.Errorf("block 3 memo length = %d, want 580 (spanning past one block)", len(text))
	}
	// Spot-check content at both ends, since the boundary
	// crossing is exactly what this test exists to catch.
	wantPrefix := "86-155  06/31/86"
	wantSuffix := "1250.00 1\r\n"
	if len(text) >= len(wantPrefix) && string(text[:len(wantPrefix)]) != wantPrefix {
		t.Errorf("block 3 prefix = %q, want %q", text[:len(wantPrefix)], wantPrefix)
	}
	if len(text) >= len(wantSuffix) && string(text[len(text)-len(wantSuffix):]) != wantSuffix {
		t.Errorf("block 3 suffix = %q, want %q", text[len(text)-len(wantSuffix):], wantSuffix)
	}
}

// TestDBaseIVMemoEndToEnd exercises the full path a real caller
// would use: open the table, find its Memo field, read a record's
// pointer, and fetch the actual text through DBaseIVMemoFile —
// against real 1994 furniture-store purchase history data.
func TestDBaseIVMemoEndToEnd(t *testing.T) {
	tf, err := os.Open(filepath.Join("testdata", "dbase5", "full", "CLIENT.DBF"))
	if err != nil {
		t.Fatalf("open CLIENT.DBF: %v", err)
	}
	defer tf.Close()

	tbl, err := Open(tf)
	if err != nil {
		t.Fatalf("dbf.Open: %v", err)
	}
	if got := tbl.MemoFormat(); got != MemoFormatDBaseIV {
		t.Errorf("MemoFormat() = %v, want MemoFormatDBaseIV", got)
	}

	schema := tbl.Schema()
	var memoField string
	for _, fld := range schema.Fields {
		if fld.Type == Memo {
			memoField = fld.Name
			break
		}
	}
	if memoField == "" {
		t.Fatal("no Memo field found in CLIENT.DBF's schema")
	}

	mf, err := os.Open(filepath.Join("testdata", "dbase5", "full", "CLIENT.DBT"))
	if err != nil {
		t.Fatalf("open CLIENT.DBT: %v", err)
	}
	defer mf.Close()
	memo, err := OpenDBaseIVMemo(mf)
	if err != nil {
		t.Fatalf("OpenDBaseIVMemo: %v", err)
	}

	rec, err := tbl.Get(1)
	if err != nil {
		t.Fatalf("Get(1): %v", err)
	}
	v, err := rec.Get(schema, memoField)
	if err != nil {
		t.Fatalf("rec.Get(%s): %v", memoField, err)
	}
	ptrText, ok := v.(string)
	if !ok {
		t.Fatalf("memo field decoded as %T, want string", v)
	}
	block, has, err := ParseMemoPointer([]byte(ptrText))
	if err != nil {
		t.Fatalf("ParseMemoPointer: %v", err)
	}
	if !has {
		t.Fatal("record 1 has no memo, expected one")
	}
	text, err := memo.Get(block)
	if err != nil {
		t.Fatalf("memo.Get(%d): %v", block, err)
	}
	if len(text) == 0 {
		t.Error("record 1's memo text is empty, expected real content")
	}
}
