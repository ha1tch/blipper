package fatfs

import (
	"fmt"
	"strings"
	"unicode/utf16"
)

// VFAT long filename support.
//
// A long name is stored in the directory as a run of 32-byte
// entries carrying attribute attrLongName (0x0F), immediately
// preceding the ordinary 8.3 entry they describe. The run appears
// in reverse sequence order — the entry holding the *last*
// fragment comes first — and each entry holds 13 UCS-2 characters
// spread across three disjoint ranges within the slot.
//
// Every entry also carries a checksum of the 8.3 alias it belongs
// to. That checksum is the only defence against desync: a
// non-LFN-aware tool that rewrites the short entry leaves the
// long-name run pointing at a name that is no longer there, and
// without validating the checksum a reader would report a name
// belonging to a different file.

const (
	// lfnCharsPerEntry is how many UCS-2 characters one LFN slot
	// carries: 5 + 6 + 2 across the three ranges.
	lfnCharsPerEntry = 13

	// lfnSeqMask extracts the sequence number from the first byte.
	lfnSeqMask = 0x1F

	// lfnLastEntry marks the entry holding the final fragment,
	// which appears first in the on-disk run.
	lfnLastEntry = 0x40

	// lfnMaxEntries bounds a name at 20 entries, i.e. 260
	// characters — the VFAT limit is 255.
	lfnMaxEntries = 20

	// lfnMaxNameLen is the longest long name VFAT permits.
	lfnMaxNameLen = 255
)

// lfnCharOffsets are the byte offsets within a 32-byte LFN entry
// holding UCS-2 characters, in order. Three disjoint runs, which
// is the format's chief awkwardness.
var lfnCharOffsets = []int{
	1, 3, 5, 7, 9, // 5 characters
	14, 16, 18, 20, 22, 24, // 6 characters
	28, 30, // 2 characters
}

// lfnChecksum computes the one-byte checksum of an 8.3 name that
// every LFN entry in the run must carry.
//
// The algorithm is fixed by the specification: an 8-bit rotate
// right, then add, across all eleven bytes.
func lfnChecksum(short [11]byte) byte {
	var sum byte
	for _, c := range short {
		sum = ((sum & 1) << 7) + (sum >> 1) + c
	}
	return sum
}

// decodeLFNEntry pulls the 13 UCS-2 code units out of one LFN
// slot. Unused positions are padded with 0xFFFF after a single
// 0x0000 terminator; both are stripped by the caller assembling
// the name.
func decodeLFNEntry(raw []byte) []uint16 {
	out := make([]uint16, 0, lfnCharsPerEntry)
	for _, off := range lfnCharOffsets {
		out = append(out, uint16(raw[off])|uint16(raw[off+1])<<8)
	}
	return out
}

// encodeLFNEntry writes 13 UCS-2 code units into an LFN slot,
// along with the sequence number, checksum, and the fixed fields
// that mark it as a long-name entry.
func encodeLFNEntry(raw []byte, chars []uint16, seq byte, last bool, checksum byte) {
	for i := range raw {
		raw[i] = 0
	}
	raw[0] = seq
	if last {
		raw[0] |= lfnLastEntry
	}
	raw[11] = attrLongName
	raw[12] = 0 // reserved
	raw[13] = checksum
	raw[26] = 0 // first-cluster field, always zero in an LFN entry
	raw[27] = 0

	for i, off := range lfnCharOffsets {
		var v uint16
		switch {
		case i < len(chars):
			v = chars[i]
		case i == len(chars):
			v = 0x0000 // terminator
		default:
			v = 0xFFFF // padding
		}
		raw[off] = byte(v)
		raw[off+1] = byte(v >> 8)
	}
}

// assembleLongName turns a collected run of LFN entries into a
// string.
//
// entries arrive in on-disk order, which is reverse sequence
// order, so the fragments are prepended rather than appended. A
// run whose sequence numbers do not form 1..n, or whose checksums
// disagree with the supplied 8.3 alias, is rejected: the caller
// then falls back to the alias rather than reporting a name that
// may belong to a different file.
func assembleLongName(entries [][]byte, short [11]byte) (string, bool) {
	if len(entries) == 0 || len(entries) > lfnMaxEntries {
		return "", false
	}
	want := lfnChecksum(short)

	units := make([]uint16, 0, len(entries)*lfnCharsPerEntry)
	// Walk in reverse, since entry 1 is last on disk.
	for i := len(entries) - 1; i >= 0; i-- {
		raw := entries[i]
		if raw[13] != want {
			return "", false // belongs to a different short entry
		}
		seq := raw[0] & lfnSeqMask
		// On-disk position i should hold sequence len(entries)-i.
		if int(seq) != len(entries)-i {
			return "", false
		}
		isLast := raw[0]&lfnLastEntry != 0
		if isLast != (i == 0) {
			return "", false // last-marker in the wrong place
		}
		units = append(units, decodeLFNEntry(raw)...)
	}

	// Trim at the first terminator; drop 0xFFFF padding.
	end := len(units)
	for i, u := range units {
		if u == 0x0000 {
			end = i
			break
		}
	}
	units = units[:end]
	for len(units) > 0 && units[len(units)-1] == 0xFFFF {
		units = units[:len(units)-1]
	}
	if len(units) == 0 {
		return "", false
	}
	return string(utf16.Decode(units)), true
}

