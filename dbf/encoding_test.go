package dbf

import "testing"

func TestParseCpgEncodingUTF8(t *testing.T) {
	for _, input := range []string{"UTF-8", "utf-8", "UTF8", "  UTF-8  \n"} {
		enc, err := ParseCpgEncoding([]byte(input))
		if err != nil {
			t.Fatalf("ParseCpgEncoding(%q): %v", input, err)
		}
		if enc.Source != EncodingSourceCpgSidecar {
			t.Errorf("%q: Source = %v, want EncodingSourceCpgSidecar", input, enc.Source)
		}
		if !enc.codec.identity() {
			t.Errorf("%q: codec is not identity, want it to be (Go strings are already UTF-8)", input)
		}
	}
}

// TestParseCpgEncodingISO88591NotSubstituted is the decisive
// regression test for the mistake caught before this shipped:
// ISO-8859-1 and Windows-1252 are NOT the same encoding — they
// differ in the 0x80-0x9F range — and an earlier draft of this
// file conflated them. 0x80 is EURO SIGN in Windows-1252 but a
// C1 control character in ISO-8859-1; this test fails if that
// substitution ever creeps back in.
func TestParseCpgEncodingISO88591NotSubstituted(t *testing.T) {
	for _, input := range []string{"ISO-8859-1", "LATIN1", "latin1"} {
		enc, err := ParseCpgEncoding([]byte(input))
		if err != nil {
			t.Fatalf("ParseCpgEncoding(%q): %v", input, err)
		}
		got, err := enc.codec.encode("\u20AC") // EURO SIGN
		if err == nil {
			t.Errorf("%q: encoding EURO SIGN succeeded (%v) — this is Windows-1252 behaviour, "+
				"not real ISO-8859-1, which has no EURO SIGN at all", input, got)
		}
	}
}

func TestParseCpgEncodingAliases(t *testing.T) {
	cases := []struct {
		input      string
		wantSource EncodingSource
	}{
		{"CP1252", EncodingSourceCpgSidecar},
		{"1252", EncodingSourceCpgSidecar},
		{"CP850", EncodingSourceCpgSidecar},
		{"850", EncodingSourceCpgSidecar},
		{"CP437", EncodingSourceCpgSidecar},
	}
	for _, c := range cases {
		enc, err := ParseCpgEncoding([]byte(c.input))
		if err != nil {
			t.Errorf("ParseCpgEncoding(%q): %v", c.input, err)
			continue
		}
		if enc.Source != c.wantSource {
			t.Errorf("%q: Source = %v, want %v", c.input, enc.Source, c.wantSource)
		}
	}
}

func TestParseCpgEncodingUnrecognized(t *testing.T) {
	if _, err := ParseCpgEncoding([]byte("SOME-MADE-UP-ENCODING")); err == nil {
		t.Error("expected an error for an unrecognized .cpg value, got nil")
	}
}

func TestTableEncodingReflectsOpenTier(t *testing.T) {
	schema := Schema{Fields: []Field{{Name: "F", Type: Character, Length: 1}}}
	buf := &memBuffer{}
	tbl, err := Create(buf, schema)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	enc := tbl.Encoding()
	if enc.Source != EncodingSourceIdentity {
		t.Errorf("fresh table: Encoding().Source = %v, want EncodingSourceIdentity", enc.Source)
	}
}

func TestSetEncodingIsExplicitOverride(t *testing.T) {
	schema := Schema{Fields: []Field{{Name: "F", Type: Character, Length: 1}}}
	buf := &memBuffer{}
	tbl, err := Create(buf, schema)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	utf8Enc, err := ParseCpgEncoding([]byte("UTF-8"))
	if err != nil {
		t.Fatalf("ParseCpgEncoding: %v", err)
	}
	tbl.SetEncoding(utf8Enc) // simulating what blipperfs would do

	if got := tbl.Encoding().Source; got != EncodingSourceCpgSidecar {
		t.Errorf("after simulated sidecar resolution: Source = %v, want EncodingSourceCpgSidecar", got)
	}

	if err := tbl.SetCodePage(CodePageIntl850); err != nil {
		t.Fatalf("SetCodePage: %v", err)
	}
	if got := tbl.Encoding().Source; got != EncodingSourceExplicitOverride {
		t.Errorf("after SetCodePage: Source = %v, want EncodingSourceExplicitOverride (last call wins)", got)
	}
}
