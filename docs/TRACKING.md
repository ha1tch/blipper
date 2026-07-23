# blipper — Live Register

Version: 0.1.0
Last reviewed: 2026-07-23

Open actionable items only. Closed items move verbatim to
docs/RESOLVED.md as part of the closing release.

## Status table

| ID | Summary | Theme | Priority | Status | Blocks |
|---|---|---|---|---|---|
| T-01 | Clipper negative numeric NTX key transform unverified | ntx | P2 | ☐ | — |
| T-02 | Memo (.DBT) support deferred | dbf | P3 | ☐ | — |
| T-03 | PACK operation not implemented | dbf | P3 | ☐ | — |

## ntx

### T-01. Clipper negative numeric NTX key transform unverified

Theme: ntx · Priority: P2 · Status: ☐

- **Trigger:** NTX key helper implementation, 2026-07-23.
- **Scope:** Clipper stores numeric NTX keys as ASCII with a byte transform for negative values so they collate below positives. The exact transform could not be verified against an authoritative source (Harbour hbrddntx `hb_ntxNumToStr`) during the session. `NumericKey` therefore errors on negative input rather than guessing a binary encoding.
- **Resolution requires:** verified transform from Harbour source, or byte-level comparison against real Clipper-generated NTX files with negative numeric keys, plus round-trip tests.

## dbf

### T-02. Memo (.DBT) support deferred

Theme: dbf · Priority: P3 · Status: ☐

- **Trigger:** design scope (design01 §2.1), 2026-07-23.
- **Scope:** .DBT memo files are deferred by design. The record codec round-trips the 10-byte memo block reference untouched, so read-modify-write of existing files with memo fields preserves DBT pointers, but memo content is unreachable through this library.


### T-03. PACK operation not implemented

Theme: dbf · Priority: P3 · Status: ☐

- **Trigger:** design scope (design01 §9), 2026-07-23.
- **Scope:** Deletion is logical only. Physical removal of deleted records (PACK), and rebuilding any NTX indexes after a PACK, are future work.

---