// buildLFNEntries produces the on-disk run of LFN slots for a long
// name, in the order they must be written (reverse sequence).
func buildLFNEntries(longName string, short [11]byte) ([][]byte, error) {
	units := utf16.Encode([]rune(longName))
	if len(units) == 0 {
		return nil, fmt.Errorf("fatfs: empty long name")
	}
	if len(units) > lfnMaxNameLen {
		return nil, fmt.Errorf("fatfs: long name is %d characters, limit is %d",
			len(units), lfnMaxNameLen)
	}
	// A rune outside the BMP encodes as a surrogate pair, which
	// utf16.Encode already produced; that is representable. What
	// is not representable is a code unit that would collide with
	// the padding sentinel.
	for _, u := range units {
		if u == 0xFFFF {
			return nil, fmt.Errorf("fatfs: long name contains U+FFFF, which collides with LFN padding")
		}
	}

	n := (len(units) + lfnCharsPerEntry - 1) / lfnCharsPerEntry
	if n > lfnMaxEntries {
		return nil, fmt.Errorf("fatfs: long name needs %d entries, limit is %d", n, lfnMaxEntries)
	}
	checksum := lfnChecksum(short)

	// Build in sequence order, then reverse for the disk layout.
	seqOrder := make([][]byte, n)
	for i := 0; i < n; i++ {
		start := i * lfnCharsPerEntry
		end := start + lfnCharsPerEntry
		if end > len(units) {
			end = len(units)
		}
		raw := make([]byte, dirEntrySize)
		encodeLFNEntry(raw, units[start:end], byte(i+1), i == n-1, checksum)
		seqOrder[i] = raw
	}
	out := make([][]byte, n)
	for i := range seqOrder {
		out[n-1-i] = seqOrder[i]
	}
	return out, nil
}

// needsLongName reports whether a name cannot be represented as a
// plain 8.3 entry and therefore requires an LFN run.
func needsLongName(name string) bool {
	_, err := normaliseName(name)
	if err != nil {
		return true
	}
	// A name that normalises cleanly still needs an LFN run if it
	// is not already in canonical upper-case 8.3 form, since the
	// short entry cannot preserve case.
	return name != formatName(mustNormalise(name))
}

func mustNormalise(name string) [11]byte {
	n, err := normaliseName(name)
	if err != nil {
		return [11]byte{}
	}
	return n
}

// generateAlias derives a unique 8.3 alias for a long name,
// following the NAME~N.EXT convention, skipping any alias already
// present in taken.
func generateAlias(longName string, taken func([11]byte) bool) ([11]byte, error) {
	base, ext := splitLongName(longName)
	base = sanitiseAliasPart(base, 8)
	ext = sanitiseAliasPart(ext, 3)
	if base == "" {
		base = "FILE"
	}

	for n := 1; n <= 999999; n++ {
		suffix := fmt.Sprintf("~%d", n)
		keep := 8 - len(suffix)
		if keep > len(base) {
			keep = len(base)
		}
		if keep < 1 {
			return [11]byte{}, fmt.Errorf("fatfs: cannot form an alias for %q", longName)
		}
		candidate := base[:keep] + suffix

		var out [11]byte
		for i := range out {
			out[i] = ' '
		}
		copy(out[0:8], candidate)
		copy(out[8:11], ext)
		if !taken(out) {
			return out, nil
		}
	}
	return [11]byte{}, fmt.Errorf("fatfs: exhausted alias numbering for %q", longName)
}

// splitLongName separates a name at its final dot.
func splitLongName(name string) (base, ext string) {
	name = strings.TrimSpace(name)
	if dot := strings.LastIndex(name, "."); dot > 0 {
		return name[:dot], name[dot+1:]
	}
	return name, ""
}

// sanitiseAliasPart uppercases and strips characters illegal in an
// 8.3 name, truncating to the given width.
func sanitiseAliasPart(s string, width int) string {
	s = strings.ToUpper(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case strings.ContainsRune("$%'-_@~`!(){}^#&", r):
			b.WriteRune(r)
		default:
			// Spaces, dots, and anything non-ASCII are dropped
			// rather than substituted, matching what Windows does
			// when deriving an alias.
		}
		if b.Len() >= width {
			break
		}
	}
	out := b.String()
	if len(out) > width {
		out = out[:width]
	}
	return out
}
