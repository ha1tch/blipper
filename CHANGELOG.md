# Changelog

## [0.1.0] - 2026-07-23

First working release of the storage foundation.

### dbf

- Fixed the `RecordSize` type mismatch that prevented compilation.
- Schema-aware `Record.Get`; per-field zero values (`int64` for
  whole-number Numeric fields).
- Record codec for C/N/F/L/D fields with exact dBASE III+ wire
  format; memo block references round-trip untouched.
- `Create`, `Open` (validates on-disk sizes, honours padded headers),
  `Table` CRUD (`Get`/`Put`/`Append`/`Delete`/`Undelete`/`Flush`),
  sequential `Cursor`.
- `errors.go` per design02 §14.

### ntx

- Full Clipper 5.x NTX implementation: 1024-byte header and page
  codecs (reader follows permuted slot arrays), preemptive-split
  B-tree insert with UNIQUE semantics, delete with borrow/merge
  rebalancing, free-page list with reuse, root growth and collapse.
- Ordered cursor with `First`/`Seek` (prefix seek via space padding).
- Point queries: `Min`, `Max`, `FirstGE`, `Successor`, `Predecessor`.
- Key helpers: `CharKey`, `DateKey`, `LogicalKey`, `NumericKey`
  (non-negative; see T-01), and `Build`.

### blipperdb

- New session layer exposing the `BlipperDB` object: named work
  areas with `Use`/`Create`/`Select`/`CloseArea`/`CloseAll`.
- Work areas with controlling order, xBase record pointer
  (`GoTop`/`GoBottom`/`GoTo`/`Skip`/`Seek`/`Eof`/`Bof`), and
  index-maintaining `Append`/`Replace`; `Delete`/`Recall` keep
  deleted records in indexes as Clipper does.

### Repository

- `go.mod` (Go 1.25), work tracking under `docs/`, reconciliation
  banner on design01, design03 for blipperdb. Closes register items
  T-04 and T-05 (see `docs/RESOLVED.md`).
