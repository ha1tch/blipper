# blipper — Resolution Record

Append-only, newest first. Each entry is the item's full register text
as at closure, stamped with closing version and date.

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
