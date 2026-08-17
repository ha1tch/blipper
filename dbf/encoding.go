package dbf

import (
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/charmap"
)

// EncodingSource identifies which of the four resolution tiers
// produced a Table's current text encoding — T-27.
//
// The four-way priority, most specific first: an explicit caller
// override; a `.cpg` sidecar file; the header's own byte 29; and,
// absent all three, identity — bytes pass through unchanged. This
// type exists purely for introspection; the actual transcoding is
// still done through the unexported textCodec it wraps.
//
// The four tiers do not need a dedicated resolution engine to
// implement the priority correctly. Open already establishes
// tiers 3/4 (byte 29, or identity when byte 29 is CodePageNone).
// blipperfs.resolveSiblings, if it finds a `.cpg`, calls
// SetEncoding to establish tier 2, overriding whatever Open set.
// A caller calling SetCodePage or SetEncoding directly establishes
// tier 1 simply by virtue of being an explicit, later call — no
// caller ever needs to be told an override "won", because nothing
// else touches the codec after Open and sibling resolution finish.
type EncodingSource int

const (
	// EncodingSourceIdentity means no encoding information was
	// found anywhere: the file declares no code page (byte 29 is
	// CodePageNone), no `.cpg` sidecar exists, and no caller
	// override was set. Bytes pass through unchanged — correct
	// by construction for a UTF-8 file, and the only honest
	// choice for anything else, since guessing would corrupt
	// data in a way that looks like success.
	EncodingSourceIdentity EncodingSource = iota

	// EncodingSourceHeaderByte29 means the encoding came from
	// the DBF header's own declared code page.
	EncodingSourceHeaderByte29

	// EncodingSourceCpgSidecar means the encoding came from a
	// sibling `.cpg` file, resolved by blipperfs and applied via
	// SetEncoding. The `dbf` package itself never reads files by
	// name — this tier exists in the type so a caller inspecting
	// Table.Encoding() can tell it apart from the other three,
	// but nothing in this package populates it directly.
	EncodingSourceCpgSidecar

	// EncodingSourceExplicitOverride means a caller called
	// SetCodePage or SetEncoding directly.
	EncodingSourceExplicitOverride
)

// String returns a readable name for the source.
func (s EncodingSource) String() string {
	switch s {
	case EncodingSourceHeaderByte29:
		return "header byte 29"
	case EncodingSourceCpgSidecar:
		return ".cpg sidecar"
	case EncodingSourceExplicitOverride:
		return "explicit override"
	default:
		return "identity"
	}
}

// Encoding is a Table's resolved text encoding, together with
// where that resolution came from.
type Encoding struct {
	Source EncodingSource

	// Name is a readable identifier for the resolved encoding:
	// a CodePage's own String() for tiers 1/3, the matched alias
	// text for tier 2, or "identity" for tier 4.
	Name string

	codec textCodec
}

// cpgAliases maps the encoding names real `.cpg` files use to a
// CodePage identifier blipper already has a table for — genuine
// exact matches only. Built from the aliases GIS producers were
// observed using this session (T-27's trigger: GADM, ArcGIS/USGS,
// Landsat Missions, and a third USGS group all writing "UTF-8"),
// plus bare numeric forms ("1252") some producers use instead of
// a name.
//
// ISO-8859-1 and UTF-8 are deliberately absent from this map —
// see ParseCpgEncoding, which handles both directly rather than
// through a CodePage substitution. An earlier draft of this file
// mapped "ISO-8859-1"/ESRI's "LATIN1" to CodePageWin1252, on the
// reasoning that the two are close. Caught and corrected before
// shipping: this is exactly the "nearly right, hides a problem"
// mistake this codebase's own code page table already warns
// against elsewhere (see codePages' CP865/CP861 comment,
// dbf/codepage.go) — Windows-1252 and ISO-8859-1 differ in the
// 0x80-0x9F range, and CodePage exists to model byte 29's
// Microsoft language-driver identifiers specifically, which
// neither UTF-8 nor plain ISO-8859-1 was ever assigned one of.
var cpgAliases = map[string]CodePage{
	"CP1252": CodePageWin1252,
	"1252":   CodePageWin1252,
	"CP850":  CodePageIntl850,
	"850":    CodePageIntl850,
	"CP437":  CodePageUS437,
	"437":    CodePageUS437,
}

// ParseCpgEncoding parses a `.cpg` file's content — typically a
// single line naming an encoding — into an Encoding with
// EncodingSourceCpgSidecar already set.
//
// Recognizes the aliases real files use, matched case-insensitively
// after trimming whitespace: "UTF-8" (and bare "UTF8"), the named
// forms in cpgAliases, and bare code-page numbers matching a
// CodePage this package already has a table for (e.g. "1252" maps
// the same as "CP1252").
//
// An unrecognized value is reported as an error rather than
// falling back to identity silently: a `.cpg` file that exists but
// names something blipper cannot honour is exactly the case T-27
// was filed to stop being wrong-by-accident about. A caller who
// wants identity behaviour for an unrecognized encoding can choose
// that explicitly by not applying the resulting error's Encoding.
func ParseCpgEncoding(content []byte) (Encoding, error) {
	name := strings.TrimSpace(string(content))
	upper := strings.ToUpper(name)

	if upper == "UTF-8" || upper == "UTF8" {
		return Encoding{
			Source: EncodingSourceCpgSidecar,
			Name:   name,
			codec:  textCodec{}, // identity: Go strings are already UTF-8
		}, nil
	}

	if upper == "ISO-8859-1" || upper == "ISO8859-1" || upper == "LATIN1" {
		// The genuine encoding, not a substitution — see
		// cpgAliases' doc comment for why this isn't routed
		// through CodePage/Windows-1252.
		return Encoding{
			Source: EncodingSourceCpgSidecar,
			Name:   name,
			codec:  textCodec{enc: charmap.ISO8859_1},
		}, nil
	}

	if cp, ok := cpgAliases[upper]; ok {
		codec, err := newTextCodec(cp)
		if err != nil {
			return Encoding{}, fmt.Errorf("dbf: .cpg names %q, which maps to an unsupported code page: %w", name, err)
		}
		return Encoding{Source: EncodingSourceCpgSidecar, Name: name, codec: codec}, nil
	}

	// A bare number not in the alias table above, but matching a
	// CodePage blipper has a real encoding for regardless.
	if n, err := strconv.ParseUint(upper, 10, 8); err == nil {
		cp := CodePage(n)
		if cp.Supported() {
			codec, err := newTextCodec(cp)
			if err != nil {
				return Encoding{}, err
			}
			return Encoding{Source: EncodingSourceCpgSidecar, Name: name, codec: codec}, nil
		}
	}

	return Encoding{}, fmt.Errorf("dbf: .cpg names %q, which blipper does not recognize", name)
}

// Encoding returns the table's currently resolved encoding.
func (t *Table) Encoding() Encoding { return t.encoding }

// SetEncoding applies enc to this table, overriding whatever
// resolved it before — the explicit-override tier, or whatever
// enc.Source itself declares if called by sidecar-resolution code
// on the caller's behalf (see blipperfs, which calls this after
// finding a `.cpg`).
//
// Like SetCodePage, this affects decoding and encoding only. It
// does not rewrite byte 29.
func (t *Table) SetEncoding(enc Encoding) {
	t.codec = enc.codec
	t.encoding = enc
}
