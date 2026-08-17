package dbf

// DBaseBinary and DBaseGeneral are internal sentinels, not
// on-disk byte values. dBASE III+/IV/5.0's own B (Binary) and G
// (OLE) field types are both 10-digit ASCII .DBT block pointers —
// functionally identical to Memo's encoding — but the on-disk
// type byte is the ordinary uppercase 'B'/'G', which Visual
// FoxPro's field types already claim with a different meaning (an
// 8-byte IEEE double and a 4-byte binary pointer respectively).
//
// readField remaps the raw byte to one of these two lowercase
// sentinels when the table's version byte indicates the dBASE
// lineage (isDBaseLineage, dbf/header.go), and writeField remaps
// back on the way out. This lets decode/encode dispatch tell the
// two meanings apart at the type level — a single switch on
// Field.Type — rather than needing table-level context threaded
// through every call. Lowercase is deliberate and safe: every
// source found this session describes on-disk dBASE field type
// bytes as uppercase, so a lowercase value can only originate
// from this remapping, never from a file.
//
// Settled via source S12, Borland's own confidential internal
// manuscript for the dBASE for Windows 5.0 Language Reference,
// Appendix C (docs/DBASE_FORMAT.md): both letters stated in as
// many words as 10-digit ASCII .DBT block numbers. Independently
// confirmed structurally consistent with a live write-oracle
// (source S13) — no B/G specimen exists to decode directly, but
// the oracle confirmed everything else this session predicted
// about dBASE 5.0's own field descriptor layout.
const (
	DBaseBinary  FieldType = 'b'
	DBaseGeneral FieldType = 'g'
)

// remapDBaseLineageType converts an on-disk field type byte to
// its lineage-aware internal value. Only B and G are affected;
// every other type byte passes through unchanged, including on
// dBASE-lineage tables, since no other letter collides with a VFP
// meaning the way B and G do.
func remapDBaseLineageType(onDisk FieldType, lineageIsDBase bool) FieldType {
	if !lineageIsDBase {
		return onDisk
	}
	switch onDisk {
	case 'B':
		return DBaseBinary
	case 'G':
		return DBaseGeneral
	default:
		return onDisk
	}
}

// unmapDBaseLineageType is remapDBaseLineageType's inverse, used
// when writing a field descriptor back to disk.
func unmapDBaseLineageType(internal FieldType) FieldType {
	switch internal {
	case DBaseBinary:
		return 'B'
	case DBaseGeneral:
		return 'G'
	default:
		return internal
	}
}
