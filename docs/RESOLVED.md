# blipper — Resolution Record

Append-only, newest first. Each entry is the item's full register text
as at closure, stamped with closing version and date.

## [0.9.25] T-38. dBASE IV/5 table write support (0x8B/0x43/0x63 version bytes) (v0.9.25, 2026-07-24)

Theme: dbf · Priority: P3 · Status: ✓ closed by implementation, same session as filing

- **Trigger:** direct question — "is there any good reason why we can only read dBASE IV and dBASE 5.0 but we can't write those formats?" Checked `dbf/create.go` directly rather than assume: `Create`/`CreateWithBacklink` hardcode `dbfVersion`/`dbfVersionMemo`, with no branch for the dBASE IV/5.0 lineage at all. Every piece a write path would need already existed and was already tested elsewhere — `B`/`G` field encoding (T-31's round-trip test), the memo file format (T-37), and every other header byte already defaulting to what a fresh table needs. Unlike T-10's VFP write exclusion, which is a deliberate, principled refusal (a promise about VFP null semantics blipper does not fully honour), nothing analogous applied here — this was omission, not policy.
- **A real correction mid-design**: the first framing of this work named the new function after dBASE IV alone. dBASE IV 2.0 and dBASE 5.0 for DOS write byte-identical headers and field descriptors for all three non-III+ version bytes (confirmed directly in T-31's own findings — the live write-oracle used to verify T-31 was dBASE 5.0, not dBASE IV, exercising the same code path either product would). `DBaseIVTableKind`'s doc comment states this explicitly to prevent the same narrowing from recurring.
- **`CreateDBaseIV(rw, schema, kind)`** covers all three version bytes T-31 added read support for — `DBaseIVTable` (`0x8B`), `DBaseIVSQLTable` (`0x43`), `DBaseIVSQLSystem` (`0x63`) — with validation matching what every real specimen actually looks like: `0x8B` requires a Memo field, the two SQL variants refuse one, since every specimen found this session (6 `0x43`, 11 `0x63`) has the memo bit clear and nothing confirms what the other combination would look like.
- Byte 28 (production `.MDX` flag) starts at `0` for a freshly created table, matching `Create`'s existing behaviour for ordinary tables — correct, since a fresh table has no `.MDX` yet.
- 3 new tests in `dbf/create_dbaseiv_test.go`: a full round trip including a `B` (`DBaseBinary`) field through `Append`/reopen, both SQL variants round-tripping, and the mismatched-memo validation.

Cross-ref: CHANGELOG 0.9.25, dbf/create_dbaseiv.go.

## [0.9.23] T-27. .cpg sidecar support: encoding resolution beyond byte 29 (v0.9.23, 2026-07-24)

Theme: blipperfs · Priority: P2 · Status: ✓ closed by implementation

- **Trigger:** requested directly ("close what can be closed with the knowledge we already have") after a thorough audit confirmed this item's design was already fully decided — four-way resolution (explicit override, `.cpg` sidecar, header byte 29, identity), layer placement (`dbf` stays filename-blind; sidecar detection belongs in `blipperfs.resolveSiblings`), and the exact aliases real files use.
- **The four-way priority needed no dedicated resolution engine.** `Open` already implements tiers 3/4 via existing `newTextCodec`/header-byte-29 logic. `blipperfs.resolveSiblings` adds tier 2, calling the new `Table.SetEncoding` when a `.cpg` sibling exists — following the exact pattern already established for memo/DBC/CDX sibling resolution. Tier 1 (explicit override) needs no special coordination: it wins simply by being an explicit, later call, since nothing else touches the codec after `Open` and sibling resolution finish.
- **New `Encoding` type** (`dbf/encoding.go`) carries both the resolved codec and which tier produced it, for introspection via `Table.Encoding()`.
- **A real mistake caught before shipping, not after.** An early draft mapped `.cpg`'s `"ISO-8859-1"`/ESRI's `"LATIN1"` to `CodePageWin1252` as a close substitute — exactly the "nearly right, hides a problem" error this codebase's own code page table already warns against elsewhere (the CP865/CP861 comment). Windows-1252 and ISO-8859-1 genuinely differ (the Euro sign exists in one, not the other). Fixed to use `charmap.ISO8859_1` directly; a regression test (`TestParseCpgEncodingISO88591NotSubstituted`) exists specifically to catch this mistake if it ever creeps back in.
- **Decisive end-to-end test** using the exact real-world pattern this item was filed for: header byte 29 = `0x00`, a `.cpg` sibling naming UTF-8, and text containing characters that exist in no single-byte DOS or Windows code page at all — `Ċ`/`Ħ`, as seen in real Malta GADM shapefiles (source: `docs/RESEARCH_NOTES.md`'s original trigger investigation). A wrong resolution fails loudly rather than producing plausible-looking wrong text.
- 10 new tests across `dbf/encoding_test.go` and `blipperfs/cpg_sidecar_test.go`, covering UTF-8, the ISO-8859-1 regression, numeric/named CP aliases, unrecognized values (reported as an error, matching the existing sibling-failure contract for CDX/DBC), and the no-`.cpg`-present fallback.

Cross-ref: CHANGELOG 0.9.23, dbf/encoding.go, blipperfs/session.go.

## [0.9.23] T-36. V/Q exact content decode: significant-trailing-space case (v0.9.23, 2026-07-24)

Theme: dbf · Priority: P3 · Status: ✓ closed by implementation

- **Trigger:** requested directly, alongside T-27/T-37/T-20's `dbf.Table.Reload`, as work fully specified from prior sessions and closable without new research.
- **Simpler than documented once examined:** `decodeValue` had exactly one call site (inside `decodeRecord`'s loop), meaning its role could be extended freely with zero risk to any other caller. `_NullFlags`'s raw bytes are located once, up front, via field-offset arithmetic (not requiring `_NullFlags` to be the last field, though every source and specimen found this session places it there); `Varchar`/`Varbinary` fields consult it directly rather than routing through a restructured `decodeValue`.
- **The decisive test revealed the old approximation was worse than documented.** Not just "loses significant trailing spaces" — for a not-full field, `strings.TrimRight` only strips space characters, so the raw length byte at the end of the field (not a space) survived untouched in the old output too. A field storing `"AB   "` (5 bytes, 3 significant trailing spaces) with length byte `0x05` decoded under the old approximation as `"AB       \x05"` — both missing the real content's trailing spaces AND carrying a stray control byte. The new exact decode returns `"AB   "` precisely.
- 5 new tests in `dbf/varlen_exact_test.go`, including a sanity check that explicitly reproduces the old approximation's output to confirm the test is decisive rather than a case where old and new happen to agree.

Cross-ref: CHANGELOG 0.9.23, dbf/record_codec.go, dbf/nullflags.go.

## [0.9.23] T-37. dBASE IV/5 memo write support (.DBT, 8-byte header format) (v0.9.23, 2026-07-24)

Theme: dbf · Priority: P3 · Status: ✓ closed by implementation

- **Trigger:** requested directly, as the smallest and most mechanical of the four items examined together in this pass — deferred at T-31's closure specifically to keep read support and write support independently verifiable.
- **`CreateDBaseIVMemo`/`(*DBaseIVMemoFile).Append`** mirror `dbf/memo.go`'s existing `CreateMemo`/`Append` for dBASE III+, adapted for the different header shapes: a 22-byte self-describing block-0 header (table name, block size) instead of a bare next-free pointer, and an 8-byte per-block header (constant marker, header-inclusive length) instead of a terminator.
- **A real bug caught before shipping, not after.** The first draft reused `encodeFieldName` — sized for 11-byte DBF field descriptors — to write the memo header's 8-byte table-name field. Wrong size; panicked immediately on the first test run. Fixed with a correctly-sized local helper (`encodeDBaseIVMemoTableName`).
- **Untested against real dBASE 5.0 re-opening a blipper-written file** — no such round trip has been attempted, stated plainly in the code's own doc comment. What is verified is internal consistency: blipper's own writer and reader agree, including across a genuine block-boundary crossing.
- 3 new tests in `dbf/memo_dbaseiv_write_test.go`, including a multi-block write (600 bytes, forcing a crossing past one block's ~504 usable bytes) mirroring T-31's real-1994-data multi-block read verification.

Cross-ref: CHANGELOG 0.9.23, dbf/memo_dbaseiv.go.

## [0.9.21] T-31. dBASE IV/5 table support: 0x8B, 0x43, 0x63 (v0.9.21, 2026-07-24)

Theme: dbf · Priority: P3 · Status: ✓ closed by implementation — full read support shipped; write support (T-37) deliberately deferred

- **Trigger:** every risk this item carried had been retired by evidence across S11/S12/S13 in prior releases; implemented directly on the user's instruction.
- **Version bytes `0x8B`/`0x43`/`0x63` accepted.** Lineage dispatch (`isDBaseLineage`, `dbf/header.go`) uses the bit test `version = byte0 & 0x07 == 0x03`, matching S11's confirmed real-world parsing rule rather than an explicit byte list — correctly includes all four dBASE-lineage bytes and excludes VFP's `0x30`/`0x31`/`0x32` and FoxPro's `0xF5`.
- **The `B`/`G` lineage trap solved with a genuinely new mechanism.** `FieldType` is a raw byte, so the same on-disk `'B'`/`'G'` can't carry two meanings simultaneously. Introduced two internal-only sentinel values, `DBaseBinary`/`DBaseGeneral` (lowercase `'b'`/`'g'` — deliberately never a real on-disk byte in any source found this session). `readField` remaps on-disk `'B'`/`'G'` to these sentinels when the table's version byte indicates the dBASE lineage; `writeField` unmaps unconditionally on the way out. Decode/encode reuse Memo's exact plain-pointer-text logic, since dBASE's `B`/`G`/`M` all share one encoding.
- **A separate, pre-existing blocker found and fixed along the way:** every dBASE 5.0 specimen carries code page byte `0x1B`, which blipper had no entry for — `dbf.Open()` passed every structural check and then hard-failed on the codec lookup. Added `CodePageDBaseIVUnknown`, a narrow identity carve-out specifically for `0x1B` (not a general "unknown = pass through" change, which would mask genuine corruption elsewhere).
- **The `.DBT` memo reader (`dbf/memo_dbaseiv.go`) implements the 8-byte-header format found via S13**, and discovered something new while testing it: the block-0 header is self-describing, carrying the table's own name and block size explicitly — not documented anywhere, found empirically and cross-checked against three real 1994 specimens (`CLIENT.DBT`, `CONTENTS.DBT`, `ORDERS.DBT`) plus the write-oracle, all four agreeing exactly.
- **Verified against real 1994 data, not just the write-oracle.** `CLIENT.DBT`'s third record carries a 580-byte memo — longer than one 512-byte block's usable capacity — and decodes correctly across the block boundary, turning what had been documented as an untested multi-block assumption into a verified one.
- **Write support deliberately scoped out, not silently dropped** — filed as **T-37**, fully specified, roughly half a day.
- 11 new tests across `dbf/dbasetypes_test.go` and `dbf/memo_dbaseiv_test.go`, all oracle-verified: the live write-oracle, three real 1994 specimens, and a full sweep of all 33 vendored tables.

Cross-ref: CHANGELOG 0.9.21, dbf/dbasetypes.go, dbf/memo_dbaseiv.go, dbf/header.go, T-37.


## [0.9.16] T-35. Verify _NullFlags bit counting with V/Q fields present (v0.9.16, 2026-07-24)

Theme: dbf · Priority: P3 · Status: ✓ closed by implementation — bit algorithm fully solved and oracle-verified; V/Q exact content decode refiled as T-36

- **Trigger:** a book chapter on VFP 9's new data types ("What's New in VFP 9", Hentzenwerke, S10 in `docs/VFP30_FORMAT.md`) contains a worked, byte-exact example — a synthetic table with seven inserted records, each with its exact raw bytes and resulting `_NullFlags` value documented in the text.
- **The complete algorithm, verified against all seven records with an exact match:** each Varchar/Varbinary field allocates a "full" bit (0=content fills the field, 1=shorter, actual length in the field's last byte) regardless of nullability; each nullable field (any type) allocates a "null" bit; a field that is both gets both, full-bit first, adjacent; a NULL value sets both bits. This generalises the v0.9.14 result (one bit per nullable field) rather than replacing it — that result was correct on every specimen tested because none contained a Varchar/Varbinary field.
- **`dbf/nullflags.go` rewritten** to the complete algorithm. `Record.IsFull` added alongside the existing `Record.IsNull`, exposing the full-bit directly.
- **Oracle is the worked example itself, reproduced byte-for-byte** in `TestNullFlagsWorkedExample` — a fully synthetic, self-contained, reproducible test needing no external specimen, since the source states the exact bytes.
- **No regression**: the five real Northwind fields verified in T-34 (orders.dbf, suppliers.dbf) still pass under the corrected algorithm, as expected — none contain V/Q fields, so the correction is a no-op for them.
- **A documentation error from v0.9.15 corrected.** That release stated V/Q's unused tail bytes pad with `0x00` (NUL), based on source S9's `SET EXACT` comparison-semantics statement. S10 states directly and unambiguously — with the worked hex bytes to prove it — that Varchar and Varbinary **pad with spaces on disk**, exactly like Character. The `0x00` claim was a real fact, but about runtime comparison behaviour (zero-padding a shorter expression during a comparison operation), not physical storage; misapplied to storage in the earlier pass.
- **`Varchar`/`Varbinary` field types implemented**, decode/encode wired through `isSupportedType`, schema validation, and `validValue`. Decode is a documented approximation (space-trim, correct when full or when content has no significant trailing spaces); write path is a documented safe subset (always writes full-width content, so the corresponding full-bit is always correctly 0). Exact content decode for the not-full/significant-trailing-space case refiled as **T-36**, since it needs a `decodeRecord` restructuring not attempted in this pass.
- 8 new tests: the worked-example reproduction, `IsFull`'s API guard, plus the `Varchar`/`Varbinary` type wiring exercised through existing schema/validValue test paths.

Cross-ref: CHANGELOG 0.9.16, dbf/nullflags.go, dbf/vfptypes.

## [0.9.14] T-34. VFP _NullFlags: bit ordering across fields, W Blob type (v0.9.14, 2026-07-24)

Theme: dbf · Priority: P3 · Status: ✓ closed by implementation — bit ordering solved, W implemented; V/Q interaction refiled as T-35

- **Trigger:** the user directly challenged the framing that VFP gaps were blocked on missing specimens, pointing out the VPFX-Samples mirror was already available. Auditing systematically found real, unread data resolving three separate things at once.
- **_NullFlags bit ordering, fully solved.** Method: correlate each nullable field's actual blank/non-blank content against every candidate bit position, across every record, for fields that are genuinely sometimes-null (not vacuously always/never). Confirmed independently on five fields across two real Northwind tables (`orders.dbf`: SHIPPEDDAT, SHIPREGION, SHIPPOSTAL; `suppliers.dbf`: REGION, FAX) with **zero exceptions across 830 and 29 records respectively**. Result: bit N corresponds to the Nth nullable field in declaration order.
- **`Record.IsNull(schema, name)` implemented** (`dbf/nullflags.go`), oracle-verified end-to-end through the public API — not just the derivation script — against the same real data.
- **A real correction found along the way:** the `_NullFlags` field-type byte is ASCII `'0'` (0x30), not the null byte `0x00` as earlier documentation (carried from secondary sources) had stated. Corrected against direct observation of real field descriptor bytes.
- **`W` (Blob) implemented as an alias for `General`'s exact encoding**, confirmed by fetching `photos.fpt` directly: the `MEDIA` field's three real block pointers led to payloads starting with a BMP header and two JPEG SOI/JFIF/EXIF headers respectively, stored under the same generic FPT block signature Memo uses. Not a new format — no new decoder needed.
- **A genuine remaining gap, stated rather than hidden:** the bit-counting logic assumes one bit per nullable field, but VFP 9 SP2's own docs (S7) say Varchar/Varbinary fields consume two bits each. No specimen with a `V`/`Q` field was found this session, so that interaction is untested and could silently miscount. Refiled as **T-35** rather than left as an unstated assumption in shipped code.
- 8 new tests across `dbf/nullflags_test.go` and the `Blob` addition to `dbf/vfptypes_test.go`, all oracle-verified against real vendor/sample data.

Cross-ref: CHANGELOG 0.9.14, dbf/nullflags.go, dbf/vfpty

## [0.9.13] T-33. Check whether VFP DateTime shares dBASE 7's Timestamp epoch (v0.9.13, 2026-07-24)

Theme: dbf · Priority: P3 · Status: ✓ closed by implementation — DateTime shipped

- **Trigger:** dBASE 7's `@` Timestamp epoch, found and documented in the same session (S6), gave a concrete, checkable claim: Julian day since 1 January 4713 BC plus milliseconds since midnight.
- **Settled by decoding a real VFP 9 specimen directly**, not by inference. `photos.dbf` (`ha1tch/VPFX-Samples/Solution/Europa/photos.dbf`), column `CREATED`, three records. Decoded under dBASE 7's exact formula: **2004-10-12, 14:03:30 / 14:20:06 / 14:22:05** — three photos taken minutes apart on one afternoon, which is what real sample-data timestamps look like rather than what a coincidence looks like. VFP 9 shipped in 2004.
- **This corrects an overstatement made earlier in the session**: T-25's original closure said DateTime was blocked because "no specimen carries the type," without qualifying that the specimen existed, was located, and simply hadn't been fetched and decoded yet. The user directly challenged the claim ("we have VFP 9 specimens in the cloned repo") before this was checked.
- **Implemented in `dbf/vfptypes.go`**: `DateTime FieldType = 'T'`, `decodeJulianDateTime`/`encodeJulianDateTime` (standard Fliegel & Van Flandern algorithm), wired into `decodeVFPValue`/`encodeVFPValue`, `isSupportedType`, `isVFPType`, `vfpFieldWidth`, and `schema.go`'s width-validation switch.
- **Found and fixed a real pre-existing bug while wiring the write path.** `record.go`'s `validValue` — the gate behind `Record.Set` — had never been extended for *any* VFP binary type (Integer, Double, Currency, General), only `Date`. Every prior VFP-type test exercised `decodeVFPValue`/`encodeVFPValue` directly and never went through the public `Set`/`Append` path, so this went uncaught since T-25. `TestVFPValuesSettableThroughPublicAPI` is the regression test; `validVFPValue` in `vfptypes.go` fixes all five types together.
- **Version-byte gate widened to `0x31`/`0x32`** (autoincrement-enabled and Varchar/Varbinary/Blob-enabled VFP), matching `0x30`'s existing treatment exactly — needed because the real oracle specimen (`photos.dbf`) is `0x32`. This was already flagged in `docs/ROADMAP.md` as the cheapest remaining VFP gap; done here as a side effect of needing it for the oracle test rather than as separately scheduled work.
- **The zero-date sentinel is handled explicitly**: both fields `0x00000000` decodes to Go's zero `time.Time` rather than through the Julian formula, which would otherwise produce a nonsense proleptic-calendar date for day 0.
- **`_NullFlags` refiled separately as T-34**, since it was the other half of the same original deferral and deserves its own tracked item now that DateTime has closed.
- 5 new tests plus the regression test for the `validValue` bug; oracle-verified against the real `photos.dbf` bytes directly (not through `dbf.Open`, since that file also carries an unimplemented `W` Blob field — a separate, unrelated gap, not routed around).

Cross-ref: CHANGELOG 0.9.13, dbf/vfptypes.go, docs/RESEARCH_NOTES.md, 

## [0.9.11] T-32. MDX numeric key encoding (v0.9.11, 2026-07-24)

Theme: mdx · Priority: P3 · Status: ✓ closed by implementation — 4-significant-digit scope, bounded deliberately

- **Trigger:** T-30's original resolution recorded 5 undecoded sample values from `OLDBALANCE` and declined to guess, on the same grounds as the VFP DateTime epoch — a plausible wrong encoding passes inspection and corrupts comparisons silently.
- **Cracked empirically, not from a source.** No documentation was found anywhere this session covering MDX numeric encoding — Microsoft's FoxPro-family archives don't cover dBASE-lineage MDX at all. The method: dBASE stores `Numeric` DBF fields as plain ASCII text, so every numeric-tagged `.MDX` specimen's ground truth was directly readable from its paired `.DBF`. Cross-referenced `CODES.MDX`/`AREACODE` (39 records, integers 202–818) and `ACCT_REC.MDX`/`OLDBALANCE` (5 records, including negative and zero) against their DBF text — **44 keys total, 44/44 exact matches** against the derived formula.
- **A third, distinct encoding** — not NDX's plain IEEE double, not CDX/IDX's byte-reversed transformed double. Normalized BCD floating-point: byte 0 is a biased decimal exponent (bias 53, `floor(log10(|v|)) + 53`), byte 1 is a constant `0x29` marker with the sign bit set for negatives, bytes 2–3 are four significant digits nibble-packed, bytes 4–11 are zero. Zero itself is a sentinel (`0x34 0x01` + zeros), confirmed by exactly one specimen.
- **A real design consequence, caught before shipping:** this encoding is **not byte-comparable** — the exponent byte grows with magnitude regardless of sign, so raw `bytes.Compare` would sort `-1000` after `-1`. `mdx`'s `Build`/`AddTag` used `bytes.Compare` universally for all key types; it now dispatches through `compareKeys`, which decodes and compares numerically for `Numeric` tags. `TestBuildNumericTag` exercises this directly and would have failed silently under the old comparator.
- **Scope bounded deliberately to what 44 keys verify.** `EncodeNumericKey` refuses (`ErrNumericUnsupported`) any value needing more than 4 significant decimal digits, rather than rounding and producing a plausible-but-unverified key. Untested: 5+ significant digits, tags with `ItemSize` other than 16 (possibly different mantissa precision), exponent range edges, and the zero sentinel beyond its single confirming data point.
- **Oracle-verified**, not just self-consistent: `TestNumericKeyOracle` reads the real vendor-written `ACCT_REC.MDX` and confirms all 5 `OLDBALANCE` values decode correctly and sort in true numeric order.
- 4 new tests in `mdx/numeric.go`'s test coverage, on top of the existing suite.

Cross-ref: CHANGELOG 0.9.11, mdx/numeric.go, docs/RESEA

## [0.9.9] Add CDX/IDX transformed numeric key codec (v0.9.9, 2026-07-24)

Theme: cdx · Priority: — · Status: ✓ closed by implementation

- **Trigger:** reviewing new Microsoft primary-source documentation for gaps it exposed in the code, not just the docs. Found `cdx`/`idx` had zero numeric-key handling — no encode/decode helpers at all — while `ndx` already has `EncodeNumericKey`/`DecodeNumericKey` for its own (different) plain-IEEE format. A caller building a numeric CDX or IDX index had no reference implementation and no guard against reaching for `ndx`'s encoding by mistake.
- **Implementation:** `cdx.EncodeNumericKey`/`DecodeNumericKey`, the algorithm now primary-sourced (Microsoft VFP 7.0 archived docs, `docs/INDEX_FORMATS.md`): IEEE double, byte-reversed to big-endian, then all bits inverted if negative or only the sign bit if not. `idx` shares it via `cdx`, matching the existing `WriteLeaf` reuse pattern.
- **Verified two ways.** Round-trip across positive/negative/zero/fractional values, and — the test that actually matters — sorting encoded byte strings reproduces numeric order across the sign boundary, which is the whole purpose of the transform.
- **Cross-checked against the known hazard.** `cdx.EncodeNumericKey(-100)` and `(100)` byte-compare correctly (`-100 < 100`); the same two values through `ndx.EncodeNumericKey` byte-compare *incorrectly*, matching `ndx`'s own guard test. Confirms the two codecs are deliberately different and both correct for their respective formats.
- 4 tests.

Cross-ref: CHANGELOG 0.9.9.

## [0.9.4] T-30. MDX index support (dBASE IV multi-tag) (v0.9.4, 2026-07-24)

Theme: mdx · Priority: P3 · Status: partial — Character/Date verified, Numeric explicitly unsupported

- Implemented single-leaf multi-tag .MDX: file header, 48-slot tag directory, per-tag header, per-tag leaf. Scope matches idx/cdx Phase 1 (single leaf per tag).
- Oracle: ACCT_REC.MDX, Borland dBASE 5.0 for DOS (1994), vendored at dbf/testdata/dbase5/full/ — vendor-written, not a Clipper-generated fixture, the strongest provenance available for this format this session.
- Verified: 2 Character tags (CUST_ID width 6, INVOICE_NO width 10) decode with correct sort order and record numbers.
- **Two undocumented details found empirically, not in docs/INDEX_FORMATS.md:**
  - Leaf entry stride is the tag header's ItemSize field (align4(4+KeySize)), not simply 4+KeySize. First attempt at 4+6=10 produced garbage after entry 0; correct stride is 12.
  - The header+tag-table region is a fixed 4 pages (2048 bytes) regardless of the declared 48-tag capacity — the first real tag in the specimen begins at page 4 even with only 3 tags in use. The 48-entry/32-byte fields declare a maximum, not a physical allocation.
- **Numeric key encoding explicitly NOT implemented.** OLDBALANCE (N, 9 digits) stores as 12 raw bytes, not an 8-byte IEEE double as CDX/NDX use. No documentation or oracle available this session establishes what encoding this is. Tags are listed and their type reported; AddTag refuses Numeric; TagEntries returns raw undecoded bytes rather than guessing.
- 5 tests including the oracle fixture.

Cross-ref: CHANGELOG 0.9.4.


## [0.9.3] T-29. IDX index support (FoxPro 2 single-key) (v0.9.3, 2026-07-24)

Theme: idx · Priority: P3 · Status: ✓ closed — compact-format scope only

- Implemented compact-format `.IDX` (index-options `0x20`), the only layout the available oracle (`DBFCDX.LIB`) produces. Uncompressed IDX remains unimplemented; no generator found.
- Reuses `cdx`'s compact-leaf bit-packing rather than reimplementing it — `cdx.WriteLeaf` exported, decode mirrored standalone since `cdx`'s decoder is coupled to its internal node type.
- Scope matches `cdx` Phase 1: single-leaf trees only (root + one 512-byte leaf page).
- **Root/free/eof header fields are byte offsets, not page numbers** — found via the oracle test failing until this was corrected; docs/INDEX_FORMATS.md doesn't state this explicitly.
- Oracle fixture: `idx/testdata/BYCODE.IDX`, Clipper 5.2e DBFCDX output, verified header fields and leaf decode.
- 6 tests.

Cross-ref: CHANGELOG 0.9.3.


## [0.9.0] T-28. NDX index support (dBASE III+ single-key) (v0.9.0, 2026-07-23)

Theme: ndx · Priority: P3 · Status: ✓ closed by implementation

- **Trigger:** index-format review, 2026-07-23.
- **Oracle available and unused.** `DBFNDX.LIB` sits in the Clipper 5.2e toolchain alongside the `DBFNTX` and `DBFCDX` drivers that produced verified fixtures three times this session. The `ANNOUNCE RDDSYS` / `rddSetDefault` harness needs only the driver name changed. See `docs/RESEARCH_NOTES.md` for the exact invocation.
- **Simplest of the three remaining index formats**, and the most directly useful: `.NDX` is dBASE III+'s own index and appears alongside the `0x03` tables that make up the entire Clipper corpus.
- **The layout is documented** in `docs/INDEX_FORMATS.md`: 512-byte pages, header fields through offset 23, the page entry triple of lower-level pointer, record number and key data, and the key-record sizing rule (a multiple of four: 4 + 4 + key size rounded up).
- **Independent corroboration for NTX as a side effect.** The same source describes NTX, and its account agrees with blipper's oracle-derived `ntx` implementation on every comparable point — 1024-byte pages, key expression as text in the header, unique flag, the entry triple. Two independent derivations reaching the same layout is stronger than either alone.
- **Numeric keys are IEEE doubles**, plainly stored — unlike CDX, which applies a byte-order and bit-inversion transform. Worth not carrying an assumption across from one format to the other.
- **Scope:** read and write, following the `ntx` package's shape. Oracle verification byte-for-byte, as with NTX.
- **Effort:** roughly 3 days.

- **Implementation (v0.9.0).** New `ndx` package: `Open`, `Create`, `Build`, `Traverse`, `Entries`, `Seek`, `First`, `Last`, `Count`, plus `EncodeNumericKey`/`DecodeNumericKey`. 12 tests.
- **The document was verified before a line was written**, and it held on first decode. Every header field matched the Clipper oracle for both key types, including the key-record sizing rule — 4 bytes of lower-level pointer, 4 of record number, and the key rounded up to a multiple of four, giving 20 for a 10-byte character key and 16 for an 8-byte numeric one. That is unusually clean for a reconstructed specification and worth recording as a point in its favour.
- **Two fixtures, not one, and the reason matters.** `BYCODE.NDX` is a character index and `BYNUM.NDX` a numeric one. The two key types compare differently, so a character-only fixture would not catch an implementation that compared numeric keys as bytes. Both were generated over a table whose records were appended in deliberately unsorted order, so an implementation that merely preserved append order fails rather than passes.
- **Numeric keys are plain IEEE-754 doubles**, unlike CDX, which transforms them so byte comparison yields numeric order. `TestNumericOrderingIsNotByteOrdering` demonstrates the hazard concretely with −100 against 100, where little-endian byte comparison disagrees with numeric order because the sign bit sits in the byte that storage places last. The failure mode is the dangerous kind: byte comparison agrees often enough to survive casual testing and disagrees on negatives.
- **Build packs a balanced tree in one pass** rather than inserting keys individually, matching how dBASE's own `INDEX ON` behaves — read the table, write the index whole. Interior nodes carry separator entries with a record number of zero, which the traversal skips.
- **A header that disagrees with itself is refused.** The record size is derivable from the key length, so a mismatch means either corruption or a format this package does not understand; reading on would misinterpret every entry.
- **A traversal guard against corrupt files.** The walk carries a visited set, because a page pointing back into its own ancestry would otherwise exhaust the stack rather than report the problem.

Cross-ref: CHANGELOG 0.9.0, docs/INDEX_FORMATS.md.

## [0.8.6] T-25. VFP 3.0 read support: 0x30 tables and four field types (v0.8.6, 2026-07-23)

Theme: dbf · Priority: P3 · Status: ✓ closed by implementation

- **Trigger:** T-24 closure, 2026-07-23. Specimens obtained and the format consolidated in `docs/VFP30_FORMAT.md`; this is what that made buildable.
- **Scope, bounded by what the specimens verify.** Accept version byte `0x30` on `Open`; decode `Currency (Y)`, `Integer (I)`, `Double (B)`, and `General (G)`; read field-descriptor byte 18 and expose the flags. All four types appear in the 129 vendor files staged from the VFP 3.0 distribution, so each decoder can be checked against real data.
- **Currency's scale falls out of its range and is worth stating.** Microsoft documents ±922337203685477.5807, which is exactly (2⁶³−1)/10⁴. So Currency is a 64-bit signed integer with four implied decimal places — neither source says so directly.
- **Field displacement, bytes 12–15, should be used.** The descriptor states each field's offset in the record rather than requiring the reader to sum preceding widths. blipper currently sums. The two agree for well-formed files; using the stated value is more robust and costs nothing.
- **Deliberately excluded, and for a reason rather than for scope.**
  - **`_NullFlags`.** Position and semantics are documented — last field, system-flagged, one bit per nullable column — but byte 18 is `0x00` in all 129 specimens, so nothing exercises it. Bit ordering under column changes is documented nowhere.
  - **`DateTime`.** Eight bytes as two four-byte integers is documented; the epoch and the midnight convention are not, and no specimen carries the type. The failure mode is dates wrong by one day, which passes inspection and corrupts records quietly — the same shape as the FPT block-numbering bug in v0.4.0.
  Both need a table **created** by running VFP. Implementing them from inference would repeat exactly the mistake the oracle exists to prevent.
- **Writing `0x30` stays out of scope entirely.** Reading a VFP table is safe; writing that version byte is a promise to honour field types and null semantics blipper would not fully implement. T-10's sidecar deliberately writes `0x03` for the same reason, and that decision should hold until the exclusions above are closed.
- **Effort:** roughly 3 days. Day 1 the version gate and byte-18 plumbing; day 2 the four decoders with fixture tests; day 3 integration and the compatibility-table update.

- **Implementation (v0.8.6).** `dbf/vfptypes.go` with the four binary types, wired into `isSupportedType`, `Schema.validate`, and the record codec. 8 tests.
- **All four verified against vendor data, not only round trips.** `CUSTOMER.DBF` from Microsoft's own `TESTDATA` sample database decodes `MAX_ORDER_` as 6300.0000 and `MIN_ORDER_` as 2600.0000 — real Currency values written by VFP itself. This is the distinction that mattered in v0.4.0: a round-trip test passes even when encode and decode are wrong in the same direction.
- **Currency is kept as a scaled integer, not a float.** `CurrencyValue` wraps the raw `int64`. The documented range needs 63 bits and a `float64` mantissa carries 53, so converting on read would silently lose precision on monetary data — the one kind of data where that is least acceptable. `Float64()`, `Scaled()`, and `String()` let callers choose.
- **The scale is inferred, and the inference is recorded.** No source states that Currency is scaled by 10⁴; it follows from the documented range ±922337203685477.5807 being exactly (2⁶³−1)/10⁴.
- **A finding worth noting: memo fields have two legal widths.** dBASE III+ and FoxPro 2 store the block number as a 10-byte ASCII string; VFP stores a 4-byte little-endian integer. `Schema.validate` previously rejected anything but 10. Confirmed across the vendor specimens, where every VFP memo field is 4 bytes and every dBASE one is 10 — a case where having real files caught something the specification does not spell out.
- **DateTime and `_NullFlags` remain excluded**, for the reasons T-24 established rather than for scope: neither appears in any of the 136 vendor files, and both would have to be implemented from inference. The failure mode for DateTime — dates wrong by one day — is the same shape as the FPT block-numbering bug.
- **Writing `0x30` stays out of scope**, unchanged. Reading a VFP table is safe; writing that version byte promises semantics blipper does not fully implement.

Cross-ref: CHANGELOG 0.8.6, docs/VFP30_FORMAT.md.

## [0.8.5] T-26. Complete the code page table against the authoritative source (v0.8.5, 2026-07-23)

Theme: dbf · Priority: P2 · Status: ✓ closed by implementation

- **Trigger:** finding the authoritative table, 2026-07-23. T-21 shipped 16 code pages assembled from secondary sources; the VFP 9 Help page "Code Pages Supported by Visual FoxPro" documents **26**, and comparing them shows nine identifiers blipper does not name at all.
- **Missing entirely, and the CJK ones matter most:** `0x7B` CP932 Japanese, `0x7A` CP936 Chinese Simplified, `0x79` CP949 Korean, `0x78` CP950 Traditional Chinese, `0x96`/`0x97`/`0x98` the Macintosh Cyrillic/EE/Greek variants, `0x68` CP895 Kamenicky Czech, `0x69` CP620 Mazovia Polish. A file declaring any of these currently fails `Open` with "unsupported code page", which is at least honest but unnecessary — `x/text/encoding/japanese`, `simplifiedchinese`, `korean`, and `traditionalchinese` provide all four CJK tables.
- **Priority is P2 because the failure is silent for the caller.** A Japanese or Korean xBase file is not exotic; FoxPro shipped localised versions and those markets used them heavily. Refusing to open such a file is a worse outcome than the encodings gap T-21 was filed to close.
- **The three already named but unmapped stay unmapped.** CP861 Icelandic, CP857 Turkish MS-DOS, CP737 Greek MS-DOS have no `charmap` table, and substituting a near neighbour would decode most of a file correctly — the kind of nearly-right that hides a problem. They are named so a file declaring one reports meaningfully rather than as "unknown 0x67".
- **Two the Help marks as undetectable under `CODEPAGE=AUTO`** (`0x68`, `0x69`, plus `0x6A`, `0x96`) are worth recording as such in the table, since it explains why a file might carry a code page its own tooling would not have inferred.
- **Effort:** half a day. The identifiers, the four CJK encodings, and a test asserting blipper's table matches the documented 26 either by mapping or by explicit named refusal.

- **Implementation (v0.8.5).** Nine identifiers added, four CJK encodings mapped, 2 tests.
- **The four CJK encodings are the substance.** `japanese.ShiftJIS`, `simplifiedchinese.GBK`, `korean.EUCKR`, `traditionalchinese.Big5`. These are multi-byte and live outside `charmap`, which is why they were missed when the table was assembled from secondary sources. FoxPro shipped localised versions for those markets, so the files are not exotic — blipper previously refused to open them at all.
- **`TestCodePageTableMatchesVFPDocumentation` pins the table to the specification**, and that is the more durable result than the additions themselves. It asserts every one of the 26 documented identifiers is *named*, so a declared-but-unmapped code page reports meaningfully instead of "unknown 0x67", and so this table cannot drift again. It already had drifted once: T-21 shipped 16 from secondary sources and nine were missing.
- **Three stay deliberately unmapped.** CP861 Icelandic, CP857 Turkish MS-DOS, CP737 Greek MS-DOS have no `charmap` table. Substituting a near neighbour would decode most of a file correctly — the kind of nearly-right that hides a problem rather than surfacing it.
- **Four identifiers are marked in the source as undetectable under `CODEPAGE=AUTO`**, recorded in `docs/VFP30_FORMAT.md` because it explains how a file can carry a code page its own tooling would not have inferred.

Cross-ref: CHANGELOG 0.8.5, docs/VFP30_FORMAT.md.

## [0.8.4] T-24. Establish whether VFP 3.0 specimens are obtainable (closed by finding, 2026-07-23)

Theme: dbf · Priority: P3 · Status: ✓ closed by finding — specimens obtained; follow-on filed as T-25

- **Trigger:** VFP 3.0 scoping, 2026-07-23. The implementation estimate is about a week for read-only, and most of the format is already built — CDX, FPT, DBC, byte-28 signalling and code pages all landed for other reasons. What is not established is whether the result could be *verified*, and that determines whether the work is possible rather than merely how long it takes.
- **This is filed as an investigation, not an implementation.** The T-11 shape: the outcome is either a specimen source and a follow-on item, or a closure recording why VFP 3.0 stays unsupported. Filing the implementation first would repeat the mistake T-11 made — assuming the verification question was answerable and discovering otherwise afterwards.
- **Why a specification is not enough here, specifically.**
  - **`_NullFlags` is underdocumented in exactly the places that matter.** The published spec says a hidden system field carries a bitmap with one bit per nullable column. It does not say whether that field is always last, how bit ordering behaves when columns are added or dropped, or how the bitmap interacts with the field-descriptor byte-18 flags. Those are questions a file answers and a document does not.
  - **The `DateTime` encoding has a trap with a bad failure mode.** Two 32-bit words holding a Julian day and milliseconds since midnight — but the epoch and the midnight convention are the sort of detail that, got wrong, produces dates correct to within a day. That passes casual inspection and corrupts a decade of records subtly, which is precisely the class of bug the FPT oracle caught in v0.4.0 when round-trip tests had reported green.
- **The corpus has been checked and has none.** A scan of all 120 files at `github.com/ha1tch/clipper` finds 117 × `0x03` and 3 × `0x83` — uniformly Clipper, no VFP-era or dBASE IV+ version bytes at all. That was the cheapest possible source and it is exhausted.
- **Candidate sources, in order of cost:**
  1. **Public shapefile data.** ESRI shapefiles carry a `.dbf` sidecar, and some government GIS exports were produced by VFP-era tooling. Free, real, and a plausible source of unusual field types.
  2. **The test corpora of `go-dbase` and `DbfDataReader`.** Both projects test against VFP files and both are public. Fixtures from a project that verified against them independently are worth more than fixtures we generate ourselves.
  3. **A VFP 3.0 installation under DOSBox or a Windows VM**, generating specimens the way Clipper 5.2e already does. Heaviest option, but it is the one that yields an oracle rather than a fixture — and an oracle is what caught the CDX and FPT bugs.
- **A specimen is not automatically an oracle.** A found file proves what a real VFP writer produces; it does not let us verify what blipper *writes*. Read-only support needs specimens; write support needs source (3). That distinction should be settled before any implementation item is sized, because it is the difference between a week and a fortnight.
- **What closure looks like either way.** Success: a fixture set with recorded provenance, plus a follow-on implementation item scoped by what those fixtures actually contain. Failure: a closure note recording where we looked and why VFP 3.0 stays at `✗` in the compatibility table — which is a genuinely useful thing to have written down, since the question will recur.
- **Effort:** one day for sources (1) and (2). Source (3) is its own item if it comes to that.

- **Answered: yes, specimens exist and are obtainable.** Microsoft's own VFP 3.0 installation media is on the Internet Archive (`archive.org/download/ms-vfp30/`, 21 MB ISO). Extracting `VFP1.CAB` yields 700 files, of which **129 carry version byte `0x30`** and 5 carry `0xF5`. That is the strongest possible provenance — vendor-written files from the product itself.
- **The candidate list resolved differently than expected.** Shapefiles were ruled out conclusively, and the reason is structural rather than bad luck: four independent producers were sampled — GADM, ArcGIS via the USGS geothermal group, Landsat Missions 2018, and the earlier corpus — and every one wrote `0x03`. **The shapefile specification effectively mandates dBASE III+ compatibility**, so no conforming writer emits `0x30` whatever software produced it. No amount of further sampling would have changed that, and it should not be retried.
- **Documentation turned out to be the harder half, and it is now consolidated.** `docs/VFP30_FORMAT.md` gathers what was found across four sources of varying durability: KB Q130461 (preserved only in one volunteer's GitHub archive), the VFP 9 SP2 Help file (Creative Commons, community-hosted, rate-limits at 503), Microsoft Learn's archived data-types page (`is_archived: true`, `NOINDEX,NOFOLLOW`), and the ISO itself. **None of the first three is durable**, which is why the material was written down rather than merely linked.
- **The decisive find was field-descriptor byte 18**, documented in full only in the VFP 9 Help: `0x01` system column, `0x02` nullable, `0x04` binary, `0x0C` autoincrementing — plus the note that the null bitmap lives in **the last system field**. That is the question the KB archive is silent on; `_NullFlags` appears **zero times across all 3,879 FoxPro KB article titles**.
- **`30DBC.DBF` in the distribution is Microsoft's own field-by-field specification of the DBC schema**, and it confirms what T-10 implemented from secondary sources this morning: `OBJECTID I(4)`, `PARENTID I(4)`, `OBJECTTYPE C(10)`, `OBJECTNAME C(128)`, `PROPERTY M`, `CODE M`, `RIINFO C(6)`, `USER M`. A design reconstructed from inference, now verified against the primary source.
- **Two gaps remain, and they are gaps in specimens rather than documentation.** Byte 18 is `0x00` in every field of all 129 files, so nothing exercises `_NullFlags`; and no file carries a `DateTime` field, so the epoch convention stays unverified. These are VFP's own metadata tables — Microsoft had no reason to make their columns nullable. Closing both needs a table **created** by running VFP, not found by reading its distribution.
- **Three specimens staged** at `dbf/testdata/vfp/` with provenance and a licensing note: `30DBC.DBF` (the DBC spec), `30PJX.DBF`, and `26PJX.DBF` for FoxPro 2.6 comparison. Everything is re-derivable from the ISO in two commands, recorded in both READMEs.
- **Follow-on filed as T-25**, scoped by what is actually verifiable: read-only support for `0x30` plus Currency, Integer, Double and General — all four present in the specimens — with `_NullFlags` and `DateTime` explicitly deferred until a table with those features can be obtained.

Cross-ref: docs/VFP30_FORMAT.md, T-25.

## [0.8.4] T-23. blipperfs: expose concrete backends beneath the FileSet interface (v0.8.4, 2026-07-23)

Theme: blipperfs · Priority: P3 · Status: ✓ closed by implementation

- **Trigger:** API review, 2026-07-23, following the same question that produced T-22. The design intent is autopilot for the common case and manual control where needed; T-22 fixed one place the manual path was weaker than the layer beneath it, and this is the remaining one.
- **The gap.** `OSDir` returns the `FileSet` interface, so a caller who needs the concrete backend — to reach a lockable handle, or anything OS-specific — must type-assert against an unexported type they cannot name. `FATImage` and `SQLiteTablespace` have the same shape: they return `FileSet`, and the concrete `fatFileSet`/`sqliteFileSet` expose `Volume()` and `Store()` methods that are unreachable without an inline interface assertion.
- **This is the escape-hatch principle, not a missing feature.** Every layer should be usable on its own and none should be a strictly weaker interface than what it wraps. A caller who wants a `fatfs.Volume` can call `fatfs.OpenImage` directly, so nothing is impossible today — but having gone through `blipperfs`, they should not have to unwind back to the driver to get at it.
- **Scope.** Small named interfaces for the capabilities a backend may expose, in the shape `Flusher` already established: something a caller can type-assert against by name rather than by inline literal. Concrete accessors where a backend has one thing to hand back.
- **Documentation is the larger half.** The four access levels — `OpenDir`, `NewSession` with a custom `FileSet`, package-level `Use`/`CreateTable` with an explicit `db`, and the format packages on bare streams — exist and work, but nothing states them together, so the manual path is discoverable only by reading source. A doc.go covering the layering belongs with this.
- **Effort:** half a day, most of it documentation.

- **Implementation (v0.8.4).** Named capability interfaces `FATBacked`, `SQLiteBacked`, `DirBacked` alongside the existing `Flusher`; `osFileSet.Root()`; compile-time assertions binding each backend to what it claims; and a `doc.go` stating the four access levels. 5 tests.
- **The interfaces are named so the assertion documents intent.** `fs.(FATBacked)` says what the caller wants; an inline `fs.(interface{ Volume() *fatfs.Volume })` says the same thing while obscuring it. This follows `Flusher` and `Compactable`: an interface describes a property some implementations have, not a duty all of them owe.
- **Compile-time assertions matter more than they look.** Without `var _ FATBacked = (*fatFileSet)(nil)` the interfaces are aspirational — a rename or signature change breaks the assertion silently at every call site instead of loudly here.
- **`OSDir` is deliberately not a `Flusher`,** and a test asserts that absence. The operating system already provides the guarantee; implementing a no-op would leave a reader wondering what it was for.
- **The documentation was the larger half, as the item predicted.** The four levels — `OpenDir`, `NewSession` with a custom `FileSet`, package-level `Use`/`CreateTable` with an explicit `db`, and the format packages on bare streams — all existed and worked, but nothing stated them together, so the manual path was discoverable only by reading source. `TestAllFourAccessLevelsWork` exercises each one, so the claim is tested rather than asserted in a comment.
- **`docs/FAMILY_COMPATIBILITY.md` gained a comparable-libraries section** while the survey was fresh: a feature matrix against `go-dbase`, `go-foxpro-dbf`, and `DbfDataReader`, with honest notes on where blipper is behind (struct/JSON conversion, VFP field types, maturity) as well as ahead (indexes, oracle verification, storage backends, database operations). The dBASE III+ and FoxBASE+ rows were promoted from `partial` to `✓` now that T-07 and T-08 have closed.

Cross-ref: CHANGELOG 0.8.4.

## [0.8.3] T-21. Code page support: decode DBF text via header byte 29 (v0.8.3, 2026-07-23)

Theme: dbf · Priority: P2 · Status: ✓ closed by implementation

- **Trigger:** competitive review, 2026-07-23. Comparable libraries (`go-dbase` in particular) support 13+ encodings with automatic code-page detection; blipper supports none, returning raw bytes as Go strings. Every real xBase file is CP437, CP850, or CP1252, so for actual production data this is the difference between reading a corpus correctly and reading it approximately — which is why it lands at P2 while most of the backlog is P3.
- **The information is already there and already read.** Byte 29 of the DBF header carries the language-driver identifier, and `readHeader` has been decoding it into `Header.CodePage` since the earliest versions. Nothing consumes it. The work is a lookup table and a decode step, not new parsing.
- **Scope.** A `Codepage` type mapping the identifier byte to a `golang.org/x/text/encoding.Encoding`, applied when decoding `Character` and `Memo` field values and when encoding them on write. Round-tripping matters as much as reading: a table read as CP850 and written back must produce the same bytes, or blipper corrupts every accented character it touches.
- **Default must not change existing behaviour.** Files carrying byte 29 = `0x00` — which is every Clipper file in the corpus, since DBFNTX does not write a language driver — have no declared encoding, and guessing one would be worse than leaving bytes alone. The default is therefore identity: bytes in, bytes out. Detection applies only when the byte says something.
- **Opt-in override.** A caller who knows their files are CP850 despite an empty byte 29 should be able to say so, since that is the common case for DOS-era Clipper data. Detection from the header, an explicit override, and identity default are three distinct behaviours and all three are wanted.
- **Effort:** roughly 2 days. Day 1 the table and the decode/encode path with round-trip tests over the corpus; day 2 the override plumbing and verification that a file with a declared code page round-trips byte-for-byte.

- **Implementation (v0.8.3).** `CodePage` type with 16 mapped identifiers, `textCodec`, decode and encode passes over `Character` and `Memo` fields, `Table.CodePage()` and `Table.SetCodePage()`. 9 tests.
- **The default is identity and that is the load-bearing decision.** Every Clipper file in the corpus carries byte 29 = `0x00`, because DBFNTX never wrote a language driver, so the overwhelmingly common case is a file with no declared encoding. Guessing one would corrupt data in a way that looks like success. `TestCorpusFilesStillDecodeIdentically` asserts a real corpus file gets the identity codec, and `TestCodePageDefaultIsIdentity` asserts high bytes survive untouched.
- **Encode is verified against stored bytes, not only round trips.** `TestCodePageWritesCorrectBytes` checks that `Ü` in a CP850 table is stored as `0x9A`. A round-trip test alone would pass even if decode and encode were wrong in the same direction, which is exactly the failure the FPT block-numbering bug turned out to be.
- **Decode failures are tolerated; encode failures are not.** A stray byte in one field should not make a table unreadable, and xBase files routinely carry rubbish in padding, so decode falls back to raw bytes. Writing a character the target code page cannot represent is an error, because substituting `?` would silently corrupt data.
- **Three identifiers are named but unmapped** — CP861, CP857, CP737 — because `x/text/charmap` has no table for them. They are named so a file declaring one produces a meaningful message rather than "unknown 0x67", and deliberately not substituted with a near neighbour: CP865 would decode most of a CP861 file correctly, which is the kind of nearly-right that hides a problem.
- **The override does not rewrite byte 29.** `SetCodePage` changes how a file is interpreted, not what it claims about itself; those are separate decisions and conflating them would have blipper quietly relabelling files. Asserted by test.

Cross-ref: CHANGELOG 0.8.3.

## [0.8.3] T-22. blipperfs: pass fatfs options through FATImage (v0.8.3, 2026-07-23)

Theme: blipperfs · Priority: P3 · Status: ✓ closed by implementation

- **Trigger:** review of the storage-backend surface, 2026-07-23.
- **The defect.** `fatfs.OpenImage` and `OpenImageRW` accept `...Option`, which is how `WithLongNames` is reached. `blipperfs.FATImage` and `FATImageRW` call them with no options and accept none themselves, so long-name support is invisible to anyone using the session layer — the layer T-14 built precisely so callers would not have to touch the driver. The capability exists and cannot be asked for.
- **Scope.** Both constructors take `...fatfs.Option` and forward them. One-line change each; the value is that it stops the session layer from being a strictly weaker interface than the driver underneath it.
- **Worth checking for the same shape elsewhere.** `SQLiteTablespace` already forwards `...sqlitefs.Option`, which is the pattern this should match. A sweep for other constructors that swallow their backend's options belongs in the same pass.
- **Effort:** under an hour including tests.

- **Implementation (v0.8.3).** Both constructors take `...fatfs.Option` and forward them. 2 tests.
- **The defect was that the session layer was a strictly weaker interface than the driver beneath it.** `fatfs.OpenImage` had always accepted options; `blipperfs.FATImage` called it with none and accepted none, so `WithLongNames` was unreachable for anyone using the layer T-14 built precisely so callers would not have to touch `fatfs`. The capability existed and could not be asked for.
- **The sweep the item called for found no other instance.** `SQLiteTablespace` already forwarded `...sqlitefs.Option`, and `OSDir` has no backend options to forward.
- **Documented the asymmetry while here.** Long names need no option on `OSDir` (the host filesystem decides) or `sqlitefs` (names are `TEXT`, holding arbitrary UTF-8, verified before T-17 was designed). Only FAT carries the 8.3 restriction that `WithLongNames` exists to lift, so "optional in all backends" would misdescribe it — in two of the three there is no restriction to make optional.

Cross-ref: CHANGELOG 0.8.3.

## [0.8.2] T-19. Concurrency: file locking and shared access (v0.8.2, 2026-07-23)

Theme: blipperdb · Priority: P2 · Status: ✓ closed by implementation (stage 1); stage 2 refiled as T-20

- **Trigger:** gap review after v0.8.0, 2026-07-23. Every type in the library documents itself as not safe for concurrent use, and means it: there is no locking of any kind. For a single-process tool that is honest and sufficient. For anything else it is a silent correctness hazard, which is why this is P2 while the rest of the backlog is P3 — a caller can be wrong without being blocked.
- **Prior art is precise here.** Clipper had a complete shared-access model: `USE ... EXCLUSIVE` versus `SHARED`, `FLOCK()` for the whole file, `RLOCK()` for the current record, `UNLOCK`, and `SET EXCLUSIVE ON/OFF`. Reproducing that vocabulary is better than inventing one, since the semantics are documented, understood, and match what the file formats were designed around.
- **The formats already reserve space for it.** DBF header byte 14 is the incomplete-transaction flag and byte 15 the encryption flag, with bytes 16–27 reserved for multi-user use. dBASE and FoxPro used region locks at conventional byte offsets rather than whole-file locks, so a compatible implementation locks the same regions rather than picking new ones — otherwise blipper and FoxPro would not see each other's locks.
- **Scope, in the order the layers depend on each other:**
  1. **A `Locker` interface** on the storage layer, since `FileSet` returns streams and only some backends can lock. `OSDir` uses `flock` or `fcntl`; `sqlitefs` has SQLite's own locking and needs only to expose it; a FAT image has no locking mechanism at all and must say so rather than pretend.
  2. **`Area.Lock` / `Unlock` / `RLock`**, mapping to Clipper's verbs, with a mode recorded at `Use` time.
  3. **Write paths respect the mode.** `Append`, `Put`, `Delete`, and `Pack` require an appropriate lock when the area is shared; today they simply write.
- **The honest hard part is that this is not just an API.** Blipper caches: `Table.recordCount` is held in memory, `cdx` caches nodes, `dbc` caches its whole row set, `fatfs` caches the FAT. Under shared access another process can invalidate any of that, so a correct implementation needs cache invalidation on lock acquisition — which is a bigger change than the locking calls themselves and should be scoped explicitly rather than discovered.
- **A staged delivery is probably right.** Exclusive-mode enforcement first, which is cheap and prevents the common accident of two writers; shared record locking second, which is where the cache question lands.
- **Verification.** The Clipper oracle can generate the locking behaviour to compare against, and Go's own test harness can drive two processes against one file. `testing/synctest` (Go 1.25) is available for the deterministic parts.
- **Effort:** roughly 1 week for exclusive-mode enforcement plus the `Locker` seam; longer for full shared access, which should be re-estimated once the cache-invalidation scope is known rather than guessed at now.

- **Implementation (v0.8.2), stage 1 as the item scoped it.** `OpenMode` with `Exclusive`/`Shared`, the `Locker` interface, `Area.FLock`/`RLock`/`Unlock`/`Locked`, `BlipperDB.UseMode`, POSIX record locks on `OSDir` handles, and enforcement on every write path. 11 tests.
- **Exclusive is the default and the zero value**, so every caller written before locking existed keeps working unchanged. That is deliberate: the common case is one process owning its data, and requiring a lock call for it would be ceremony. `Use` opens exclusive; a caller wanting shared access asks for it through `UseMode`.
- **Clipper's vocabulary throughout.** `FLOCK` and `RLOCK` map to `FLock` and `RLock`, `UNLOCK` to `Unlock`, `USE ... SHARED` to `UseMode(..., Shared)`. Those semantics are documented and understood by anyone who worked with these files; a new vocabulary would only obscure a well-mapped problem.
- **POSIX record locks via `fcntl`, not `flock`.** Byte-range locks are what per-record locking requires, and are what other database software on the same platform uses; `flock` locks whole files only. `F_SETLK` rather than `F_SETLKW`, so a conflict fails immediately and the blocking policy stays where the caller can see it.
- **The locks are proven real across processes**, which is the only way to prove anything about POSIX record locks: `TestRegionLocksAreRealAcrossProcesses` takes a lock, has a second process attempt the same range and confirms refusal, then releases and confirms the same probe succeeds. Without that second half the test would pass even if locking were permanently broken.
- **Enforcement distinguishes what is being written.** `Append` and `Pack` change the file and need `FLock`; `Replace`, `Delete`, `Recall` and `MemoSet` need a lock covering *that record*, so holding record 1 does not license writing record 2. Both directions are tested.
- **Storage that cannot lock says so** rather than pretending. A FAT image has no locking mechanism, so `FLock` there returns `ErrLockUnsupported`, following the `Flusher` precedent where a capability absent from an implementation is absent from its interface too.
- **A caveat recorded rather than discovered:** POSIX record locks are released when *any* descriptor for the file is closed by the process, and do not stack per descriptor, so two areas in one process over the same path interfere. Within a process, exclusive mode is the right answer; these locks coordinate between processes.
- **Stage 2 is refiled as T-20, not silently dropped.** The item anticipated that shared access needs cache invalidation — `Table.recordCount`, `cdx` nodes, `dbc` rows and the `fatfs` FAT are all held in memory, and another process can invalidate any of them. Stage 1 enforces the protocol; it does not yet make a shared reader see another writer's changes. Claiming otherwise would be the more dangerous outcome, so the boundary is stated.

Cross-ref: CHANGELOG 0.8.2, T-20.

## [0.8.1] T-18. Memo compaction during PACK (orphaned DBT/FPT blocks) (v0.8.1, 2026-07-23)

Theme: dbf · Priority: P3 · Status: ✓ closed by implementation

- **Trigger:** T-03 closure review, 2026-07-23. PACK compacts the table and rebuilds the indexes; the memo file is left alone, so its blocks accumulate.
- **Two distinct sources of waste**, worth separating because only one is new:
  1. **Records removed by PACK.** Their memo blocks are now unreachable — nothing points at them.
  2. **Memo rewrites.** `MemoSet` appends and repoints, orphaning the old blocks. FoxPro behaves the same way, so this is expected rather than a defect, but it is the larger source over time.
- **Scope.** A `Compact` operation on the memo file that copies live memos into a fresh file, returning a block mapping so record memo pointers can be rewritten. Both formats: DBT's 512-byte blocks and FPT's configurable ones.
- **The mapping is the same shape as T-03's**, deliberately. `RecordMapping` made record renumbering an explicit value that every consumer works from; memo compaction needs the analogous thing for block numbers, and the parallel is worth preserving rather than inventing a second convention.
- **Ordering matters and is not obvious.** Memo compaction must follow record compaction: PACK decides which records survive, and only then is it known which memos are live. Running them the other way would preserve memos belonging to records about to be dropped.
- **Liveness is determined by scanning, not by refcounting.** A memo block is live if some surviving record's memo field points at it. There is no back-pointer from a memo to its record, so the only sound method is to walk the packed table collecting pointers. That is one full table scan, which is acceptable inside an operation that already rewrites the whole file.
- **Integration.** `Area.Pack` grows an option, or a sibling `Area.PackAll`, that compacts the memo after the table and rewrites the surviving records' pointers. The default should probably stay table-only: memo compaction rewrites a second file and callers who do not rewrite memos gain nothing.
- **Effort:** roughly 2 days. Day 1 the compaction and mapping for both formats; day 2 the `Area` integration, pointer rewriting, and tests including a round trip proving memo content survives a pack that moved every block.

- **Implementation (v0.8.1).** `dbf.CompactMemo` and `dbf.RewriteMemoPointers`, returning a `BlockMapping`; `blipperdb.Area.PackAll` coordinating the whole sequence. 8 tests.
- **`BlockMapping` mirrors `RecordMapping` deliberately.** T-03 made record renumbering an explicit value that every consumer works from, which is what made PACK's coordination tractable. Memo blocks have the same problem — pointers to them live inside records that know nothing about a move — so they get the same answer rather than a second convention. Same accessors: `Lookup(old) (new, kept)`, `Kept`, `Dropped`, `Identity`.
- **Liveness is decided by scanning, as the item anticipated.** A memo carries no back-pointer to its owning record, so the only sound method is to read every surviving record and collect its pointers. One full table scan, inside an operation that already rewrites a whole file.
- **Ordering is load-bearing and is why `PackAll` is a separate method rather than an option on `Pack`.** Which memos are live is decided by which records survive, so the table must be packed first; compacting first would carefully preserve memos belonging to records about to be dropped.
- **Both sources of waste are covered, and the second is the larger.** Records removed by PACK leave their memos unreachable, but every `MemoSet` appends and repoints, orphaning the previous entry — FoxPro behaves identically, so it is expected rather than a defect, and over time it dominates. `TestCompactMemoReclaimsRewriteOrphans` drives three rounds of rewrites with no pack involved and asserts the compacted file is smaller.
- **Both formats, one code path each.** DBT and FPT differ in block geometry and entry header, so `compactDBT` and `compactFPT` are separate, but the mapping and the pointer rewrite are shared. The FPT compaction preserves the source's block size, so a caller's configured geometry survives.
- **A read error during compaction fails the operation** rather than silently dropping the entry. Losing memo content quietly is worse than failing loudly, and a pointer into a truncated memo file is exactly the case where quiet would be wrong.
- **`Pack` alone remains the default.** Compaction rewrites a second file and a caller who never rewrites memos gains nothing from it.

Cross-ref: CHANGELOG 0.8.1.

## [0.8.0] T-17. fatfs: VFAT long filename support, configurable (v0.8.0, 2026-07-23)

Theme: fatfs · Priority: P3 · Status: ✓ closed by implementation

- **Trigger:** requested 2026-07-23. T-15 shipped short-name-only on the reasoning that xBase filenames are 8.3 by construction, which remains true; this item adds LFN anyway, behind a switch, so the capability exists without being imposed.
- **Configurable, defaulting off.** `OpenImage`/`OpenImageRW` take options; `WithLongNames(true)` enables LFN parsing and generation. Default off keeps the existing behaviour and the existing performance for the common case, and means a directory scan does not pay LFN reassembly on volumes that have none.
- **Read path.** LFN entries carry attribute `0x0F`, precede their 8.3 entry, and appear in reverse sequence order. Each 32-byte slot holds 13 UCS-2 characters across three disjoint runs (offsets 1–10, 14–25, 28–31), with a sequence number whose `0x40` bit marks last-in-sequence and a one-byte checksum of the 8.3 alias. The checksum is what detects desync when another tool has rewritten the directory: a mismatched run is discarded and the 8.3 alias used instead, rather than reporting a name that belongs to a different file.
- **Write path, the expensive half.** Writing a long name requires: generating a unique 8.3 alias (`NAME~1.EXT`, `~2`, … scanning for collisions), computing its checksum, and allocating **contiguous** directory slots — a 20-character name needs two LFN entries plus the short entry, three consecutive free slots. `allocEntry` currently returns any single free slot and needs a run-finding variant. On FAT16 the root directory cannot grow, so a fragmented directory can fail to place a long name while free slots remain; that is a real error case, not a theoretical one.
- **The orphaned-entry gap T-15 left.** `fatfs` currently writes only the 8.3 entry. Creating a file where another tool had written LFN entries leaves those `0x0F` entries behind with a checksum that no longer matches, so an LFN-aware tool sees a stale long name. With LFN support this is fixable: `Create` and `Remove` must clear the preceding LFN run, not just the short entry. This applies **even with long names disabled**, since the orphan is created by the write regardless.
- **Scope.** FAT32 and FAT16 alike — LFN is a directory-entry convention, not a FAT-width one, and the same code serves both. UCS-2 is decoded to Go strings as UTF-16; characters outside the BMP are not representable in a single LFN slot pair and are rejected on write rather than silently mangled.
- **Verification.** The existing mtools oracle covers both directions: `mcopy` writes long names into an image for `fatfs` to read, and `mdir`/`mcopy` read back what `fatfs` writes. The test that matters is that a **non-LFN-aware reader still sees a valid volume** — every long-named file must have a working 8.3 alias.
- **Performance is the stated risk.** Enabling LFN makes directory enumeration reassemble names across multiple entries and makes creation scan for contiguous runs and alias collisions. The register keeps this configurable partly so the cost can be measured against the default; if it proves material, `bench/` gains a directory-scan benchmark, and the repoman journal makes the whole campaign revertible via `undo --since before-t17-lfn`.
- **Effort:** roughly 3 days. Day 1 read path with checksum validation; day 2 write path with alias generation and contiguous-slot allocation; day 3 the orphan fix, the oracle tests, and the non-LFN-reader compatibility check.

- **Implementation (v0.8.0 pending).** `fatfs/lfn.go` carries the codec; the read path collects runs during `loadRoot`, and `Create`/`Remove` handle generation and orphan clearing. 10 tests plus 4 benchmarks.
- **Performance measured rather than asserted, which is why the default stands.** Directory load with long names enabled costs about 20% more: 73µs against 87µs on an image carrying long names, 64µs against 76µs on one without. Paid once per `Open`, small in absolute terms, real in proportion. Enough to justify leaving the option off by default, not enough to argue against having it.
- **The checksum is the load-bearing part of the read path.** Every LFN entry carries a checksum of the 8.3 alias it belongs to, and validating it is the only defence against desync: a tool that rewrote the short entry leaves a run describing a name that is no longer there. `assembleLongName` rejects a mismatched run and the caller falls back to the alias, because reporting a name belonging to a different file is worse than reporting an ugly one. Guarded by `TestLFNChecksumMismatchIsRejected`, along with ordering invariants in `TestLFNRejectsMalformedRuns`.
- **Contiguity is the load-bearing part of the write path.** LFN entries must sit immediately before the short entry they describe, so `allocEntryRun` finds a consecutive run rather than any free slot. On FAT16 the root cannot grow, so a fragmented directory can fail to place a long name while free slots remain scattered — that case is reported distinctly from a full directory, since the remedy differs.
- **The orphan fix applies regardless of the option.** T-15 wrote only the 8.3 entry, so creating a file where another tool had written long-name entries left them behind pointing at a checksum that no longer matched. `Create` and `Remove` now clear the preceding run **whether or not long names are enabled**, because the orphan is produced by the write, not by the reading configuration.
- **Compatibility is checked in the direction that matters.** `TestLongNamedFileIsVisibleToShortNameReader` writes a long name, reopens with the option off, and confirms a working 8.3 alias — a reader without long-name support must still see a valid volume. The mtools oracle covers the other direction, with `mcopy` writing genuine long names into a FAT32 image that `fatfs` reads back.
- **Non-BMP runes are representable** as surrogate pairs, which `utf16.Encode` already produces. What is rejected on write is a name containing U+FFFF, since that collides with the format's own padding sentinel.
- **Reverting is one command.** The campaign is journaled from mark `before-t17-lfn`, so `ed.py undo --since before-t17-lfn` removes it wholesale if the cost proves unwelcome in practice.

Cross-ref: CHANGELOG 0.8.0.

## [0.7.0] T-03. PACK operation not implemented (v0.7.0, 2026-07-23)

Theme: dbf · Priority: P3 · Status: ✓ closed by implementation

- **Trigger:** design scope (design01 §9), 2026-07-23.
- **Scope:** Deletion is logical only. Physical removal of deleted records (PACK), and rebuilding any NTX indexes after a PACK, are future work.

- **Implementation (v0.7.0 pending).** `dbf.Table.Pack` returning a `RecordMapping`, `blipperdb.Area.Pack` coordinating the rebuild, and a `Compactable` interface extracted from two real implementations. 10 tests.
- **The mapping was the hard part, and it is now explicit.** PACK's difficulty is not removing rows; it is that record numbers are a shared namespace across four file formats with no coordination between them. An index holds record numbers, a memo pointer lives inside a record, and nothing propagates a renumbering. `RecordMapping` is that missing coordination, made a value so every consumer works from the same account of what moved: `Lookup(old) (new, survives)`, plus `Kept`, `Removed`, `OldCount`, and `Identity`.
- **`Compactable` describes a property, not a duty.** It was written while implementing the rebuild for NTX and CDX, then extracted once two implementations existed, rather than declared up front and discovered wrong on the second. `AttachedCatalogue` deliberately does not implement it — long field names have nothing to do with record numbers — and a test asserts that absence so a future no-op cannot creep in. This follows the `Flusher` precedent, where `OSDir` correctly does not implement a capability it lacks.
- **The two rebuild strategies differ because the formats do.**
  - **NTX** holds a key function, so entries can be recomputed. The rebuild scans for entries whose records were removed, drops them, then deletes and reinserts the survivors that moved. Entries whose number did not change are left alone.
  - **CDX** carries its key expression as *text* and blipper has no expression evaluator, so entries are remapped rather than recomputed: each surviving entry keeps its key and takes its record's new number. This is exact, because packing changes which records exist and how they are numbered, never their field values.
- **`Identity()` makes defensive packing cheap.** A pack that removes nothing skips both the file rewrite and every attachment rebuild, and a test asserts the CDX bytes are untouched — otherwise the fast path would be a claim rather than a guarantee.
- **Ordering is load-bearing and documented.** The table is packed first, then attachments rebuild by reading the packed table. If a rebuild fails the error is returned with the table already packed, leaving that attachment stale; this is reported rather than hidden, because silently continuing would leave an index that looks valid and is not.
- **A read-only CDX cannot be rebuilt**, and says so. `AttachCDX` takes an `io.ReadSeeker`; the attachment now also records a writable handle when one was supplied, so a pack against a read-only CDX fails with a clear message instead of obscurely partway through.
- **Records only move toward lower offsets**, so the compaction pass is a forward copy that never overwrites a record it has yet to read. Truncation is applied when the underlying stream supports it; a stream without `Truncate` keeps its length with the EOF marker at the real end, which every xBase reader honours.

Cross-ref: CHANGELOG 0.7.0.

## [0.6.5] T-16. sqlitefs: SQLite-backed tablespace with chunked blobs (v0.6.5, 2026-07-23)

Theme: sqlitefs · Priority: P3 · Status: ✓ closed by implementation

- **Trigger:** tablespace discussion, 2026-07-23, after T-15 shipped FAT images. FAT gives a self-contained container; SQLite gives one with transactions, which is the property blipper's multi-file consistency problem actually needs.
- **Scope.** A `sqlitefs` package storing files as chunked blobs in a SQLite database, exposed through `blipperfs.FileSet`. A whole xBase dataset — DBF, CDX, FPT, DBC — lives in one `.db` file, and a `Flush` commits every file atomically.
- **Why this and not ZIP or a bespoke container.** SQLite is the only candidate offering atomic multi-file commit. Blipper writes a DBF record, its CDX entry, and its FPT block as three separate stream writes with no commit boundary; on `OSDir` and on FAT a crash between them leaves an inconsistent set. SQLite makes that one transaction. `archive/zip` cannot update in place at all, so it suits export snapshots rather than a live tablespace.
- **Chunked, not whole-blob.** Files are split across rows, so a seek maps to a chunk lookup, a write touches one row, and growth appends a row. This sidesteps the fixed-size constraint on SQLite blob handles entirely — `sqlite3_blob_write` cannot extend a blob, but with chunking nothing ever needs to. It also removes any dependency on the incremental-blob API, so the driver needs only ordinary `SELECT`/`INSERT`/`UPDATE`.
- **Two tables, normalised on a file id.**

  ```sql
  CREATE TABLE files (
      id   INTEGER PRIMARY KEY,
      name TEXT NOT NULL UNIQUE COLLATE NOCASE,
      size INTEGER NOT NULL
  );

  CREATE TABLE chunks (
      file_id INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
      idx     INTEGER NOT NULL,
      data    BLOB NOT NULL,
      PRIMARY KEY (file_id, idx)
  ) WITHOUT ROWID;
  ```

  The obvious alternative is one table keyed on `(name, idx)`, repeating the filename in every chunk row. That was the shape the chunk-size benchmark used. Normalising is better, but the reason is not the one it first appears to be:
  - **Storage saving is negligible.** `"CUSTOMERS.DBF"` is 13 bytes against a 32 KB chunk — about 0.04%. Of the 1.5% total overhead measured at 32 KB, almost all is B-tree cell headers and overflow-page pointers, not the repeated name. Normalising saves roughly 9 bytes per row.
  - **Key comparison is the real gain, and it is modest.** Every read resolves `WHERE file_id=? AND idx=?`. An integer comparison at each B-tree level beats a `COLLATE NOCASE` string comparison, on a path taken for every chunk access.
  - **A place for file-level state is the strongest argument, and it is a correctness one.** Without a `files` row there is nowhere to record a file's logical size, so `Stat` would have to compute `(chunk_count-1)*chunkSize + len(last_chunk)` — reading the last chunk merely to learn a length. The `size` column makes it one indexed read.
  - **`ON DELETE CASCADE` makes `Remove` atomic.** Otherwise deletion is two statements that must both succeed or orphaned chunks leak.
  - **Room for per-file metadata later** — mtime, a format tag, a flag — without touching the chunk table.

  Cost is one extra lookup per `Open` to resolve name to id, cached in the file handle and paid once rather than per read.
- **`PRIMARY KEY (file_id, idx)` with `WITHOUT ROWID` is load-bearing and must survive the normalisation.** It makes the primary key the physical storage order, so every chunk of a file is stored adjacently in index order: a sequential scan walks the B-tree in storage order, and a random seek is a single descent to an exact key. A conventional rowid table with a secondary index on `(file_id, idx)` would cost two lookups per read and scatter chunks by insertion order. This property was doing real work in the benchmark.
- **`COLLATE NOCASE` on `files.name`** gives DOS-era case-insensitive lookup for free, which `osFileSet` currently does by hand.
- **Chunk size: 32 KB default, configurable.** Measured 2026-07-23 on this container (SSD-backed ext4, WAL mode, `synchronous=NORMAL`, 20 MB payload, warm cache, 2000 iterations on the descent workload), against four blipper-shaped workloads — a 32-byte header rewrite, an 80-byte record append, a 4-node scattered CDX descent, and a full sequential scan:

  | chunk | writeAll | hdrWrite | recAppend | descent | scan | db size | overhead |
  | ----- | -------- | -------- | --------- | ------- | ---- | ------- | -------- |
  | 8K    | 468ms    | 697µs    | 744µs     | 1.3ms   | 42.7ms | 21.3 MB | 6.5% |
  | 16K   | 402ms    | 717µs    | 994µs     | 1.7ms   | 38.6ms | 20.6 MB | 3.0% |
  | **32K** | **230ms** | **539µs** | **543µs** | **531µs** | **33.6ms** | 20.3 MB | 1.5% |
  | 48K   | 236ms    | 629µs    | 672µs     | 783µs   | 39.4ms | 20.2 MB | 1.0% |
  | 64K   | 309ms    | 1.3ms    | 1.7ms     | 3.3ms   | 38.4ms | 20.2 MB | 1.0% |
  | 128K  | 255ms    | 2.5ms    | 2.7ms     | 5.3ms   | 34.5ms | 20.1 MB | 0.5% |
  | 256K  | 271ms    | 5.6ms    | 6.2ms     | 8.4ms   | 55.3ms | 20.0 MB | 0.0% |
  | 512K  | 273ms    | 10.3ms   | 10.8ms    | 14.7ms  | 48.2ms | 20.0 MB | 0.0% |

- **The measurement contradicted the reasoning, which is why it was worth taking.** The a-priori argument was for 8 KB, on the grounds that a scattered CDX descent at 32 KB pulls 64× more bytes than the 512-byte nodes it wants. That amplification is real and 32 KB is still 2.5× faster, because the dominant cost at small chunk sizes is per-row overhead — a 20 MB file is 2560 rows at 8 KB and 640 at 32 KB, and each operation pays B-tree descent plus statement overhead per row touched. 32 KB is the crossover where per-row cost stops dominating and per-byte cost has not yet taken over. Storage overhead moves the same way (6.5% at 8 KB, 1.5% at 32 KB), penalising small chunks further.
- **Caveats recorded with the number.** One machine, one 20 MB payload, warm cache, and a *single* file. A dataset large enough to miss SQLite's page cache would shift the optimum smaller, since amplification would cost real I/O rather than memcpy. `modernc.org/sqlite` is a source translation of C; a cgo build may have different per-statement overhead and therefore a different crossover. Hence configurable, with 32 KB as a measured default rather than a fixed constant.
- **The benchmark measured the one-table `(name, idx)` shape, not the normalised two-table one above.** Chunk size and table layout are independent enough that the 32 KB result should carry, but it has not been shown to. Re-run `bench/chunksize` against the real schema when T-16 is built, and with several interleaved files rather than one — `CreateTable` writes four files in sequence, and that could fragment the chunk table in a way a single-file benchmark cannot show.
- **API shape.**
  - `sqlitefs.Open(path string, opts ...Option) (*FS, error)` and `sqlitefs.OpenDB(*sql.DB, opts ...Option)` for a caller-owned handle.
  - `WithChunkSize(n int) Option`, defaulting to 32 KB.
  - `FS` implements `blipperfs.FileSet` plus `blipperfs.Flusher`, where `Flush` is `COMMIT`.
  - As with `fatfs`, the package must not import blipper: the `blipperfs` adapter lives in `blipperfs`. `sqlitefs` is a chunked file store over SQLite that happens to have blipper as its first consumer.
- **Transactions are the point, and need deciding deliberately.** Between `Flush` calls the FS holds an open transaction, so a crash rolls back to the last commit rather than leaving a half-written CDX. This is a stronger guarantee than `Flusher` currently advertises — today it means "some sets buffer writes", and this makes it "some sets are transactional". Worth stating in `blipperfs`'s documentation rather than acquiring by accident.
- **First non-stdlib dependency in the repository.** `modernc.org/sqlite` is pure Go (no cgo) and pulls `modernc.org/libc`, `mathutil`, `memory`, and `golang.org/x/sys`. Verified installable in-container 2026-07-23. This is a real change to the project's dependency posture and is called out here rather than discovered at review.
- **Oracle.** The `sqlite3` CLI: create a tablespace with blipper, verify with `SELECT f.name, f.size, count(c.idx), sum(length(c.data)) FROM files f JOIN chunks c ON c.file_id = f.id GROUP BY f.id`, and confirm both that the reassembled bytes match what blipper wrote and that `f.size` agrees with the chunk lengths. That second check is worth having: a `size` column that can drift from the chunks it describes is a new failure mode the denormalised shape did not have.
- **Effort:** roughly 3 days. Day 1 schema and `FileSet`; day 2 chunked read/write with the seek arithmetic; day 3 transactions, the `blipperfs` adapter, and the CLI oracle harness. The second table adds little work but one invariant worth a dedicated test: `files.size` must always agree with the chunks it describes, including after a truncating `Create` and after a write that extends the last chunk.

- **Implementation (v0.6.5).** Package `sqlitefs` plus the `blipperfs.SQLiteTablespace` adapter. 12 tests across both packages.
- **Reusability verified structurally.** `grep -rn "ha1tch/blipper" sqlitefs/*.go` returns nothing outside tests, matching the `fatfs` precedent. The adapter lives in `blipperfs`.
- **Chunk-size default re-measured against the shipped schema**, as this item required. The first benchmark used the denormalised single table and one file; it was rewritten for the normalised pair and for four interleaved files, mirroring what `CreateTable` writes. 32 KB survives: fastest on record append and index descent, with 32K/48K within noise across the rest.
- **The multi-file run exposed two effects the single-file one could not**, both strengthening the choice rather than complicating it. 8 KB collapses on sequential scan — 413 ms against 34 ms at 32 KB, a 12× penalty absent from the first run, because four interleaved files in one chunk table mean many more rows and the scan pays B-tree traversal per row. And 512 KB collapses on bulk write, 1.20 s against a ~380 ms plateau, where the first run showed no cliff.
- **The `files.size` drift risk is guarded directly.** `TestSizeAgreesWithChunks` opens a second connection and cross-checks `files.size` against `COUNT` and `SUM(LENGTH(data))` over the chunks after each operation that can change a length: initial write, truncating `Create`, a write extending the last chunk, and an in-place overwrite that must *not* change size.
- **`PRAGMA foreign_keys=ON` is load-bearing and easy to miss.** SQLite disables foreign keys by default, so without it `ON DELETE CASCADE` silently does nothing and `Remove` leaks every chunk. `TestRemoveCascades` counts rows in `chunks` after a delete rather than trusting the declaration.
- **The transaction is the point of the package.** An open transaction accumulates writes between `Flush` calls, so a set of files written together commits together. `TestFlushIsTheCommitBoundary` proves it from outside: a second connection sees zero files before `Flush` and all three after.
- **Dependency posture changed as forecast.** `modernc.org/sqlite` is now a direct dependency, pulling nine indirect modules. This is the repository's first non-stdlib dependency and was flagged when the item was filed rather than discovered at review.

Cross-ref: CHANGELOG 0.6.5.

## [0.6.0] T-14. blipperfs: path-aware session layer (OpenDir, Use, CreateTable) (v0.6.0, 2026-07-23)

Theme: blipperfs · Priority: P2 · Status: ✓ closed by implementation

- **Trigger:** API-shape review, 2026-07-23, after T-10 closed. With CDX (T-09), FPT (T-12), memo integration (T-13), and DBC (T-10) all landed, the caller-facing cost of the stream-only design became visible: opening a fully-featured table takes five calls, two filename derivations, one raw-bit test, and a format switch — all of it re-deriving information blipper already holds.
- **The defect is not "too many constructors".** It is that blipper made a deliberate design choice (streams, not paths) and never built the layer that choice requires. Three specific leaks:
  1. **Format dispatch leaks.** `switch area.Table().MemoFormat()` in caller code puts blipper's taxonomy on display. T-13's whole point was that `Table.MemoFormat()` is the dispatch surface — but it currently dispatches in the caller's file rather than ours.
  2. **Sibling naming leaks.** `TABLE.DBF → TABLE.DBT/.FPT/.CDX/.NTX` is xBase convention, not caller policy. Deferred as out-of-scope in T-13 and again in T-10. Correct per-item; wrong cumulatively — three deferrals of the same thing is a missing component, not a scope boundary.
  3. **Raw bits leak.** `TableFlags()&0x04` requires the caller to know T-10's truth table, which is an internal invariant.
- **Precedent: FoxPro's `USE`.** `USE Customers` opened the DBF, followed the version byte to the right memo format, followed the backlink to the DBC, auto-opened the structural CDX, and left free-index attachment explicit. **Automatic where the file itself declares the answer; explicit where the user must choose.** blipper can adopt that line exactly, because it already reads every declaring byte.
- **Design.**
  - **`FileSet` interface** — `Open(name)`, `Create(name)`, `Exists(name)`. Implementations: `OSDir(path)` for real disk, `MemFileSet` for tests. Keeps `dbf`/`dbc`/`cdx`/`ntx`/`blipperdb` stream-based and in-memory-testable; only this layer knows filesystems.
  - **`OpenDir(path) (*blipperdb.BlipperDB, error)`** — the headline constructor. Scans for `*.DBF`, derives each alias from the file stem, opens each with full sibling resolution, registers them all. A data directory *was* the database in these applications; this matches that.
  - **`Use(db, fs, alias, name) (*blipperdb.Area, error)`** — explicit single-table open with the same resolution. `OpenDir` is sugar over this, not a replacement: needed for one-table opens, files scattered across directories, and `MemFileSet` tests.
  - **`CreateTable(db, fs, alias, stem, spec) (*blipperdb.Area, error)`** — symmetric creation. `TableSpec` carries `Schema`, `MemoFormat`, `LongNames map[string]string`, `Tags []cdx.TagSpec`. One call writes the DBF with correct version byte and table flags, the memo file, the DBC with backlink and long names registered, and the CDX. Today that is five calls in a specific order with two invariants the caller must not get wrong.
- **Automatic vs explicit split — a design commitment, not a convenience call.** Memo, DBC, and structural CDX resolve automatically because the file declares them. Free NTX indexes stay explicit because nothing in the DBF names them; auto-globbing `*.NTX` would attach indexes the user never asked for. `Use` must never surprise.
- **Missing-sibling policy.** A missing *declared* sibling is an error: if `MemoFormat()` says FPT and `STEM.FPT` is absent, that is corruption — T-10's truth table already says exactly this for the DBC case. A missing *undeclared* sibling is not an error: no `.CDX` just means no structural index.
- **Naming.** Package is `blipperfs`, not an abbreviation. It appears once or twice per caller file (a constructor package), so readability wins over brevity; callers who want terse can alias at the import site (`import bpfs ".../blipperfs"`) without the library baking that choice in. Same reasoning that keeps `blipperdb` from being `bpdb`. `OpenDir` rather than `NewFS` because it does I/O and can fail — `NewFS` would suggest a cheap handle construction, which is `OSDir`'s job.
- **No breaking changes.** `blipperfs` sits above the existing API; everything already written keeps working.
- **Effort:** roughly 1 week. `FileSet` + `OSDir` + `MemFileSet`, then `Use`, then `OpenDir`, then `CreateTable`, then tests including a round-trip that creates a full four-file set and reopens it with a single call.

## fatfs

- **Implementation (v0.6.0), in three phases:**
  - **Phase 1**: `FileSet` interface with `OSDir` and `MemFileSet`; package-level `Use` and `CreateTable`; `TableSpec`. 7 tests.
  - **Phase 2**: `Session` binding a `BlipperDB` to its `FileSet`, removing the paired `(db, fs)` argument from every call site. `OpenDir` became shorthand for `OpenFileSet(OSDir(path))`. 2 tests.
  - **Phase 3**: handle lifetime — `Session.Close()`, plus stream retention on the newer attachments so `Area.close()` can release them. 2 tests.
- **A design bug the round-trip test caught.** `CreateTable` registers the catalogue row under `spec.TableLongName`, which deliberately need not match the file stem, but `Use` was looking that row up *by* stem. The mapping from a `.DBF` to its row in a `.DBC` is not recoverable from the filename at all. `resolveTableLongName` now tries a case-insensitive stem match, falls back to the sole table row when the catalogue holds exactly one, and reports ambiguity rather than guessing when several exist with no match. Unit tests would not have found this — only creating and then reopening did.
- **A leak the close work exposed.** `Area.close()` released the DBF stream and NTX index streams, but the attachments added later (memo, CDX, catalogue) kept no reference to their streams at all, so there was nothing to close. A session over a directory of memo-bearing tables leaked a descriptor per table. All three now retain `src` and `Area.close()` releases them.
- **Ergonomics evidence rather than assertion.** Converting the phase-1 tests to `Session`'s short form removed every direct `blipperdb` import from the test file — the compiler flagged it unused. A refactor that makes a dependency disappear from callers is one where the abstraction was probably right.
- **What is deliberately not here.** Automatic free-NTX attachment: nothing in the DBF names those indexes, and globbing `*.NTX` would attach indexes the caller never asked for. `Use` must never surprise.

Cross-ref: CHANGELOG 0.6.0.

## [0.6.0] T-15. fatfs: reusable FAT16/FAT32 driver, read + cached write (v0.6.0, 2026-07-23)

Theme: fatfs · Priority: P3 · Status: ✓ closed by implementation

- **Trigger:** API-shape discussion, 2026-07-23. With `blipperfs.FileSet` in place, mounting a disk image as a table source is a self-contained driver problem rather than a change to blipper's core.
- **Scope.** A standalone `fatfs` package implementing FAT16 and FAT32, read and write, with a write-back cache. Exposed to blipper through a thin `blipperfs.FileSet` adapter, but **the package itself must not import blipper** — it is a general-purpose FAT driver that happens to have blipper as its first consumer.
- **Reusability is a hard requirement, not an aspiration.** `fatfs` depends only on the standard library and operates on an `io.ReadWriteSeeker` holding the image. No blipper types in its API. The `blipperfs` adapter lives in `blipperfs`, not in `fatfs`, so the dependency arrow points one way only.
- **Format coverage.**
  - **FAT16 and FAT32.** FAT12 deliberately excluded: its 12-bit packed entries straddle byte boundaries, and no realistic xBase dataset arrives on a volume small enough to need it.
  - **Short names (8.3) only.** VFAT long names postponed — DOS-era xBase data is 8.3 by construction, and every FAT32 volume carries an 8.3 alias for every file, so a short-name-only reader is correct on FAT32 images. LFN can be filed separately if a need appears.
  - FAT32's root directory is an ordinary cluster chain rather than a fixed region, which removes a special case rather than adding one.
- **Write-back cache.** The FAT and directory regions are cached in memory and written on flush, not on every allocation. This is what makes write support cheap: allocating a hundred clusters touches memory a hundred times and disk once. Cache invalidation is trivial here because `fatfs` owns the image exclusively while open.
- **Commit semantics.** `fatfs` exposes `Flush()`/`Sync()` on the volume, and the `blipperfs` adapter surfaces it. This matches Clipper's own `COMMIT`, which flushes buffers for a work area; the tablespace-level equivalent flushes the whole image. Precedent is exact, naming is settled.
- **Oracle.** `mkfs.vfat` (dosfstools) creates images; `mtools` (`mcopy`, `mdir`, `minfo`) reads and writes them without mounting. Both installed and verified available in-container 2026-07-23. Verification runs both directions: images built by `mkfs.vfat` + `mcopy` must read byte-identically through `fatfs`, and images written by `fatfs` must be readable by `mdir`/`mcopy` with matching content. Same shape as the DOSBox oracle for Clipper formats, with less setup.
- **Correctness risk and how it is contained.** A wrong FAT entry does not fail the write; it corrupts a chain, and the damage surfaces on a later read. Two mitigations, both required:
  1. **A property-test harness**: write N files of random sizes, then verify no two chains share a cluster, every chain terminates, every file reads back byte-identical, and the free-cluster count agrees with the FAT.
  2. **`OpenImage` is read-only; `OpenImageRW` is the explicit write constructor.** Someone pointing this at a 30-year-old image must not be able to modify it by accident.
- **Deliberately out of scope.** FAT12; VFAT long names; `FSInfo` maintenance beyond treating it as the non-authoritative hint it is; volume creation (`mkfs`) — an image is supplied, not made.
- **Effort:** roughly 5 days. Boot-sector and BPB parsing, FAT16/32 chain walking, directory enumeration, read path, cached write path with allocation, the `blipperfs` adapter, and the mtools oracle harness.
- **Order:** after T-14, which supplies the `FileSet` seam it plugs into.

- **Implementation (v0.6.0).** Package `fatfs` (geometry, volume, directory, file) plus the `blipperfs` adapter (`FATImage`, `FATImageRW`, `Flusher`). 10 tests across both packages.
- **Reusability verified structurally, not asserted.** `grep -rn "ha1tch/blipper" fatfs/*.go` returns nothing outside tests. The adapter lives in `blipperfs`, so the dependency arrow points one way and the package stands alone for any consumer wanting a FAT driver.
- **The caching insight changed the cost.** Write support looked like the expensive, risky half until it was pointed out that a write-back cache changes the picture entirely: the FAT and root directory live in memory and reach the image on `Flush`, so allocating a forty-cluster chain touches memory forty times and the image once. That is what made read *and* write affordable in one item rather than two.
- **A bug the write tests caught.** `loadRoot` stopped decoding at the first `0x00` end-of-directory marker. That is correct for enumeration — nothing live follows it — and wrong for allocation, because the slots after it are precisely the free space a new file needs. `allocEntry` had nothing to hand out on a mostly-empty volume. Every slot is now decoded; `isFile()` already filtered the free ones out of `List` and `findEntry`.
- **`Flusher` is the commit seam.** `OSDir` does not implement it, since the operating system's filesystem already provides that guarantee; container-backed sets do. This is the tablespace-level counterpart to Clipper's own `COMMIT`, which flushes one work area's buffers.
- **The integration test is the point of the item.** A complete xBase dataset — DBF, FPT, DBC, CDX — created inside a FAT16 image through the ordinary `blipperfs` API, committed, reopened from the mutated bytes, every sibling resolved automatically and memo content intact.
- **Oracle.** `mkfs.vfat` built the images, `mcopy` placed the files, `mdir` verified. Fixtures committed gzipped (38 KB and 63 KB) with provenance in `fatfs/testdata/README.md`. Image sizes are set by the specification's cluster-count rule, not preference.

Cross-ref: CHANGELOG 0.6.0.

## [0.6.0] T-10. DBC-compatible sidecar for long field names (v0.6.0, 2026-07-23)

Theme: dbf · Priority: P3 · Status: ✓ closed by implementation

- **Trigger:** long-field-names investigation, 2026-07-23. Design refined 2026-07-23 after discussion of provenance bits and DBC file identity.
- **Scope:** dBASE III+ caps field names at 10 characters (§9.2). VFP 3.0 introduced the database container (DBC) side-file, itself a DBF, whose rows hold long names (up to 128 chars) plus per-object metadata. A blipper table with an associated `.DBC` gains long field names; the file is not a private blipper format but a **real Visual FoxPro 3.0 DBC**, a subset implementation.
- **Extension and layout.** `TABLE.DBF` → `TABLE.DBC`, colocated. Same 8-column DBF schema as VFP's DBC: `OBJECTID I(4)`, `PARENTID I(4)`, `OBJECTTYPE C(10)`, `OBJECTNAME C(128)`, `PROPERTY M`, `CODE M`, `RIINFO C(6)`, `USER M`. Self-referencing tree via `PARENTID` (Field row → Table row → Database row). Long names live on Field rows in `OBJECTNAME`. `PROPERTY`/`CODE`/`USER` stay empty — not because they are private, but because we have not implemented the features that populate them (validation rules, stored procedures, user metadata). A VFP reader can open the table and its DBC and get valid data; what it will not get is nulls (no `_NullFlags`), Currency/DateTime/Integer/Double (unimplemented), or FPT memos (we write DBT).
- **Table header signalling.** Byte 28 of the DBF header carries **two bits set**:
  - **Bit 2** (`0x04`) — VFP's "table is owned by a DBC". Setting it makes a VFP reader look for a DBC via the 263-byte backlink between the field terminator and the first record.
  - **Bit 3** (`0x08`) — blipper-reserved provenance flag. "Written by a blipper-aware writer, DBC guaranteed."
  - Combined value `0x0C`.
- **Bit availability verified.** Byte 28 across 120 real Clipper/dBASE III+ tables (corpus scan 2026-07-23) is `0x00` in every file. Reserved bytes 12–13, 14, 15, 16–27, 30–31 are also uniformly `0x00` in the same scan. (Field-descriptor reserved regions, §9.2, are a separate matter and do carry stale bytes; those are inside the descriptor array, not the file header.) The `0x08` bit position is documented unused in every published dBASE, FoxPro, VFP, and Clipper spec.
- **Truth table for the two bits and the sidecar file:**

  | byte 28 | .DBC present | Meaning |
  | ------- | ------------ | ------- |
  | `0x00`  | no  | plain III+ table, no sidecar |
  | `0x00`  | yes | III+ table with a stray/legacy DBC — ignore |
  | `0x04`  | yes | VFP-written table, DBC authoritative |
  | `0x04`  | no  | broken VFP table (backlink points at missing DBC) — error |
  | `0x0C`  | yes | blipper-written, DBC guaranteed, both blipper and VFP can read |
  | `0x0C`  | no  | blipper-written but DBC missing — corruption, error |
  | `0x08`  | either | should never occur — blipper writes bits 2 and 3 together or neither |

  The last row is a useful invariant: bit 3 without bit 2 is malformed and detecting it means someone hand-edited the header.
- **Backlink.** VFP's 263-byte backlink between the field terminator and the first record must be written for VFP interop. `Open` already tolerates padded headers via `TestOpenHonoursPaddedHeader`, so writing one is a header-size adjustment, not a structural change. Roughly half a day extra work.
- **API surface in `blipperdb`.** A `Catalogue` type opens alongside a `Table`, resolving short↔long names. `Open` performs a `stat` on the sibling `.DBC` regardless of bit state; the bit determines only whether *absence* is an error.
- **Scope boundaries at the time of writing.** T-10 does not claim version byte `0x30`, does not implement `_NullFlags`, and does not write the undocumented `PROPERTY` binary blob — those remain unimplemented for now. **FPT is not permanently excluded:** it is tracked as T-12 (a direct implementation, since T-11's oracle question was closed by finding — see `docs/RESOLVED.md` and CHANGELOG). When T-12 ships, T-10 memo fields switch from `.DBT` to `.FPT` accordingly. Whichever memo format is current when T-10 lands is what its tables carry. The rest of the subset above (nulls, PROPERTY, `0x30`) is subject to the same principle: filed later if warranted, not permanently excluded.
- **Effort:** roughly 1 week.
- **Order:** T-09 (CDX, closed in v0.3.0) gave hands-on experience with a VFP-lineage format (compact indexes, tag directories, compression); T-10 builds on that foundation for writing VFP-shaped table files. Structurally independent, but sequencing was deliberate.

- **Implementation (v0.6.0 pending), landed in three phases:**
  - **Phase 1**: `dbc` package — schema, `Create`, `Open`, tree navigation, long-name resolution. 5 tests.
  - **Phase 2**: `dbf` byte 28 signalling and 263-byte VFP backlink. `CreateWithBacklink`, truth-table invariant on `Open`, `Table.TableFlags()`/`Backlink()` accessors. 4 tests.
  - **Phase 3**: `blipperdb.AttachedCatalogue` with `Area.AttachCatalogue`/`CreateCatalogue`/`Catalogue`/`CatalogueLongName`. Symmetry with `AttachedMemo` and `AttachedCDX`. 5 tests including the deferred `ErrNotDBC` case from phase 1.
- **Design details from the original filing preserved as-is:**
  - Extension `.DBC`, colocated with `.DBF`.
  - Byte 28 = `0x0C` (bit 2 VFP DBC-owned + bit 3 blipper provenance). Truth-table invariant `0x08` alone is enforced on `Open`.
  - 263-byte VFP backlink between the field terminator and the fileTerminator/first-record position. `Open` reads and parses it; header rewrites preserve it.
  - PROPERTY/CODE/USER memo columns stay empty — no `.DCT`/`.FPT` needed for the DBC itself.
  - Version byte stays `0x03` on the DBC (not VFP's `0x30`). Documented scope boundary from the original filing.
- **Symmetry principle preserved.** The Area now has three attachment types — `AttachedCDX`, `AttachedMemo`, `AttachedCatalogue` — with the same shape: `Attach*` / `Create*` / accessor / lookup sugar. No format asymmetry baked in.
- **14 tests total, all passing.**
- **What is deliberately not in T-10's close:**
  - Version byte `0x30` — not needed for our subset, not written.
  - `_NullFlags` — VFP nulls remain unimplemented.
  - The undocumented `PROPERTY` binary blob — not written.
  - Cross-VFP interop verified only by structural inspection (no VFP oracle available); a lenient VFP reader should open blipper-written DBFs and DBCs, but round-trip through actual VFP has not been tested this session.

Cross-ref: CHANGELOG 0.6.0.

## [0.5.0] T-13. blipperdb integration for memo-bearing tables (DBT and FPT together) (v0.5.0, 2026-07-23)

Theme: blipperdb · Priority: P3 · Status: ✓ closed by implementation

- **Trigger:** T-12 in-flight, 2026-07-23. Filed explicitly so the work does not vanish when T-12's out-of-scope note goes with it.
- **Scope.** Extend `blipperdb.Area` to attach and manage a sibling memo file — DBT or FPT — alongside the DBF. The Area currently has no memo integration for either format: callers who want memo content open the sibling file directly via `dbf.OpenMemo` or `dbf.OpenFPT`, hold two references (Area + memo file), and coordinate lifetimes themselves. That works, but it externalises what blipperdb otherwise encapsulates.
- **Both formats together, deliberately.** Landing FPT-only integration ahead of DBT would bake in an asymmetry: two years from now every Area method would carry the shape "if FPT do X else fall through". Doing them together means the dispatch site is `Table.MemoFormat()` and both paths are equal citizens. This is a lesson from the version-byte plumbing in T-12: preserving the on-disk byte and dispatching on it is cheaper than special-casing one format after the fact.
- **Expected API.**
  - `Area.AttachMemo(rw io.ReadWriteSeeker) error` — attaches the sibling memo file. Dispatches on `Table.MemoFormat()` to `OpenMemo` or `OpenFPT`. Errors if the table's format is `MemoFormatNone`, or if a memo is already attached.
  - `Area.CreateMemo(rw io.ReadWriteSeeker, blockSize uint16) error` — creates a fresh sibling. `blockSize` is honoured only for FPT (DBT is fixed at 512 bytes); pass 0 to accept the default for either format.
  - `Area.Memo() *AttachedMemo` — accessor for the attached memo, or nil.
  - `Area.MemoGet(field string) ([]byte, error)` — reads the memo referenced by the named field in the current record, returning empty content when the memo pointer is absent. This is the common-case sugar that removes the pointer-parsing boilerplate from callers.
  - `Area.MemoSet(field string, content []byte) error` — appends a new memo to the sibling, writes its block pointer into the named field of the current record, and rewrites the record. Old blocks are not reclaimed (FoxPro semantics; consistent with §T-03's PACK-scoped compaction).
  - `AttachedMemo` is a small wrapper holding the sibling file (DBT or FPT) and the format enum. Its own accessors expose the raw `MemoFile` / `FPTFile` for callers that need lower-level access.
- **What this does not add.** Automatic sibling-file discovery by naming convention (e.g. `TABLE.DBF` → `TABLE.DBT`) is out of scope — blipperdb operates on streams, not paths, and that convention belongs one layer up. Callers who want file-system-level convenience wire that themselves.
- **Testing.** Round-trip tests for both formats: attach + set + get + reopen, plus a mixed test that opens a Clipper-generated DBT-bearing table (verified corpus files) and reads memos through the Area API rather than directly through `MemoFile`. Once T-12's oracle test lands, an FPT-side counterpart follows the same shape.
- **Effort estimate.** ~3–5 days. Small surface area, but the reader/writer contracts are already in place (T-01, T-02, T-12), so this is mostly plumbing plus tests.
- **Order.** After T-12 closes. There is no reason to build T-13 on top of an FPT reader whose oracle has not yet run; the correctness ceiling for T-13 is whatever T-12's ceiling is.

- **Implementation (v0.5.0).** Ships the API sketched in the original detail, minus the naming-convention discovery which stays deliberately out of scope:
  - `Area.AttachMemo(rw)` — dispatches on `Table.MemoFormat()` to `dbf.OpenMemo` (DBT) or `dbf.OpenFPT` (FPT). Refuses `MemoFormatNone`, refuses double-attach.
  - `Area.CreateMemo(rw, blockSize)` — companion for fresh files. `blockSize` is honoured only for FPT; DBT is fixed at 512.
  - `Area.Memo()` — accessor, nil if none.
  - `Area.MemoGet(field)` — reads the memo referenced by the named field in the current record; empty content when the pointer is absent.
  - `Area.MemoSet(field, content)` — appends memo content, updates the field's block pointer, rewrites the record via `Table.Put`. Old blocks orphaned (FoxPro semantics, T-03 territory).
  - `AttachedMemo` wrapper exposes `Format()`, `DBT()`, `FPT()` for lower-level access — the escape hatch for callers who need FPT `MemoType` awareness (Picture, Object) or DBT's dBASE-specific behaviour.
- **FPT type is discarded on read and defaulted to `MemoText` on write** at the Area level. Callers who need type awareness use `AttachedMemo.FPT().Get(block)` / `.Append(content, type)` directly. This keeps the Area API content-focused; the type-aware path is one accessor away.
- **Symmetry between DBT and FPT preserved.** Every Area-level method dispatches on `Table.MemoFormat`; no method exists only for one format. This is the design principle from T-13's original filing — landing FPT-only would have baked in an asymmetry.
- **Five tests, all passing:**
  - `TestMemoAttachRefusedOnPlainTable` guards the `MemoFormatNone` case.
  - `TestMemoDBTRoundTrip` writes a memo, reads it back, and confirms an absent-pointer record returns empty.
  - `TestMemoFPTRoundTrip` flips a fresh table's version byte to `0xF5` (exercising T-12's version-byte round-trip), attaches a fresh FPT, and round-trips a binary payload containing `0x1A` — the byte a broken DBT/FPT dispatch would treat as a terminator.
  - `TestMemoAttachTwiceRefused` guards double-attach.
  - `TestMemoGetSetWithoutAttach` errors cleanly with no memo attached.
- **What is deliberately not in this landing:**
  - Automatic sibling-file discovery by naming convention (`TABLE.DBF` → `TABLE.DBT`/`.FPT`). Callers wire that themselves.
  - Attached-CDX/NTX index maintenance on `MemoSet`. The record rewrite goes through `Table.Put` which does not update attached indexes; consistency is the caller's responsibility, matching how `Area.Replace` already behaves.
  - Free-block reclamation on rewrite. Old memo blocks stay orphaned (FoxPro's own behaviour). Real reclamation is T-03 territory.

Cross-ref: CHANGELOG 0.5.0.

## [0.4.5] T-08. DBF header year decoded as 1900+y; Clipper writes year mod 100 (v0.4.5, 2026-07-23)

Theme: dbf · Priority: P2 · Status: ✓ closed by implementation

- **Trigger:** Clipper oracle investigation of SET EPOCH, 2026-07-23.
- **Scope:** `decodeHeaderDate` reads the 32-byte header's date as `1900 + year`, and `encodeHeaderDate` writes `year - 1900`. Clipper 5.2e writes **year mod 100**: a file generated 2026-07-23 under guard G-01 carries byte `26`, which this library reads as 1926 and would write as `126` (overflowing the intended range). The header date is only a "last updated" stamp and affects no record data, so nothing is corrupted, but the reported date is wrong for any file written after 1999.
- **The corpus shows both conventions in use.** Year bytes 91–98 (63 files) are 1990s files under `1900+y`; byte 109 (1 file) is 2009 under `1900+y` and would be an implausible 2109 under mod-100; byte 9 (1 file) is ambiguous between 1909 and 2009. Clipper 5.2e itself writes mod-100. There is therefore no single correct decoding, and any rule is a heuristic.
- **Not related to SET EPOCH.** `SET EPOCH TO <year>` (verified present in STD.CH, mapping to `Set(_SET_EPOCH, <year>)`, default 2000) windows two-digit years at *parse* time in `CTOD` and friends. Date **fields** in records are stored as full `YYYYMMDD` — verified: dates entered under epoch 1900 and epoch 2000 produced `19500615` and `20500615` on disk. Record dates are unambiguous and need no epoch handling; only the header stamp is affected.
- **Resolution requires:** a decision on the windowing rule for reads (a pivot such as "byte < 80 means 2000+y" matches both Clipper 5.2e and the corpus, but is a guess for bytes 80–99 written after 2079), and a decision on what to write — mod-100 matches Clipper, `1900+y` matches part of the corpus. Whichever is chosen, document it as a deliberate divergence and add a round-trip test through the oracle.

- **Implementation (v0.4.5).** Split the decode/encode paths:
  - `decodeHeaderDate`: byte < 80 → 2000+y (Clipper mod-100 post-Y2K); byte ≥ 80 → 1900+y (dBASE III+ legacy). Documented as `headerYearPivot = 80` with the trade-off called out (byte 79 ambiguous between 1979 and 2079 → picks 2079; byte 80 ambiguous between 1980 and 2080 → picks 1980).
  - `encodeHeaderDate`: `year % 100` (matches Clipper 5.2e). Removed the 2155 upper bound — mod-100 keeps the byte ≤ 99 regardless of year.
- **Verified against corpus and oracle.** UM.DBF (header bytes `62 0A 0C`, byte 0x62 = 98) decodes as 1998-10-12 under the legacy branch. A Clipper 5.2e-generated file today carries byte 26 and decodes as 2026 under the pivot branch. Both match reality.
- **One existing test asserted the old broken behavior** (`year - 1900 = 126` for 2026) and was updated to assert `year % 100 = 26` with a T-08 reference in the comment.
- **Five tests, all passing:**
  - `TestHeaderYearPivotDecode` covers the pivot boundary (0, 26, 79, 80, 91, 98, 99, 109) matching both encoders.
  - `TestHeaderYearMod100Encode` covers 1980, 1999, 2000, 2026, 2099, 2100.
  - `TestHeaderYearRoundTripToday` proves encode-then-decode is the identity inside the 2000–2079 window.
  - `TestHeaderYearMatchesCorpusUMDBF` reads a real 1998 corpus file and asserts the correct year.
  - Existing `TestFlushStampsDate` updated to mod-100.

Cross-ref: CHANGELOG 0.4.5.

## [0.4.5] T-07. Duplicate field names rejected on open; Clipper tolerates them (v0.4.5, 2026-07-23)

Theme: dbf · Priority: P2 · Status: ✓ closed by implementation

- **Trigger:** Clipper corpus probe against github.com/ibarrar/clipper, 2026-07-23.
- **Scope:** `Schema.Validate` rejects duplicate field names. `UM.DBF` in the corpus genuinely declares `ACCUMDSUM` twice and was written and used by Clipper, so the format tolerates what this library forbids. The invariant is documented in design02 §3 ("No duplicate field names") and is desirable when creating tables; enforcing it on open makes real files unreadable.
- **Resolution requires:** a decision on splitting the invariant — tolerate duplicates on `Open`, forbid them on `Create` — and, if adopted, a rule for how `Record.Get`/`Set` resolve an ambiguous name (Clipper resolves to the first match). Changes a documented contract, so it is a design decision rather than a fix.

- **DBC hypothesis, tested and falsified (2026-07-23).** Before implementing, verified whether UM.DBF might be a VFP DBC-owned table where the "duplicates" would be shortname collisions disambiguated by long names in a sibling `.DBC`. Every marker refuted this:
  - Version byte `0x03`, byte 28 = `0x00` (no DBC flag). A DBC-owned table would carry `0x30`–`0x32`, or `0x03` with byte 28 bit 2 set.
  - No `.DBC` anywhere in the containing directory — all siblings are `.DBF`/`.NTX`, a Clipper POS/MTS layout.
  - Header size 578 = 32 + 17×32 + 1 + 1: only 1 padding byte, no 263-byte VFP backlink.
  - Field descriptor bytes 12–15 = `99190000` in every field (Clipper stale-memory addresses, oracle §9.2); VFP zeros these.
  - Field descriptor byte 18 = `0x03` everywhere (Clipper stale bytes); VFP uses this byte for `system`/`nullable`/`NOCPTRANS`.
  The three `ACCUMDSUM` fields are genuinely duplicated. Clipper never enforced field-name uniqueness at the format level; the application relied on positional access.
- **Implementation (v0.4.5).** Split `Schema.Validate` into two paths:
  - `Validate()` (public, strict) — used by `Create`, still rejects duplicates. Writing new ambiguous tables would be a step backward.
  - `validateForOpen()` (unexported, permissive) — used by `Open`, tolerates duplicates.
  `Record.Get(schema, name)` resolves to the first matching field via `schemaFieldIndex`'s linear scan, matching Clipper's own behavior. Callers who need positional access to a later duplicate use `GetIndex`/`SetIndex`.
- **Fixture staged.** `dbf/testdata/UM.DBF` from the ha1tch/clipper corpus, provenance in `UM.README.md`. Four tests, all passing: Open tolerates duplicates and finds all three at positions 12/13/14; Create still rejects; public `Validate` still rejects; named access resolves to the first match (field 12).

Cross-ref: CHANGELOG 0.4.5.

## [0.4.0] T-12. FPT memo file support (FoxPro-format, oracle: DBFCDX.LIB) (v0.4.0, 2026-07-23)

Theme: dbf · Priority: P3 · Status: ✓ closed v0.4.0 · Blocks/after: superseded T-11 (closed by finding); enabled T-13

- **Trigger:** T-11 closure by finding, 2026-07-23. The oracle question that framed T-11 as an *assessment* dissolved: Clipper 5.2e's `DBFCDX.LIB` already reads and writes FoxPro-compatible `.FPT` files, per the Clipper 5.x Drivers Guide chapter 4. Same toolchain we used for CDX, no new dependencies.
- **Scope.** Add a `dbf.FPTFile` type alongside the existing `MemoFile` (which handles DBT), with `OpenFPT`/`CreateFPT` and `Get`/`Append` mirroring the DBT API. Support the version-byte convention: `0xF5` marks an FPT-bearing table (versus `0x83` for DBT). Extend `blipperdb` so tables opened via the CDX-aware path can carry FPT memos.
- **Format facts** (from MS Learn `aa975374` and the Apollo API docs):
  - Extension `.FPT`, colocated with the DBF like DBT.
  - Header at block 0 carries a big-endian uint32 at offset 0 (next free block number) and a big-endian uint16 at offset 6 (block size in bytes). Configurable block size, typical range 32–1024. FoxPro's own default is 512; Clipper's DBFCDX default is 64. Blipper's writer should default to 64 to match the oracle byte-for-byte and let callers override.
  - Each memo entry begins with an 8-byte header: type (uint32 big-endian, 0=picture, 1=text, 2=object) and length (uint32 big-endian) of the memo content that follows.
  - **No `0x1A` terminator scan** (this is the key advantage over DBT); length is explicit, so binary data round-trips cleanly.
  - Memo pointer in the DBF record is the same 10-byte right-aligned ASCII block number as DBT — the pointer format is common; only the memo-file layout differs.
- **Verification via existing oracle.**
  1. Extend `CX.PRG` to include a memo field (`M`) and populate it with known values (short, block-spanning, and binary-ish content).
  2. Link with `DBFCDX.LIB` and generate the DBF + FPT pair under the existing DOSBox harness (guard G-01 or a sibling guard G-02).
  3. Byte-compare the FPT against blipper's output for the same schema and values.
  4. Round-trip test: read the Clipper-generated FPT, extract memo values, write them back through blipper's writer, byte-compare the result.
- **API surface expected:**
  - `dbf.OpenFPT(rw)` / `dbf.CreateFPT(rw, blockSize uint16)` — separate constructors to keep block-size at creation time explicit.
  - `FPTFile.Get(blockNo uint32) ([]byte, MemoType, error)` — returns type and content.
  - `FPTFile.Append(data []byte, memoType MemoType) (uint32, error)` — returns first block used.
  - `MemoType` constant enum (`MemoPicture` = 0, `MemoText` = 1, `MemoObject` = 2).
  - `dbf.Table.OpenFPT(rw)` / `.CreateFPT(rw, blockSize)` — table-level helpers when the DBF's version byte is `0xF5`.
- **Version byte handling.** Extend the `Open` validity gate to accept `0xF5` alongside `0x03`/`0x83`, dispatching to FPT rather than DBT when the byte is `0xF5`. This is the single riskiest change in T-12: it affects the version-byte switch, which currently rejects `0xF5` in `TestOpenRejectsUnknownVersion`. That test needs updating.
- **Constraints.**
  - No `_NullFlags`, no VFP field types (`Y`/`T`/`I`/`B`/`G`/`V`/`W`/`Q`) — those remain out of scope.
  - Free-block reclamation on rewrite: FPT does not track a free list like DBT; overwriting a memo either fits in the existing allocation (if the new content is smaller or equal in blocks) or grows to a fresh allocation, leaving the old blocks orphaned. This matches how FoxPro itself behaves. Callers who need compaction issue a `PACK` (T-03 territory).
  - Block-size mismatch: reader rejects FPT files whose block size is 0 or not a multiple of 32, matching FoxPro's constraint.
- **Effort estimate.** ~1 to 1.5 weeks. FPT reading is simpler than DBT (no terminator scan, no unused-block-marker heuristics); writing is simpler in the length-known-up-front case and harder for large binary blobs that span many blocks (still nothing like the complexity of CDX compression).
- **Dependencies.** Independent of T-10 (DBC sidecar), which currently notes that its memo fields switch from `.DBT` to `.FPT` when T-12 lands. T-10 sequencing does not change; when T-10 begins, whichever memo format is current becomes what its tables carry.
- **Deliberately out of scope for T-12: blipperdb integration for memo-bearing tables.** blipperdb has no memo integration for either DBT or FPT today — callers open the sibling memo file directly via `dbf.OpenMemo` or `dbf.OpenFPT`. Adding FPT-only integration to blipperdb would be lopsided and would bake in an asymmetry between the two formats. Both formats land together in **T-13**, using `Table.MemoFormat()` as the dispatch surface.
- **Progress log:**
  - v0.3.1 shipped the FPT core (OpenFPT/CreateFPT/Get/Append), version-byte round-trip (0xF5 accepted, no silent demotion on rewrite), 8 tests.
  - v0.3.2 (interim) added `Table.MemoFormat()` and the `MemoFormat` enum: 5 tests including proof that a header rewrite preserves the FPT flavour.
  - **Remaining**: oracle byte-comparison test via extended `CX.PRG` + `DBFCDX.LIB` under DOSBox (guard G-01 or a sibling). When that lands, T-12 can close.

Cross-ref: CHANGELOG 0.4.0.

## [0.3.1] T-11. Assess viability of FPT memo file support (needs FoxPro 2.x oracle) (closed by finding, 2026-07-23)

Theme: dbf · Priority: P3 · Status: ✓ closed by finding · Closure: no implementation, no assessment — the premise was false

**Closed by finding.** T-11's premise was that assessing FPT viability required a separate FoxPro 2.x toolchain to serve as an oracle. That premise was wrong. Clipper 5.2e's `DBFCDX.LIB` — which we already use as the oracle for CDX (T-09) — also reads and writes FoxPro-compatible `.FPT` memo files. Per the Clipper 5.x Drivers Guide chapter 4: "The DBFCDX driver uses FoxPro compatible memo (.fpt) files to store data for memo fields. These memo files have a default block size of 64 bytes rather than the 512 byte default for (.dbt) files."

Consequences:
- No FoxPro 2.6 download, no new DOSBox harness, no oracle question. The existing Clipper 5.2e oracle (`docs/CLIPPER_ORACLE.md`) covers FPT.
- FPT implementation is a straightforward next item, filed as **T-12**, sized as ~1 to 1.5 weeks with round-trip verification against Clipper-generated FPT files.
- Clipper's DBFCDX defaults to 64-byte FPT blocks (FoxPro itself defaults to 512); T-12's writer should let the caller pick and default to 64 to match what the oracle produces (so diffs will be byte-exact).

Original T-11 detail preserved verbatim below for the archival record.

---

Theme: dbf · Priority: P3 · Status: ☐

- **Trigger:** user request following the FPT/DBT boundary finding recorded in `docs/CLIPPER_ORACLE.md §5.1` (2026-07-23).
- **Scope, not a foregone conclusion.** `.FPT` is FoxPro's memo format, structurally distinct from dBASE III+'s `.DBT` this library already implements: configurable block size (`SET BLOCKSIZE`), explicit type + length header per block rather than terminator scanning, and a 4-byte binary memo pointer in the record rather than the DBT 10-byte right-aligned ASCII. Byte-level spec at Microsoft Learn `aa975374`.
- **Why the question matters.** VFP itself treats DBT as read-legacy: `COPY` of a DBT-bearing table produces an FPT, per the Hacker's Guide chapter cited in §5.1. So FPT is the memo format for anything FoxPro-lineage. In particular, T-10 level 3 (VFP-writable sidecar) requires FPT, and any future move toward reading real VFP tables also does. T-11's outcome therefore gates part of T-10's scope.
- **Viability question, not implementation task.** The point is to decide whether FPT belongs in blipper, not to implement it. The load-bearing question is byte-for-byte oracle validation. `DBFCDX.LIB` does not help — it produces CDX indexes but leaves tables and memos at dBASE III+ (`0x03`/`0x83`, `.DBT`).
- **Assessment plan (~1 day of oracle work):**
  1. Fetch FoxPro 2.6 for DOS from WinWorld (URL discussed earlier in this session).
  2. Test whether it links and runs under DOSBox via the harness in `CLIPPER_ORACLE.md §2`–§4. Expect fresh harness traps; document them there.
  3. If it runs, generate a minimal FPT with a known memo value, decode against the MS Learn spec, confirm agreement. On success, file an implementation item (probably T-12) and register a second dormant guard alongside G-01.
  4. If it does not run under DOSBox, or if generated files disagree with the spec, FPT joins VFP field types in the "documented but not verifiable" category and is likely declined, with the reasoning recorded here as the resolution.
- **Effort estimate for implementation, should assessment succeed:** roughly 1–1.5 weeks. FPT is easier than DBT for reading (explicit length, no terminator scan) and harder for writing well (block sizing, packing). The memo-pointer format change touches every DBF codec site that currently reads memo columns.
- **Dependencies:** structurally independent of T-09 and T-10. Directly bears on whether T-10 level 3 is even reachable.

Cross-ref: CHANGELOG 0.3.1, T-12.

## [0.3.0] T-09. CDX compound index support (multi-tag, compressed, conditional) (v0.3.0, 2026-07-23)

Theme: ntx · Priority: P2 · Status: ✓ closed v0.3.0 · Blocks/after: unblocks T-10

- **Trigger:** oracle investigation of alternative RDDs, 2026-07-23.
- **Scope:** `ntx` implements Clipper's NTX format only: one key per index file, fixed-width uncompressed keys, ascending order, no conditional indexes. Clipper 5.2e also ships `DBFCDX.LIB`, which writes FoxPro-2-compatible compound indexes: up to 99 tags in a single file, compressed keys (trailing blanks and shared prefixes between adjacent keys stored in one or two bytes), conditional tags (`FOR`/`WHILE`), and descending order. A `cdx` package alongside `ntx` would address the retrieval limits recorded against this library — notably that `blipperdb` navigation re-descends the tree per step (design03 §4.2) and that a multi-condition query has no composite key to use.
- **Verifiable today.** `DBFCDX.LIB` is a reference implementation already present in the toolchain, so the same byte-for-byte comparison that closed T-01 and T-02 applies. Confirmed working under guard G-01: a generator linked with DBFCDX produced `CDATA.CDX`, 5120 bytes, ten 512-byte blocks, root at offset 1024, tag-index root holding 2 keys (tags `BYCODE` and `BYNVAL`), with the companion DBF carrying `0x01` in the byte-28 table-flags field marking a structural CDX. No new downloads or licensing questions, unlike Visual FoxPro field types (`0x30` family), which nothing in the toolchain or corpus can generate.
- **Effort estimate:** roughly 1–2 weeks. The page format is materially harder than NTX because of key compression, which is where the defects would be; the tag directory is a second index layer above the key indexes; and `blipperdb` would need an order-selection API that names tags rather than index files.
- **Verified decode.** The Microsoft Learn spec (`aa975346`/`aa975347`) matches the CDX generated under G-01 byte-for-byte. Header options `0xE0` decoded as compact + compound-header; the tag-index root node was located and its two entries (`BYCODE`, `BYNVAL`) recovered from the bit-packed record/dup/trail encoding — including a `dup=2` on the second entry, i.e. real prefix compression with `BY` shared and only `NVAL` stored. The spec is not merely documented, it is understood.
- **Collation caveat.** VFP 3.0 added named collation sequences (GENERAL, SPANISH, etc.) baked into the CDX at creation and not re-orderable at open time. A `cdx` package that ignores the collation identifier and compares bytes will traverse in the wrong order and return wrong results with no error — the same class of silent-wrong-answer bug that space-vs-zero padding was in NTX, and one the oracle cannot catch because `DBFCDX.LIB` produces only MACHINE collation. **The package must read the collation identifier and refuse anything non-MACHINE**; failing loudly is the only safe behaviour.
- **Resolution requires:** a `cdx` package with (a) read support: open, tag enumeration, collation check with MACHINE-only enforcement, seek, ordered traversal, decompression, verified against Clipper-generated files; (b) write support: tag creation, compression, rebalancing; (c) a decision on whether `blipperdb` exposes CDX tags through the existing `SetOrder` numbering or a new tag-named API. Read-only is a viable shipping milestone; write can wait.

Cross-ref: CHANGELOG 0.3.0.

## [0.2.0] T-02 — Memo (.DBT) support deferred (v0.2.0, 2026-07-23)

Theme: dbf · closed 0.2.0 · 2026-07-23


- **Trigger:** design scope (design01 §2.1), 2026-07-23.
- **Scope:** .DBT memo files are deferred by design. The record codec round-trips the 10-byte memo block reference untouched, so read-modify-write of existing files with memo fields preserves DBT pointers, but memo content is unreachable through this library.
- **Update 2026-07-23:** the dBASE III+ .DBT format is now verified against Clipper 5.2e output (guard G-01, generated table with short, empty, block-spanning and trailing memos). Rules: 512-byte blocks; block 0 is the header, carrying the next-free-block number as uint32 LE at offset 0; the DBF memo field holds the starting block number as right-aligned ASCII in 10 bytes, all spaces when the memo is empty (no block is allocated for an empty memo, and 0 is never used as a pointer); memo text is terminated by `0x1A 0x1A`; text spans consecutive blocks freely, occupying `ceil((len+2)/512)` of them, and the next memo begins at the following block. Note the terminator may appear truncated to a single `0x1A` in the final block of a file, where the second byte falls beyond EOF. A reader must not treat the first `0x1A` as the end: memo text can legitimately contain `0x1A` bytes, so scan for the pair. Corpus samples: `CLIPPER5/SOURCE/TBROW/GENERAL/MEMOTEST.DBF` (4 records, populated .DBT), and `AIM/TSMS/DATA/REPORTX.DBF`/`REPORTZ.DBF` — but note both REPORT* files reference blocks beyond their 512-byte .DBT, so their memo content was never archived; they are useful only for header and pointer parsing, not content round-trips.
- **Resolution requires:** a `.DBT` reader and writer, a decision on whether `dbf.Table` opens the companion file itself (a departure from the io.ReadWriteSeeker convention, since the memo file is a second stream) or whether the caller supplies it, and free-block reuse policy on rewrite. Round-trip tests against oracle-generated files, plus a read test against MEMOTEST.DBF.

Cross-ref: CHANGELOG 0.2.0.

## [0.2.0] T-01 — Clipper negative numeric NTX key transform unverified (v0.2.0, 2026-07-23)

Theme: ntx · closed 0.2.0 · 2026-07-23


- **Trigger:** NTX key helper implementation, 2026-07-23.
- **Scope:** Clipper stores numeric NTX keys as ASCII with a byte transform for negative values so they collate below positives. The exact transform could not be verified against an authoritative source (Harbour hbrddntx `hb_ntxNumToStr`) during the session. `NumericKey` therefore errors on negative input rather than guessing a binary encoding.
- **Update 2026-07-23:** the transform is now verified. A Clipper 5.2e oracle under headless DOSBox generated indexes over both signs; the rule is `out = 0x5C - in` applied bytewise to the zero-padded ASCII rendering of the absolute value, decimal point preserved (nine's complement shifted down by four). Confirmed exact on seven values across two independent runs, including the `9 -> #` extreme. Full derivation, digit map and reproduction procedure in docs/CLIPPER_ORACLE.md §9.
- **Resolution requires:** implement the transform in `ntx.NumericKey`, remove the negative-input error, and add round-trip tests plus a collation test asserting negatives sort below positives and larger magnitudes sort first. Ideally also a golden test against a Clipper-generated NTX.

Cross-ref: CHANGELOG 0.2.0.

## [0.2.0] T-06 — MaxFields=128 rejects valid Clipper tables (v0.2.0, 2026-07-23)

Theme: dbf · closed 0.2.0 · 2026-07-23


- **Trigger:** Clipper corpus probe against github.com/ibarrar/clipper, 2026-07-23.
- **Scope:** `dbf/schema.go` declares `MaxFields = 128` with the comment "practical dBASE III+ limit". The limit is invented; it has no basis in the format, where the field count is bounded only by the header size. Four valid production tables written by Clipper are rejected by `Schema.Validate`: `CASH.DBF` and `CASH0198.DBF` (159 fields), `TERMINAL.DBF` and `TERM0198.DBF` (156 fields). These are unremarkable files — `TERM0198.DBF` is 249 KB, 172 records, 1418-byte records, with ordinary field names.
- **Resolution requires:** derive the bound from the header instead of a constant (a 16-bit header size caps the count near 1020), or raise the constant to the true format limit; a regression test built from a table with more than 128 fields.

Cross-ref: CHANGELOG 0.2.0.

## [0.1.0] T-05 — blipperdb session layer (work-area pool for USE semantics) (v0.1.0, 2026-07-23)

Theme: blipperdb · closed 0.1.0 · 2026-07-23


- **Trigger:** requested by Horatio, 2026-07-23.
- **Scope:** New package blipperdb (conventional import alias bdb): central BlipperDB object pooling open tables and indexes by alias so a Clipper-like language can USE A / USE B / USE C in one running session. Work areas carry attached indexes with KeyFuncs, a controlling order, and a record pointer (GoTop/GoBottom/Skip/GoTo/Seek/Eof/Bof). Area-level writes maintain all attached indexes.
- **Depends on:** ntx insert/delete (in progress this session).

Cross-ref: CHANGELOG 0.1.0.

## [0.1.0] T-04 — Design01 §5.3 reconciliation banners needed (readHeader/writeHeader) (v0.1.0, 2026-07-23)

Theme: docs · closed 0.1.0 · 2026-07-23


- **Trigger:** dbf open/flush implementation, 2026-07-23.
- **Scope:** Implementation deviates from design01 §5.3 stated internal signatures: readHeader additionally returns on-disk header/record sizes; writeHeader takes explicit sizes. Root cause: DBF files with padded headers must survive open and header rewrite without corruption. Design docs need dated reconciliation banners recording what actually shipped (working agreement Part 3 §5).

Cross-ref: CHANGELOG 0.1.0.

## [genesis] T-00 — repository created (v0.0.0, 2026-07-23)

Initial commit: design01.md, design02.md, partial dbf package.





RCH_NOTES.md.

T-34.


pes.go, T-35.


go, T-36.

