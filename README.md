# blipper

An implementation of an xBase language modeled after Nantucket
Clipper 5.x.

This repository currently contains the storage foundation: Go
packages for the dBASE III+ / Clipper 5.x on-disk formats and the
work-area session layer a Clipper-like runtime needs.

## Packages

| Package | Purpose |
| ------- | ------- |
| `dbf` | dBASE III+ `.DBF` tables: create, open, read, append, update, logical delete, sequential cursor |
| `ntx` | Clipper 5.x `.NTX` indexes: B-tree insert/delete, ordered cursors, prefix seek, point queries, key encoding helpers |
| `blipperdb` | The session layer: a `BlipperDB` object pooling tables and indexes in named work areas with USE/SELECT/SKIP/SEEK semantics |

The `dbf` and `ntx` packages operate on `io.ReadWriteSeeker` and never
open or close files; `blipperdb` owns what it is handed. The
conventional alias for `blipperdb` is `bdb`.

## Example

```go
import (
    "os"

    bdb "github.com/ha1tch/blipper/blipperdb"
    "github.com/ha1tch/blipper/dbf"
    "github.com/ha1tch/blipper/ntx"
)

func main() {
    f, _ := os.OpenFile("people.dbf", os.O_RDWR, 0)
    ix, _ := os.OpenFile("people.ntx", os.O_RDWR, 0)

    db := bdb.New()
    defer db.CloseAll()

    area, _ := db.Use("PEOPLE", f)

    schema := area.Table().Schema()

    area.SetIndex(ix, func(r dbf.Record) []byte {
        v, _ := r.Get(schema, "NAME")
        return ntx.CharKey(v.(string), 20)
    })

    area.Seek([]byte("SMITH"))

    for !area.Eof() {
        rec, _ := area.Record()
        _ = rec // ...
        area.Skip(1)
    }
}
```

## Compatibility notes

* Character, Numeric, Float, Logical and Date fields are fully
  supported. Memo fields round-trip their 10-byte block reference
  untouched, so existing files with memos are not corrupted, but memo
  content is not interpreted (`.DBT` support is deferred).
* Files with padded headers (header size larger than the computed
  minimum) open correctly and keep their layout across header
  rewrites.
* Two deliberate divergences from Clipper, both safer for data:
  numeric overflow on encode is an error rather than an asterisk
  fill, and oversize character values truncate exactly as Clipper's
  REPLACE does.
* NTX numeric keys: non-negative values only for now; Clipper's byte
  transform for negative numeric keys is tracked in
  `docs/TRACKING.md` (T-01) pending verification against real Clipper
  output.
* Deletion is logical (`PACK` is future work), and deleted records
  are kept in indexes, as Clipper does.

Design documents: `design01.md` (architecture), `design02.md` (DBF
core contracts), `design03.md` (the blipperdb session layer). Work
tracking: `docs/TRACKING.md` and `docs/RESOLVED.md`.

## Requirements

* Go 1.25 or later

## License

GPL v3. See LICENSE.

Copyright (c) 2026 haitch
h@ual.li · https://oldbytes.space/@haitchfive
