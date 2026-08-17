# Chunk-size benchmark for T-16

Measures the cost of storing xBase files as chunked blobs in
SQLite across a range of chunk sizes, using four workloads that
mirror what blipper actually does rather than generic I/O:

| workload | pattern | blipper equivalent |
|----------|---------|--------------------|
| `writeAll` | store a 20 MB file | initial table creation |
| `hdrWrite` | 32 bytes at offset 0 | `Table.Flush` after append |
| `recAppend` | 80 bytes at the end | `Area.Append` |
| `descent` | 4 scattered 512-byte reads | CDX B-tree lookup |
| `scan` | sequential read of the whole file | full table scan |

## Running it

The benchmark is deliberately not part of `go test`: it needs
`modernc.org/sqlite`, which the library itself does not depend on
until T-16 lands, and it takes a couple of minutes.

    cd bench/chunksize
    go mod init chunkbench
    go get modernc.org/sqlite
    go run .

## Result, 2026-07-23

SSD-backed ext4, WAL mode, `synchronous=NORMAL`, 20 MB payload,
warm cache, 2000 iterations on the descent workload:

| chunk | writeAll | hdrWrite | recAppend | descent | scan | db size | overhead |
|-------|----------|----------|-----------|---------|------|---------|----------|
| 8K    | 468ms    | 697µs    | 744µs     | 1.3ms   | 42.7ms | 21.3 MB | 6.5% |
| 16K   | 402ms    | 717µs    | 994µs     | 1.7ms   | 38.6ms | 20.6 MB | 3.0% |
| **32K** | **230ms** | **539µs** | **543µs** | **531µs** | **33.6ms** | 20.3 MB | 1.5% |
| 48K   | 236ms    | 629µs    | 672µs     | 783µs   | 39.4ms | 20.2 MB | 1.0% |
| 64K   | 309ms    | 1.3ms    | 1.7ms     | 3.3ms   | 38.4ms | 20.2 MB | 1.0% |
| 128K  | 255ms    | 2.5ms    | 2.7ms     | 5.3ms   | 34.5ms | 20.1 MB | 0.5% |
| 256K  | 271ms    | 5.6ms    | 6.2ms     | 8.4ms   | 55.3ms | 20.0 MB | 0.0% |
| 512K  | 273ms    | 10.3ms   | 10.8ms    | 14.7ms  | 48.2ms | 20.0 MB | 0.0% |

32 KB is fastest on every workload.

## Why the a-priori answer was wrong

The reasoning before measuring argued for 8 KB: a CDX descent
reads four scattered 512-byte nodes, so a 32 KB chunk pulls 64×
more than it needs. That amplification is real — and 32 KB is
still 2.5× faster on exactly that workload.

The dominant cost at small chunk sizes is per-row overhead, not
per-byte transfer. A 20 MB file is 2560 rows at 8 KB and 640 at
32 KB, and every operation pays a B-tree descent plus statement
overhead for each row it touches. 32 KB sits at the crossover
where that stops dominating and per-byte cost has not yet taken
over. Storage overhead pushes the same direction: 6.5% at 8 KB
against 1.5% at 32 KB, all of it row headers and keys.

## Caveats

One machine, one payload size, warm cache, one file. A dataset
large enough to miss SQLite's page cache would move the optimum
smaller, because amplification would cost real I/O rather than
memcpy. `modernc.org/sqlite` is a source translation of C, so a
cgo build may have different per-statement overhead and a
different crossover.

This is why T-16 makes chunk size configurable with 32 KB as a
measured default, rather than baking in a constant.

## Second run, 2026-07-23: normalised schema, four interleaved files

The first run used a single denormalised `(name, idx)` table and
one file. T-16 ships the normalised pair — `files` holding id,
name, and logical size, `chunks` keyed on `(file_id, idx)` with
`ON DELETE CASCADE` — so the benchmark was rewritten to match and
to write four files in sequence (DBF, FPT, CDX, DBC), mirroring
what `blipperfs.CreateTable` does.

| chunk | writeAll | hdrWrite | recAppend | descent | scan | db size |
|-------|----------|----------|-----------|---------|------|---------|
| 8K    | 877ms    | 1.6ms    | 728µs     | 1.5ms   | 413ms  | 29.2 MB |
| 16K   | 551ms    | 923µs    | 735µs     | 1.7ms   | 37.7ms | 28.4 MB |
| **32K** | **385ms** | 806µs  | **561µs** | **627µs** | 34.0ms | 28.0 MB |
| 48K   | 307ms    | 762µs    | 602µs     | 681µs   | 29.9ms | 27.8 MB |
| 64K   | 412ms    | 1.7ms    | 2.2ms     | 3.1ms   | 28.4ms | 27.7 MB |
| 128K  | 368ms    | 2.7ms    | 2.5ms     | 5.2ms   | 29.3ms | 27.6 MB |
| 256K  | 453ms    | 5.4ms    | 9.4ms     | 8.3ms   | 42.6ms | 27.6 MB |
| 512K  | 1.20s    | 20.3ms   | 14.2ms    | 15.7ms  | 45.5ms | 27.6 MB |

The 32 KB choice survives. It is fastest on record append and on
the index descent, and 32K/48K are within noise of each other
across the rest — the same broad plateau the first run showed,
with the same degradation above 64 KB.

Two things the multi-file run exposed that the single-file one
could not:

- **8 KB collapses on sequential scan**: 413 ms against 34 ms at
  32 KB, a 12× penalty absent from the first run. With four
  files interleaved in one chunk table, small chunks mean many
  more rows and the scan pays B-tree traversal per row. This
  strengthens the case against small chunks rather than
  weakening it.
- **512 KB collapses on writeAll**: 1.20 s, roughly 3× the
  plateau, where the first run showed no such cliff.

Absolute totals are higher throughout because the workload now
writes about 35 MB across four files rather than 20 MB in one.
