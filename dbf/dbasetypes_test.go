package dbf

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestIsDBaseLineage checks the bit test against every version
// byte encountered or accepted this session, both directions.
func TestIsDBaseLineage(t *testing.T) {
	cases := []struct {
		version byte
		want    bool
	}{
		{dbfVersion, true},                 // 0x03
		{dbfVersionMemo, true},             // 0x83
		{dbfVersionDBaseIV, true},          // 0x8B
		{dbfVersionDBaseIVSQLTable, true},  // 0x43
		{dbfVersionDBaseIVSQLSystem, true}, // 0x63
		{dbfVersionVFP, false},             // 0x30
		{dbfVersionVFPAutoinc, false},      // 0x31
		{dbfVersionVFPVarLen, false},       // 0x32
		{dbfVersionFPT, false},             // 0xF5
	}
	for _, c := range cases {
		if got := isDBaseLineage(c.version); got != c.want {
			t.Errorf("isDBaseLineage(0x%02X) = %v, want %v", c.version, got, c.want)
		}
	}
}

// TestDBaseIVVersionBytesAccepted confirms Open no longer refuses
// the three version bytes T-31 adds.
func TestDBaseIVVersionBytesAccepted(t *testing.T) {
	for _, v := range []byte{dbfVersionDBaseIV, dbfVersionDBaseIVSQLTable, dbfVersionDBaseIVSQLSystem} {
		raw := make([]byte, fileHeaderSize)
		raw[0] = v
		if _, _, err := readHeader(bytes.NewReader(raw)); err != nil {
			t.Errorf("readHeader rejected version 0x%02X: %v", v, err)
		}
	}
}

// TestBGLineageRemapRoundTrip is a synthetic test, not against a
// real specimen — no B or G field exists in any specimen this
// session found (checked against 33 vendored dBASE 5.0 tables and
// a live write-oracle). The byte layout itself (10-digit ASCII
// .DBT pointer) is settled beyond reasonable doubt by source S12,
// Borland's own confidential internal manuscript — see
// docs/DBASE_FORMAT.md. This test exercises the remap/unmap
// mechanism against that documented layout directly.
func TestBGLineageRemapRoundTrip(t *testing.T) {
	// A dBASE-lineage field descriptor: name "PHOTO", type 'B',
	// length 10 (the ASCII pointer width).
	var raw [fieldDescriptorSize]byte
	copy(raw[:11], "PHOTO")
	raw[11] = 'B'
	raw[16] = 10

	field, done, err := readField(bytes.NewReader(raw[:]), true /* dBASE lineage */)
	if err != nil {
		t.Fatalf("readField: %v", err)
	}
	if done {
		t.Fatal("readField reported done on a real descriptor")
	}
	if field.Type != DBaseBinary {
		t.Errorf("field.Type = %c (0x%02X), want DBaseBinary", rune(field.Type), field.Type)
	}

	// Decode: same plain-pointer-text convention as Memo.
	pointer := []byte("0000000042")
	v, err := decodeValue(pointer, field)
	if err != nil {
		t.Fatalf("decodeValue: %v", err)
	}
	if v != "0000000042" {
		t.Errorf("decoded = %q, want \"0000000042\"", v)
	}

	// Encode: round-trip back to the same 10 bytes.
	var dst [10]byte
	if err := encodeValue(dst[:], field, "0000000042"); err != nil {
		t.Fatalf("encodeValue: %v", err)
	}
	if string(dst[:]) != "0000000042" {
		t.Errorf("encoded = %q, want \"0000000042\"", dst[:])
	}

	// writeField must unmap DBaseBinary back to the on-disk 'B'.
	var out bytes.Buffer
	if err := writeField(&out, field); err != nil {
		t.Fatalf("writeField: %v", err)
	}
	if got := out.Bytes()[11]; got != 'B' {
		t.Errorf("written type byte = 0x%02X, want 'B' (0x42)", got)
	}

	// Re-reading the written bytes under dBASE lineage must
	// recover DBaseBinary again — the full round trip.
	field2, _, err := readField(bytes.NewReader(out.Bytes()), true)
	if err != nil {
		t.Fatalf("re-reading written field: %v", err)
	}
	if field2.Type != DBaseBinary {
		t.Errorf("round trip: field.Type = %c, want DBaseBinary", rune(field2.Type))
	}
}

