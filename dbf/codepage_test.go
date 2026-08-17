package dbf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cpSchema() Schema {
	return Schema{Fields: []Field{
		{Name: "NAME", Type: Character, Length: 20},
	}}
}

// buildCPTable creates a table whose header declares a code page.
func buildCPTable(t *testing.T, cp CodePage) (*Table, *memFile) {
	t.Helper()
	file := &memFile{}
	if _, err := Create(file, cpSchema()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Byte 29 is the language driver. Set it and reopen so the
	// codec is built from the header the way a real file's would
	// be.
	file.data[29] = byte(cp)
	file.pos = 0
	reopened, err := Open(file)
	if err != nil {
		t.Fatalf("Open with code page 0x%02X: %v", byte(cp), err)
	}
	return reopened, file
}

// TestCodePageDefaultIsIdentity is the compatibility guarantee.
// Every Clipper file carries byte 29 = 0x00, because DBFNTX never
// wrote a language driver, and those files must decode exactly as
// they did before this feature existed.
func TestCodePageDefaultIsIdentity(t *testing.T) {
	file := &memFile{}
	tbl, err := Create(file, cpSchema())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := tbl.CodePage(); got != CodePageNone {
		t.Errorf("CodePage = %v, want none", got)
	}
	if !tbl.codec.identity() {
		t.Error("a table declaring no code page did not get the identity codec")
	}

	// High bytes must survive untouched.
	raw := "caf\xe9 \xff\xfe"
	rec := NewRecord(tbl.Schema())
	rec.Set(tbl.Schema(), "NAME", raw)
	if _, err := tbl.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := tbl.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	v, _ := got.Get(tbl.Schema(), "NAME")
	if v.(string) != raw {
		t.Errorf("identity codec altered bytes: got %q, want %q", v, raw)
	}
}

// TestCodePageDecodesDeclaredEncoding covers the core case: a
// file declaring CP850 has its high bytes interpreted, not passed
// through.
func TestCodePageDecodesDeclaredEncoding(t *testing.T) {
	tbl, _ := buildCPTable(t, CodePageIntl850)
	if got := tbl.CodePage(); got != CodePageIntl850 {
		t.Fatalf("CodePage = %v, want CP850", got)
	}
	if tbl.codec.identity() {
		t.Fatal("a table declaring CP850 got the identity codec")
	}

	// "MÜNCHEN" — Ü is 0x9A in CP850.
	rec := NewRecord(tbl.Schema())
	rec.Set(tbl.Schema(), "NAME", "MÜNCHEN")
	if _, err := tbl.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, err := tbl.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	v, _ := got.Get(tbl.Schema(), "NAME")
	if v.(string) != "MÜNCHEN" {
		t.Errorf("round trip through CP850: got %q, want %q", v, "MÜNCHEN")
	}
}

// TestCodePageWritesCorrectBytes verifies the encode side against
// the actual on-disk representation, not just a round trip
// through blipper. A round trip would pass even if both halves
// were wrong in the same way.
func TestCodePageWritesCorrectBytes(t *testing.T) {
	tbl, file := buildCPTable(t, CodePageIntl850)
	rec := NewRecord(tbl.Schema())
	rec.Set(tbl.Schema(), "NAME", "MÜNCHEN")
	if _, err := tbl.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Locate the record and check the stored byte for Ü.
	off := int(tbl.recordOffset(1)) + 1 // skip the deletion marker
	stored := file.data[off : off+7]
	// CP850: M=0x4D Ü=0x9A N=0x4E C=0x43 H=0x48 E=0x45 N=0x4E
	want := []byte{0x4D, 0x9A, 0x4E, 0x43, 0x48, 0x45, 0x4E}
	for i := range want {
		if stored[i] != want[i] {
			t.Errorf("stored byte %d = 0x%02X, want 0x%02X (CP850)", i, stored[i], want[i])
		}
	}
}

// TestSeveralCodePagesRoundTrip exercises the table across the
// encodings most likely to appear in real data.
func TestSeveralCodePagesRoundTrip(t *testing.T) {
	cases := map[CodePage]string{
		CodePageUS437:      "CAFÉ",
		CodePageIntl850:    "MÜNCHEN",
		CodePageWin1252:    "naïve façade",
		CodePageWin1250:    "Kraków",
		CodePageWin1251:    "Москва",
		CodePageRussian866: "Москва",
		CodePageGreek1253:  "Αθήνα",
	}
	for cp, text := range cases {
		t.Run(cp.String(), func(t *testing.T) {
			tbl, _ := buildCPTable(t, cp)
			rec := NewRecord(tbl.Schema())
			rec.Set(tbl.Schema(), "NAME", text)
			if _, err := tbl.Append(rec); err != nil {
				t.Fatalf("Append: %v", err)
			}
			got, err := tbl.Get(1)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			v, _ := got.Get(tbl.Schema(), "NAME")
			if v.(string) != text {
				t.Errorf("round trip: got %q, want %q", v, text)
			}
		})
	}
}

// TestUnrepresentableCharacterIsAnError verifies the write side
// fails loudly. Substituting '?' for a character the code page
// cannot hold would corrupt data in a way that looks like
// success.
func TestUnrepresentableCharacterIsAnError(t *testing.T) {
	tbl, _ := buildCPTable(t, CodePageUS437)
	rec := NewRecord(tbl.Schema())
	rec.Set(tbl.Schema(), "NAME", "日本語")
	if _, err := tbl.Append(rec); err == nil {
		t.Error("Append of a character CP437 cannot represent succeeded; want error")
	}
}

// TestUnsupportedCodePageIsReported checks that a file declaring
// an encoding blipper has no table for fails on Open, rather than
// being silently treated as identity.
func TestUnsupportedCodePageIsReported(t *testing.T) {
	file := &memFile{}
	if _, err := Create(file, cpSchema()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	file.data[29] = 0x67 // Icelandic CP861, named but unmapped
	file.pos = 0
	if _, err := Open(file); err == nil {
		t.Error("Open of a file declaring an unsupported code page succeeded; want error")
	}
}

// TestSetCodePageOverride covers the case the register called the
// common one for DOS-era Clipper data: a file declaring nothing
// that genuinely holds CP850 text.
func TestSetCodePageOverride(t *testing.T) {
	file := &memFile{}
	tbl, err := Create(file, cpSchema())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !tbl.codec.identity() {
		t.Fatal("expected identity codec before override")
	}
	if err := tbl.SetCodePage(CodePageIntl850); err != nil {
		t.Fatalf("SetCodePage: %v", err)
	}
	if tbl.codec.identity() {
		t.Error("codec still identity after override")
	}

	rec := NewRecord(tbl.Schema())
	rec.Set(tbl.Schema(), "NAME", "MÜNCHEN")
	if _, err := tbl.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, _ := tbl.Get(1)
	v, _ := got.Get(tbl.Schema(), "NAME")
	if v.(string) != "MÜNCHEN" {
		t.Errorf("override round trip: got %q, want %q", v, "MÜNCHEN")
	}

	// The override must not rewrite byte 29: what a file claims
	// about itself is a separate decision from how it is read.
	if file.data[29] != 0x00 {
		t.Errorf("SetCodePage rewrote byte 29 to 0x%02X; it should stay 0x00",
			file.data[29])
	}
}

func TestSetCodePageRejectsUnsupported(t *testing.T) {
	file := &memFile{}
	tbl, _ := Create(file, cpSchema())
	if err := tbl.SetCodePage(CodePage(0x67)); err == nil {
		t.Error("SetCodePage with an unmapped identifier succeeded; want error")
	}
}

// TestCorpusFilesStillDecodeIdentically guards the whole corpus
// against regression: every Clipper file declares no language
// driver, so none of them should be affected by this feature.
func TestCorpusFilesStillDecodeIdentically(t *testing.T) {
	path := filepath.Join("testdata", "UM.DBF")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("corpus fixture unavailable: %v", err)
	}
	tbl, err := Open(&memFile{data: data})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := tbl.CodePage(); got != CodePageNone {
		t.Errorf("corpus file declares code page %v; expected none", got)
	}
	if !tbl.codec.identity() {
		t.Error("corpus file did not get the identity codec")
	}
}

func TestCodePageStringAndSupported(t *testing.T) {
	if !CodePageNone.Supported() {
		t.Error("CodePageNone should report as supported: identity is a behaviour, not a gap")
	}
	if !CodePageIntl850.Supported() {
		t.Error("CP850 should be supported")
	}
	if CodePage(0x67).Supported() {
		t.Error("CP861 has no charmap table and should report unsupported")
	}
	if CodePage(0xEE).String() == "" {
		t.Error("an unknown identifier should still produce a message")
	}
}

// TestCodePageTableMatchesVFPDocumentation pins blipper's table to
// the authoritative list in "Code Pages Supported by Visual FoxPro"
// (VFP 9 SP2 Help; see docs/VFP30_FORMAT.md for provenance).
//
// The point is not that blipper must decode all 26 — three have no
// charmap table and are deliberately unmapped. The point is that
// every documented identifier must be *named*, so a file declaring
// one produces a meaningful message rather than "unknown 0x67", and
// so this table cannot silently drift from the specification again.
// It already had: T-21 shipped 16 identifiers assembled from
// secondary sources, and finding the real list showed nine missing.
func TestCodePageTableMatchesVFPDocumentation(t *testing.T) {
	documented := map[CodePage]string{
		0x01: "437", 0x02: "850", 0x03: "1252", 0x04: "10000",
		0x64: "852", 0x65: "866", 0x66: "865", 0x67: "861",
		0x68: "895", 0x69: "620", 0x6A: "737", 0x6B: "857",
		0x78: "950", 0x79: "949", 0x7A: "936", 0x7B: "932",
		0x7C: "874", 0x7D: "1255", 0x7E: "1256",
		0x96: "10007", 0x97: "10029", 0x98: "10006",
		0xC8: "1250", 0xC9: "1251", 0xCA: "1254", 0xCB: "1253",
	}
	for id, cp := range documented {
		name := id.String()
		if strings.HasPrefix(name, "unknown") {
			t.Errorf("identifier 0x%02X (CP%s) is documented by Microsoft but unnamed in blipper",
				byte(id), cp)
		}
	}
	// Guard the count too, so an addition here is deliberate.
	if len(documented) != 26 {
		t.Errorf("the documented list has %d entries, expected 26", len(documented))
	}
}

// TestCJKCodePagesDecode covers the four multi-byte encodings added
// in T-26. FoxPro shipped localised versions for these markets, so
// the files are not exotic; before this, blipper refused to open
// them at all.
func TestCJKCodePagesDecode(t *testing.T) {
	cases := map[CodePage]string{
		CodePageJapanese: "東京",
		CodePageChineseS: "北京",
		CodePageKorean:   "서울",
		CodePageChineseT: "臺北",
	}
	for cp, text := range cases {
		t.Run(cp.String(), func(t *testing.T) {
			if !cp.Supported() {
				t.Fatalf("%v reports unsupported", cp)
			}
			codec, err := newTextCodec(cp)
			if err != nil {
				t.Fatalf("newTextCodec: %v", err)
			}
			encoded, err := codec.encode(text)
			if err != nil {
				t.Fatalf("encode(%q): %v", text, err)
			}
			// Multi-byte: the stored form must differ from UTF-8,
			// or the codec is not doing anything.
			if string(encoded) == text {
				t.Errorf("encoded form equals the UTF-8 input; no conversion happened")
			}
			if got := codec.decode(encoded); got != text {
				t.Errorf("round trip: got %q, want %q", got, text)
			}
		})
	}
}
