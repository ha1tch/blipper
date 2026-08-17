package dbf

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// The fixtures in testdata were written by Clipper 5.2e under the
// oracle in docs/CLIPPER_ORACLE.md, from this program:
//
//	CREATE FMEMO FROM FSTRUCT       && CODE C(8), NOTE M(10)
//	APPEND BLANK
//	REPLACE CODE WITH "SHORT", NOTE WITH "Hello memo."
//	APPEND BLANK
//	REPLACE CODE WITH "EMPTY", NOTE WITH ""
//	APPEND BLANK
//	REPLACE CODE WITH "SPAN",  NOTE WITH REPLICATE("X", 700)
//	APPEND BLANK
//	REPLACE CODE WITH "CTRL",  NOTE WITH "before" + CHR(26) + "after"
//	APPEND BLANK
//	REPLACE CODE WITH "LAST",  NOTE WITH "tail memo"
//
// Register item T-02.

func openFixture(t *testing.T, name string) *os.File {
	t.Helper()

	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatalf("opening fixture: %v", err)
	}

	t.Cleanup(func() { f.Close() })

	return f
}

// A table written with a memo field carries version byte 0x83, which
// Open must accept rather than reject as unsupported.
func TestOpenAcceptsMemoVersion(t *testing.T) {
	table, err := Open(openFixture(t, "FMEMO.DBF"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if got := table.RecordCount(); got != 5 {
		t.Errorf("RecordCount = %d, want 5", got)
	}

	schema := table.Schema()
	if !schema.HasMemo() {
		t.Error("schema does not report a memo field")
	}
}

func TestMemoReadsClipperText(t *testing.T) {
	table, err := Open(openFixture(t, "FMEMO.DBF"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	memo, err := OpenMemo(openFixture(t, "FMEMO.DBT"))
	if err != nil {
		t.Fatalf("OpenMemo: %v", err)
	}

	schema := table.Schema()

	cases := []struct {
		code    string
		want    string
		present bool
	}{
		{"SHORT", "Hello memo.", true},
		{"EMPTY", "", false},
		{"SPAN", strings.Repeat("X", 700), true},
		{"CTRL", "before\x1aafter", true},
		{"LAST", "tail memo", true},
	}

	for i, c := range cases {
		record, err := table.Get(uint32(i + 1))
		if err != nil {
			t.Fatalf("Get(%d): %v", i+1, err)
		}

		code, err := record.Get(schema, "CODE")
		if err != nil {
			t.Fatalf("Get CODE: %v", err)
		}

		if got := strings.TrimSpace(code.(string)); got != c.code {
			t.Fatalf("record %d is %q, expected %q", i+1, got, c.code)
		}

		raw, err := record.Get(schema, "NOTE")
		if err != nil {
			t.Fatalf("Get NOTE: %v", err)
		}

		block, present, err := ParseMemoPointer([]byte(raw.(string)))
		if err != nil {
			t.Fatalf("%s: ParseMemoPointer: %v", c.code, err)
		}

		if present != c.present {
			t.Fatalf("%s: memo present = %v, want %v", c.code, present, c.present)
		}

		if !present {
			continue
		}

		text, err := memo.Get(block)
		if err != nil {
			t.Fatalf("%s: memo.Get(%d): %v", c.code, block, err)
		}

		if string(text) != c.want {
			t.Errorf("%s: memo = %q (%d bytes), want %q (%d bytes)",
				c.code, truncate(string(text)), len(text),
				truncate(c.want), len(c.want))
		}
	}
}

// The terminator is a 0x1A pair. Memo text may itself contain a lone
// 0x1A, and a reader that stops at the first one truncates the memo:
// the CTRL fixture stores "before\x1aafter", where the embedded byte
// precedes the real terminator.
func TestMemoTextMayContainTerminatorByte(t *testing.T) {
	memo, err := OpenMemo(openFixture(t, "FMEMO.DBT"))
	if err != nil {
		t.Fatalf("OpenMemo: %v", err)
	}

	// CTRL is the fourth record, whose memo Clipper placed at block 5.
	text, err := memo.Get(5)
	if err != nil {
		t.Fatalf("memo.Get: %v", err)
	}

	if !bytes.Contains(text, []byte{0x1A}) {
		t.Fatal("fixture no longer contains an embedded 0x1A; test is void")
	}

	if string(text) != "before\x1aafter" {
		t.Errorf("memo = %q, want %q", text, "before\x1aafter")
	}
}

func TestMemoRoundTrip(t *testing.T) {
	file := &memFile{}

	memo, err := CreateMemo(file)
	if err != nil {
		t.Fatalf("CreateMemo: %v", err)
	}

	texts := [][]byte{
		[]byte("first"),
		[]byte(strings.Repeat("Y", 1200)),
		[]byte("has \x1a inside"),
		[]byte(""),
	}

	blocks := make([]uint32, len(texts))

	for i, text := range texts {
		block, err := memo.Append(text)
		if err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}

		blocks[i] = block
	}

	// Reopen to prove the header was persisted, not just held in memory.
	reopened, err := OpenMemo(file)
	if err != nil {
		t.Fatalf("OpenMemo: %v", err)
	}

	if reopened.NextFree() != memo.NextFree() {
		t.Errorf("NextFree after reopen = %d, want %d",
			reopened.NextFree(), memo.NextFree())
	}

	for i, want := range texts {
		got, err := reopened.Get(blocks[i])
		if err != nil {
			t.Fatalf("Get(%d): %v", blocks[i], err)
		}

		if !bytes.Equal(got, want) {
			t.Errorf("memo %d = %q, want %q", i, truncate(string(got)), truncate(string(want)))
		}
	}
}

// Memos must not overlap: a memo long enough to span blocks has to
// advance the free pointer by every block it occupies.
func TestMemoAllocationDoesNotOverlap(t *testing.T) {
	memo, err := CreateMemo(&memFile{})
	if err != nil {
		t.Fatalf("CreateMemo: %v", err)
	}

	first, err := memo.Append([]byte(strings.Repeat("A", 700)))
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	second, err := memo.Append([]byte("B"))
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	// 700 bytes plus a 2-byte terminator needs two blocks.
	if second != first+2 {
		t.Errorf("second memo at block %d, want %d (first + 2)", second, first+2)
	}

	text, err := memo.Get(second)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if string(text) != "B" {
		t.Errorf("second memo = %q, want %q", text, "B")
	}
}

func TestMemoPointerFormat(t *testing.T) {
	cases := []struct {
		block uint32
		want  string
	}{
		{0, "          "},
		{2, "         2"},
		{34, "        34"},
		{123456, "    123456"},
	}

	for _, c := range cases {
		if got := string(FormatMemoPointer(c.block)); got != c.want {
			t.Errorf("FormatMemoPointer(%d) = %q, want %q", c.block, got, c.want)
		}
	}

	// A blank field means no memo; block 0 is the header and is never
	// a valid pointer, so a stored 0 means the same thing.
	for _, raw := range []string{"          ", "         0"} {
		_, present, err := ParseMemoPointer([]byte(raw))
		if err != nil {
			t.Fatalf("ParseMemoPointer(%q): %v", raw, err)
		}

		if present {
			t.Errorf("ParseMemoPointer(%q) reported a memo", raw)
		}
	}

	if _, _, err := ParseMemoPointer([]byte("      abc ")); err == nil {
		t.Error("ParseMemoPointer accepted a non-numeric pointer")
	}
}

// A table created with a memo field must advertise it in the version
// byte, and must keep doing so after a header rewrite.
func TestCreateSetsMemoVersionByte(t *testing.T) {
	file := &memFile{}

	schema := Schema{Fields: []Field{
		{Name: "CODE", Type: Character, Length: 8},
		{Name: "NOTE", Type: Memo, Length: 10},
	}}

	table, err := Create(file, schema)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got := versionByte(t, file); got != 0x83 {
		t.Errorf("version byte after Create = 0x%02X, want 0x83", got)
	}

	// Appending rewrites the header; the memo bit must survive.
	if _, err := table.Append(NewRecord(schema)); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if got := versionByte(t, file); got != 0x83 {
		t.Errorf("version byte after Append = 0x%02X, want 0x83", got)
	}

	// A memoless table must not gain the bit.
	plain := &memFile{}

	if _, err := Create(plain, Schema{Fields: []Field{
		{Name: "CODE", Type: Character, Length: 8},
	}}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got := versionByte(t, plain); got != 0x03 {
		t.Errorf("memoless version byte = 0x%02X, want 0x03", got)
	}
}

func versionByte(t *testing.T, f *memFile) byte {
	t.Helper()

	if _, err := f.Seek(0, 0); err != nil {
		t.Fatalf("Seek: %v", err)
	}

	var b [1]byte

	if _, err := f.Read(b[:]); err != nil {
		t.Fatalf("Read: %v", err)
	}

	return b[0]
}

func truncate(s string) string {
	if len(s) <= 40 {
		return s
	}

	return s[:40] + "..."
}