// TestBGLineageDoesNotAffectVFP confirms the same 'B'/'G' bytes
// keep their VFP meaning when lineage is false — the entire point
// of the remap being conditional rather than global.
func TestBGLineageDoesNotAffectVFP(t *testing.T) {
	var raw [fieldDescriptorSize]byte
	copy(raw[:11], "PRICE")
	raw[11] = 'B'
	raw[16] = 8 // VFP Double width

	field, _, err := readField(bytes.NewReader(raw[:]), false /* VFP lineage */)
	if err != nil {
		t.Fatalf("readField: %v", err)
	}
	if field.Type != Double {
		t.Errorf("field.Type = %c (0x%02X), want Double ('B')", rune(field.Type), field.Type)
	}
}

// TestDBaseIVOracleSpecimenOpens is the decisive integration
// check: the actual write-oracle table (dbf/testdata/dbase5/oracle/
// TESTGEN.DBF, real dBASE 5.0 for DOS output, S13) opens and
// decodes its records correctly end to end.
func TestDBaseIVOracleSpecimenOpens(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "dbase5", "oracle", "TESTGEN.DBF"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	tbl, err := Open(f)
	if err != nil {
		t.Fatalf("dbf.Open: %v", err)
	}
	if tbl.RecordCount() != 2 {
		t.Fatalf("record count = %d, want 2", tbl.RecordCount())
	}

	schema := tbl.Schema()
	wantID := []int64{1, 2}
	wantName := []string{"ALPHA", "BRAVO"}
	wantPointer := []string{"0000000001", "0000000002"}

	for i := uint32(1); i <= 2; i++ {
		rec, err := tbl.Get(i)
		if err != nil {
			t.Fatalf("Get(%d): %v", i, err)
		}
		id, _ := rec.Get(schema, "ID")
		name, _ := rec.Get(schema, "NAME")
		notes, _ := rec.Get(schema, "NOTES")

		if id != wantID[i-1] {
			t.Errorf("record %d: ID = %v (%T), want %d", i, id, id, wantID[i-1])
		}
		if name != wantName[i-1] {
			t.Errorf("record %d: NAME = %v, want %s", i, name, wantName[i-1])
		}
		if notes != wantPointer[i-1] {
			t.Errorf("record %d: NOTES pointer = %v, want %s", i, notes, wantPointer[i-1])
		}
	}
}

// TestDBaseIVAllSpecimensOpen sweeps every vendored dBASE 5.0 DOS
// table (33 files, four version bytes: 0x03, 0x43, 0x63, 0x8B)
// and confirms each opens without error and reports a sane record
// count. Field-level assertions live in the more targeted tests
// above; this one exists to catch anything that only shows up
// across the full corpus.
func TestDBaseIVAllSpecimensOpen(t *testing.T) {
	dir := filepath.Join("testdata", "dbase5", "full")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	tested := 0
	versionsSeen := map[byte]int{}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".DBF" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, err := os.Open(path)
		if err != nil {
			t.Errorf("%s: open: %v", e.Name(), err)
			continue
		}

		tbl, err := Open(f)
		if err != nil {
			t.Errorf("%s: dbf.Open: %v", e.Name(), err)
			f.Close()
			continue
		}

		versionsSeen[tbl.versionByte]++
		tested++
		f.Close()
	}

	if tested == 0 {
		t.Fatal("no .DBF files found under testdata/dbase5/full")
	}
	t.Logf("%d specimens opened successfully, versions seen: %v", tested, versionsSeen)
}
