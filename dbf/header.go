package dbf

import (
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

const (
	fileHeaderSize = 32

	headerTerminator = 0x0D
	fileTerminator   = 0x1A

	dbfVersion = 0x03 // dBASE III+
	dbfMemoBit = 0x80 // set in the version byte when a .DBT accompanies

	dbfVersionMemo = dbfVersion | dbfMemoBit // 0x83

	// dbfVersionVFP is Visual FoxPro's signature. Accepted on
	// read; blipper never writes it, because writing 0x30 is a
	// promise to honour field types and null semantics it does
	// not fully implement. See docs/VFP30_FORMAT.md.
	dbfVersionVFP = 0x30

	// dbfVersionVFPAutoinc and dbfVersionVFPVarLen are VFP with
	// autoincrement fields enabled, and VFP with Varchar/
	// Varbinary/Blob enabled, respectively. Differ from 0x30 only
	// in field-descriptor bytes 19-23 (autoincrement next/step
	// values, unused by blipper's decoders) and the addition of
	// the V/Q/W type codes (still unimplemented — see
	// dbf/vfptypes.go). Accepted on read for the same reason and
	// with the same read-only restriction as 0x30.
	dbfVersionVFPAutoinc = 0x31
	dbfVersionVFPVarLen  = 0x32

	// dbfVersionDBaseIV, dbfVersionDBaseIVSQLTable and
	// dbfVersionDBaseIVSQLSystem are the dBASE IV/5.0 lineage's
	// own version bytes — T-31. All three, along with dbfVersion
	// and dbfVersionMemo above, share low 3 bits equal to 3; see
	// isDBaseLineage. `0x63`'s field-descriptor layout needs no
	// handling distinct from `0x03`/`0x83`: confirmed by a real,
	// shipped C reader (source S11, docs/DBASE_FORMAT.md) tested
	// against a currently-operating product's production files,
	// which decodes only version=byte0&0x07 and memo=byte0>>7,
	// never inspecting bits 4-6.
	dbfVersionDBaseIV          = 0x8B // dBASE IV/5.0, with memo
	dbfVersionDBaseIVSQLTable  = 0x43 // dBASE IV SQL table
	dbfVersionDBaseIVSQLSystem = 0x63 // dBASE IV SQL system file

	// Table-flags byte (offset 28 in the DBF header). See T-10's
	// truth table. Bits 2 and 3 are the ones this codebase touches;
	// bit 0 (structural CDX) and bit 1 (memo) are VFP conventions
	// that blipper does not currently emit.
	dbfTableFlagDBC     = 0x04                                  // VFP: table is owned by a DBC
	dbfTableFlagBlipper = 0x08                                  // blipper-reserved provenance
	dbfTableFlagDBCPair = dbfTableFlagDBC | dbfTableFlagBlipper // 0x0C

	// dbfBacklinkSize is the length of the VFP backlink that
	// sits between the field terminator and the first record
	// when a table is DBC-owned.
	dbfBacklinkSize = 263
)

// isDBaseLineage reports whether versionByte belongs to the dBASE
// III+/IV/5.0 family, where field types B and G mean 10-digit
// ASCII .DBT block pointers — rather than Visual FoxPro's meaning
// for the same two letters, an 8-byte IEEE double and a 4-byte
// binary pointer respectively. See dbf/dbasetypes.go.
//
// Tested via the low 3 bits (version = byte0 & 0x07), not an
// explicit list of exact bytes, because that is what a real,
// shipped reader tested against production files actually checks
// (source S11) — and because 0x43/0x63's SQL-table bits (4-6)
// must not affect this decision, matching S11's finding that
// field-layout parsing ignores those bits entirely.
func isDBaseLineage(versionByte byte) bool {
	return versionByte&0x07 == dbfVersion
}

// trimBacklink returns the NUL-terminated relative path stored
// in a 263-byte backlink block.
func trimBacklink(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// headerInfo carries the physical bookkeeping read from a DBF header.
//
// These values belong to the implementation and never surface in the
// public API.
type headerInfo struct {
	recordCount uint32
	headerSize  uint16
	recordSize  uint16

	// versionByte carries the raw first-byte value from the read
	// header, so that rewriting preserves whichever format
	// (0x03/0x83 DBT-flavour, 0xF5 FPT-flavour) the file was
	// created with. Prevents silent demotion of an FPT table
	// to a DBT-marked one on rewrite.
	versionByte byte

	// hasMemo records bit 7 of the version byte, so that rewriting a
	// header preserves a table's association with its .DBT rather
	// than silently demoting it to a memoless table.
	hasMemo bool

	// tableFlags carries byte 28 verbatim so that rewriting
	// preserves DBC-owned (bit 2, 0x04) and blipper-provenance
	// (bit 3, 0x08) signalling. See T-10's truth table.
	tableFlags byte

	// backlink carries the 263-byte VFP backlink between the
	// field terminator and the first record, when byte 28 bit
	// 2 is set. Empty string when no backlink is present.
	backlink string
}

// readHeader reads the 32-byte DBF file header.
//
// It returns the logical header metadata together with the physical
// bookkeeping stored in the file.
func readHeader(r io.Reader) (Header, headerInfo, error) {
	var raw [fileHeaderSize]byte

	if _, err := io.ReadFull(r, raw[:]); err != nil {
		return Header{}, headerInfo{}, err
	}

	if raw[0] != dbfVersion && raw[0] != dbfVersionMemo &&
		raw[0] != dbfVersionFPT && raw[0] != dbfVersionVFP &&
		raw[0] != dbfVersionVFPAutoinc && raw[0] != dbfVersionVFPVarLen &&
		raw[0] != dbfVersionDBaseIV && raw[0] != dbfVersionDBaseIVSQLTable &&
		raw[0] != dbfVersionDBaseIVSQLSystem {
		return Header{}, headerInfo{},
			fmt.Errorf("unsupported DBF version 0x%02X", raw[0])
	}

	header := Header{
		LastUpdate: decodeHeaderDate(
			raw[1],
			raw[2],
			raw[3],
		),
		CodePage: raw[29],
	}

	info := headerInfo{
		recordCount: binary.LittleEndian.Uint32(raw[4:8]),
		headerSize:  binary.LittleEndian.Uint16(raw[8:10]),
		recordSize:  binary.LittleEndian.Uint16(raw[10:12]),
		versionByte: raw[0],
		hasMemo:     raw[0]&dbfMemoBit != 0,
		tableFlags:  raw[28],
	}

	return header, info, nil
}

// writeHeader writes the 32-byte DBF file header.
//
// headerSize and recordSize are passed explicitly rather than being
// recomputed from a schema: a file opened with a padded header keeps
// its original header size when the header is rewritten. The version
// byte is passed through verbatim, preserving whichever memo format
// (DBT-flavour or FPT-flavour) the file was created with.
func writeHeader(
	w io.Writer,
	header Header,
	headerSize uint16,
	recordSize uint16,
	recordCount uint32,
	versionByte byte,
	tableFlags byte,
) error {

	var raw [fileHeaderSize]byte

	raw[0] = versionByte
	raw[28] = tableFlags

	encodeHeaderDate(header.LastUpdate, raw[1:4])

	binary.LittleEndian.PutUint32(
		raw[4:8],
		recordCount,
	)

	binary.LittleEndian.PutUint16(
		raw[8:10],
		headerSize,
	)

	binary.LittleEndian.PutUint16(
		raw[10:12],
		recordSize,
	)

	// bytes 12..13
	// Reserved.

	// byte 14
	// Incomplete transaction flag.
	// Always zero.

	// byte 15
	// Encryption flag.
	// Always zero.

	// bytes 16..27
	// Reserved.

	// byte 28
	// Production MDX flag.
	// Clipper NTX tables leave this zero.

	raw[29] = header.CodePage

	// bytes 30..31
	// Reserved.

	_, err := w.Write(raw[:])
	return err
}

// headerYearPivot is the Y2K windowing threshold for decoding the
// header's year byte. Bytes below this are interpreted as 2000+y
// (mod-100, Clipper 5.2e's convention); bytes at or above are
// interpreted as 1900+y (dBASE III+'s original documented
// convention).
//
// The pivot at 80 is a heuristic, not a rule. Byte 79 is ambiguous
// between 1979 and 2079; we pick 2079. Byte 80 is ambiguous
// between 1980 and 2080; we pick 1980. The corpus at
// github.com/ha1tch/clipper contains 1990s-era files (bytes 91-98)
// and a 2009 file (byte 109), both of which decode correctly under
// this rule. Files generated by Clipper 5.2e under guard G-01
// today (byte 26) also decode correctly. Files written by a
// 1900+y encoder between 2079 and 2155 would be misdecoded, and
// files written by a mod-100 encoder between 1980 and 1999 would
// be misdecoded — neither exists in the corpus.
const headerYearPivot = 80

func decodeHeaderDate(year, month, day byte) time.Time {
	if year == 0 && month == 0 && day == 0 {
		return time.Time{}
	}

	var y int
	if year < headerYearPivot {
		y = 2000 + int(year) // post-Y2K, mod-100 convention (Clipper)
	} else {
		y = 1900 + int(year) // legacy dBASE III+ convention
	}

	return time.Date(
		y,
		time.Month(month),
		int(day),
		0,
		0,
		0,
		0,
		time.UTC,
	)
}

func encodeHeaderDate(t time.Time, dst []byte) {
	if len(dst) != 3 {
		panic("encodeHeaderDate: destination must be exactly 3 bytes")
	}

	if t.IsZero() {
		clear(dst)
		return
	}

	year := t.Year()

	// Clipper 5.2e writes year mod 100 (verified byte-for-byte
	// under guard G-01; a file generated 2026-07-23 carries
	// byte 26). We match Clipper on the write side. Combined
	// with headerYearPivot on the read side this gives the
	// widest range of correct decodes for both this library's
	// output and the historical corpus.
	//
	// The year range 1900..2155 comes from a naive byte-fits
	// analysis of "year - 1900"; under mod-100 the byte is
	// always 0..99 so this range constraint no longer bites.
	// We keep the historical lower bound to catch nonsense
	// like time.Time{} slipping through the IsZero check.
	if year < 1900 {
		panic(fmt.Sprintf("year %d out of DBF range", year))
	}

	dst[0] = byte(year % 100)
	dst[1] = byte(t.Month())
	dst[2] = byte(t.Day())
}
