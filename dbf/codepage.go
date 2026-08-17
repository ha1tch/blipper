package dbf

import (
	"fmt"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// Code page support.
//
// Byte 29 of the DBF header carries a language-driver identifier
// naming the character encoding of the file's text. Blipper has
// read that byte into Header.CodePage since the beginning and
// done nothing with it, returning raw bytes as Go strings. That
// is correct for pure ASCII and wrong for everything else: a
// CP850 file containing "MÜNCHEN" decodes to mojibake, and
// writing it back unchanged is only safe by accident.
//
// The identifiers below are Microsoft's, as documented for the
// Visual FoxPro header. dBASE and Clipper use the same byte with
// the same values where they set it at all — DBFNTX does not,
// which is why every file in the Clipper corpus carries 0x00.
//
// Three behaviours are wanted and all three are distinct:
//
//   - a file declaring a code page is decoded accordingly;
//   - a file declaring nothing is left alone, byte for byte,
//     because guessing would be worse than not trying;
//   - a caller who knows better than the header can say so.

// CodePage is a DBF language-driver identifier.
type CodePage byte

// Language driver identifiers. This is the set for which a
// charmap encoding exists; identifiers outside it are reported
// as unsupported rather than silently ignored, so a caller
// learns that their file says something blipper cannot honour.
const (
	CodePageNone        CodePage = 0x00 // no language driver declared
	CodePageUS437       CodePage = 0x01 // U.S. MS-DOS
	CodePageIntl850     CodePage = 0x02 // International MS-DOS
	CodePageWin1252     CodePage = 0x03 // Windows ANSI, Western European
	CodePageMacRoman    CodePage = 0x04 // Standard Macintosh
	CodePageEE852       CodePage = 0x64 // Eastern European MS-DOS
	CodePageRussian866  CodePage = 0x65 // Russian MS-DOS
	CodePageNordic865   CodePage = 0x66 // Nordic MS-DOS
	CodePageIcelandic   CodePage = 0x67 // Icelandic MS-DOS
	CodePageGreek737    CodePage = 0x6A // Greek MS-DOS (437G)
	CodePageTurkish857  CodePage = 0x6B // Turkish MS-DOS
	CodePageThai874     CodePage = 0x7C // Thai Windows
	CodePageHebrew1255  CodePage = 0x7D // Hebrew Windows
	CodePageArabic1256  CodePage = 0x7E // Arabic Windows
	CodePageMazovia     CodePage = 0x69 // Mazovia (Polish) MS-DOS
	CodePageKamenicky   CodePage = 0x68 // Kamenicky (Czech) MS-DOS
	CodePageJapanese    CodePage = 0x7B // Japanese Windows (932)
	CodePageChineseS    CodePage = 0x7A // Chinese Simplified Windows (936)
	CodePageKorean      CodePage = 0x79 // Korean Windows (949)
	CodePageChineseT    CodePage = 0x78 // Traditional Chinese Windows (950)
	CodePageMacGreek    CodePage = 0x98 // Greek Macintosh (10006)
	CodePageMacCyril    CodePage = 0x96 // Russian Macintosh (10007)
	CodePageMacEE       CodePage = 0x97 // Macintosh EE (10029)
	CodePageWin1250     CodePage = 0xC8 // Central European Windows
	CodePageWin1251     CodePage = 0xC9 // Russian Windows
	CodePageTurkish1254 CodePage = 0xCA // Turkish Windows
	CodePageGreek1253   CodePage = 0xCB // Greek Windows

	// CodePageDBaseIVUnknown is dBASE IV/5.0's own language
	// driver byte — confirmed twice independently as this
	// lineage's real, common value (the original 1994 vendor
	// specimens at dbf/testdata/dbase5/ and a live 2026
	// write-oracle at dbf/testdata/dbase5/oracle/ both carry it),
	// but no primary source found anywhere this session describes
	// which specific OEM encoding it denotes. See newTextCodec:
	// treated as identity rather than refusing to open an
	// otherwise fully legitimate table, the same choice already
	// made for CodePageNone. T-31.
	CodePageDBaseIVUnknown CodePage = 0x1B
)

// codePages maps identifiers to encodings. Absence means blipper
// has no table for that identifier, which is reported rather
// than guessed at.
//
// Three identifiers are named above but absent here — CP861
// (Icelandic), CP857 (Turkish MS-DOS), and CP737 (Greek MS-DOS) —
// because golang.org/x/text/encoding/charmap has no table for
// them. They are named so a file declaring one produces a
// meaningful message rather than "unknown 0x67", and left
// unmapped rather than substituted with a near neighbour: CP865
// is close to CP861 and would decode most of a file correctly,
// which is exactly the kind of nearly-right that hides a
// problem.
var codePages = map[CodePage]encoding.Encoding{
	CodePageUS437:       charmap.CodePage437,
	CodePageIntl850:     charmap.CodePage850,
	CodePageWin1252:     charmap.Windows1252,
	CodePageMacRoman:    charmap.Macintosh,
	CodePageEE852:       charmap.CodePage852,
	CodePageRussian866:  charmap.CodePage866,
	CodePageNordic865:   charmap.CodePage865,
	CodePageThai874:     charmap.Windows874,
	CodePageHebrew1255:  charmap.Windows1255,
	CodePageArabic1256:  charmap.Windows1256,
	CodePageWin1250:     charmap.Windows1250,
	CodePageWin1251:     charmap.Windows1251,
	CodePageTurkish1254: charmap.Windows1254,
	CodePageGreek1253:   charmap.Windows1253,

	// CJK encodings are multi-byte and live in their own
	// x/text packages rather than charmap. FoxPro shipped
	// localised versions for these markets and the files are
	// not exotic; refusing to open one was a worse outcome
	// than the gap T-21 was filed to close.
	CodePageJapanese: japanese.ShiftJIS,
	CodePageChineseS: simplifiedchinese.GBK,
	CodePageKorean:   korean.EUCKR,
	CodePageChineseT: traditionalchinese.Big5,

	// Macintosh variants beyond Roman.
	CodePageMacCyril: charmap.MacintoshCyrillic,
}

// codePageNames gives each identifier a readable name, for
// diagnostics and error messages.
var codePageNames = map[CodePage]string{
	CodePageNone:           "none",
	CodePageDBaseIVUnknown: "dBASE IV/5.0 language driver (encoding unknown, treated as identity)",
	CodePageUS437:          "CP437 (U.S. MS-DOS)",
	CodePageIntl850:        "CP850 (International MS-DOS)",
	CodePageWin1252:        "Windows-1252 (Western European)",
	CodePageMacRoman:       "Macintosh Roman",
	CodePageEE852:          "CP852 (Eastern European MS-DOS)",
	CodePageRussian866:     "CP866 (Russian MS-DOS)",
	CodePageNordic865:      "CP865 (Nordic MS-DOS)",
	CodePageIcelandic:      "CP861 (Icelandic MS-DOS)",
	CodePageGreek737:       "CP737 (Greek MS-DOS)",
	CodePageTurkish857:     "CP857 (Turkish MS-DOS)",
	CodePageThai874:        "Windows-874 (Thai)",
	CodePageHebrew1255:     "Windows-1255 (Hebrew)",
	CodePageArabic1256:     "Windows-1256 (Arabic)",
	CodePageWin1250:        "Windows-1250 (Central European)",
	CodePageWin1251:        "Windows-1251 (Russian)",
	CodePageTurkish1254:    "Windows-1254 (Turkish)",
	CodePageGreek1253:      "Windows-1253 (Greek)",
	CodePageMazovia:        "CP620 (Mazovia, Polish MS-DOS)",
	CodePageKamenicky:      "CP895 (Kamenicky, Czech MS-DOS)",
	CodePageJapanese:       "CP932 (Japanese Windows, Shift-JIS)",
	CodePageChineseS:       "CP936 (Chinese Simplified Windows, GBK)",
	CodePageKorean:         "CP949 (Korean Windows, EUC-KR)",
	CodePageChineseT:       "CP950 (Traditional Chinese Windows, Big5)",
	CodePageMacGreek:       "CP10006 (Greek Macintosh)",
	CodePageMacCyril:       "CP10007 (Russian Macintosh)",
	CodePageMacEE:          "CP10029 (Macintosh EE)",
}

// String returns a readable name for the code page.
func (c CodePage) String() string {
	if name, ok := codePageNames[c]; ok {
		return name
	}
	return fmt.Sprintf("unknown language driver 0x%02X", byte(c))
}

// Supported reports whether blipper has an encoding table for
// this identifier.
func (c CodePage) Supported() bool {
	if c == CodePageNone {
		return true // identity is a supported behaviour, not a gap
	}
	_, ok := codePages[c]
	return ok
}

// textCodec converts between a file's encoding and Go strings.
//
// The zero value is the identity codec: bytes pass through
// unchanged. That is deliberately the default, because a file
// declaring no code page — which is every Clipper file, since
// DBFNTX never writes byte 29 — has no encoding blipper can
// honestly claim to know. Guessing would corrupt data in a way
// that looks like success.
type textCodec struct {
	enc encoding.Encoding
}

// newTextCodec returns a codec for a code page identifier.
func newTextCodec(cp CodePage) (textCodec, error) {
	if cp == CodePageNone || cp == CodePageDBaseIVUnknown {
		return textCodec{}, nil
	}
	enc, ok := codePages[cp]
	if !ok {
		return textCodec{}, fmt.Errorf("dbf: unsupported code page 0x%02X", byte(cp))
	}
	return textCodec{enc: enc}, nil
}

// identity reports whether this codec passes bytes through.
func (c textCodec) identity() bool { return c.enc == nil }

// decode converts stored bytes to a Go string.
//
// A decode failure returns the raw bytes rather than an error:
// a single bad byte in one field should not make a table
// unreadable, and xBase files in the wild routinely carry
// stray bytes in padding. Failing loudly here would trade a
// cosmetic problem for a fatal one.
func (c textCodec) decode(b []byte) string {
	if c.identity() {
		return string(b)
	}
	out, err := c.enc.NewDecoder().Bytes(b)
	if err != nil {
		return string(b)
	}
	return string(out)
}

// encode converts a Go string to stored bytes.
//
// Unlike decode, an encode failure is returned: writing a
// character the target code page cannot represent would silently
// corrupt the file, and the caller can choose a different value
// or a different code page.
func (c textCodec) encode(s string) ([]byte, error) {
	if c.identity() {
		return []byte(s), nil
	}
	out, err := c.enc.NewEncoder().Bytes([]byte(s))
	if err != nil {
		return nil, fmt.Errorf("dbf: %q cannot be represented in this code page: %w", s, err)
	}
	return out, nil
}

// CodePage returns the table's declared code page.
func (t *Table) CodePage() CodePage { return CodePage(t.header.CodePage) }

// SetCodePage overrides the encoding used for this table's text,
// regardless of what the header declares.
//
// This exists because the common case for DOS-era Clipper data is
// a file that carries no language driver at all — DBFNTX never
// wrote byte 29 — while genuinely holding CP850 or CP437 text. A
// caller who knows their corpus can say so; blipper will not
// guess on their behalf.
//
// The override affects decoding and encoding only. It does not
// rewrite byte 29, so a table opened with an override and written
// back still declares whatever it declared before. Changing what
// a file claims about itself is a separate decision from
// interpreting it correctly.
func (t *Table) SetCodePage(cp CodePage) error {
	codec, err := newTextCodec(cp)
	if err != nil {
		return err
	}
	t.codec = codec
	t.encoding = Encoding{Source: EncodingSourceExplicitOverride, Name: cp.String(), codec: codec}
	return nil
}

// applyDecode converts a decoded record's text fields from the
// file's encoding to Go strings.
//
// Applied as a pass over the decoded record rather than threaded
// through decodeValue, because only two of the field types carry
// text and the alternative is a codec parameter on every codec
// signature. Narrower is easier to reason about.
func (c textCodec) applyDecode(rec *Record, schema Schema) {
	if c.identity() {
		return
	}
	for i, f := range schema.Fields {
		if f.Type != Character && f.Type != Memo {
			continue
		}
		s, ok := rec.Values[i].(string)
		if !ok {
			continue
		}
		rec.Values[i] = c.decode([]byte(s))
	}
}

// applyEncode converts a record's text fields from Go strings to
// the file's encoding, returning a copy so the caller's record is
// not mutated.
//
// A field that cannot be represented is an error rather than a
// silent substitution: writing a '?' where an accented character
// belonged would corrupt the data in a way that looks like
// success.
func (c textCodec) applyEncode(rec Record, schema Schema) (Record, error) {
	if c.identity() {
		return rec, nil
	}
	out := Record{Deleted: rec.Deleted, Values: make([]any, len(rec.Values))}
	copy(out.Values, rec.Values)
	for i, f := range schema.Fields {
		if f.Type != Character && f.Type != Memo {
			continue
		}
		if i >= len(out.Values) {
			break
		}
		s, ok := out.Values[i].(string)
		if !ok {
			continue
		}
		b, err := c.encode(s)
		if err != nil {
			return Record{}, fmt.Errorf("field %q: %w", f.Name, err)
		}
		out.Values[i] = string(b)
	}
	return out, nil
}
