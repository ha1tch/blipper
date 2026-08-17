# UM.DBF — Clipper corpus specimen with genuine duplicate field names

Sourced from the ha1tch/clipper corpus at
`PCPOSMTS/TEST/MTSDATA/UM.DBF`. This is a real Clipper POS/MTS
production file (17 fields, 2 records, 1017 bytes) whose schema
declares `ACCUMDSUM` three times in field positions 12, 13, and 14.

Analysis under T-07 (2026-07-23) verified the file is a plain
Clipper table, NOT a VFP DBC-owned table:

- Version byte `0x03`, byte 28 = `0x00` (no DBC flag)
- No sibling `.DBC` file (the whole directory is `.DBF`/`.NTX`)
- Header size = 578 = 32 + 17×32 + 1 + 1, no 263-byte backlink
- Field descriptor bytes 12–15 = `99190000` (Clipper stale-memory
  addresses, oracle §9.2) — VFP would zero these
- Field descriptor byte 18 = `0x03` on every field (Clipper stale
  bytes) — VFP would use for `system`/`nullable`/`NOCPTRANS`

The duplicates are genuine. Clipper never enforced field-name
uniqueness at the format level; this application relied on
positional access. blipper's `Open` tolerates this as of T-07;
`Create` still rejects it. Named access via `Record.Get(schema,
"ACCUMDSUM")` resolves to the first match (field 12) — matching
Clipper's own behavior via linear scan.
