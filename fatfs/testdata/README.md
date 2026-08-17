# fatfs test fixtures

Generated 2026-07-23 with dosfstools and mtools, both standard
packages:

    dd if=/dev/zero of=fat16.img bs=1M count=16
    mkfs.vfat -F 16 -s 4 -n TESTVOL fat16.img

    dd if=/dev/zero of=fat32.img bs=1M count=40
    mkfs.vfat -F 32 -s 1 -n TESTVOL fat32.img

    mcopy -i <img> small.txt ::SMALL.TXT
    mcopy -i <img> big.bin   ::BIG.BIN
    mcopy -i <img> weird.bin ::WEIRD.BIN

Sizes are set by the FAT specification's cluster-count rule, not
by preference: FAT16 requires at least 4085 clusters and FAT32 at
least 65525, so these are close to the smallest images that
legitimately carry each type.

Images are stored gzipped (38 KB and 63 KB) because almost all of
both is zeroes.

Content:

| File      | Size  | Purpose                                        |
|-----------|-------|------------------------------------------------|
| SMALL.TXT | 14    | fits in one cluster                            |
| BIG.BIN   | 20000 | spans several clusters; exercises chain walking |
| WEIRD.BIN | 14    | binary, contains 0x1A and 0x00                 |

`mdir -i <img> ::` confirms all three in both images.

## LFN fixture, 2026-07-23

`lfn32.img.gz` carries genuine VFAT long-name entries written by
`mcopy`, for T-17:

    dd if=/dev/zero of=lfn32.img bs=1M count=40
    mkfs.vfat -F 32 -s 1 -n LFNTEST lfn32.img
    mcopy -i lfn32.img longsrc.txt ::"CUSTOMERS_ARCHIVE_2024.DBF"
    mcopy -i lfn32.img accsrc.txt  ::"Ünïcödé Namé.DBF"
    mcopy -i lfn32.img longsrc.txt ::SHORT.TXT

`mdir` confirms the aliases mtools generated:

| long name | alias |
|-----------|-------|
| `CUSTOMERS_ARCHIVE_2024.DBF` | `CUSTOM~1.DBF` |
| `Ünïcödé Namé.DBF` | `__N__C~1.DBF` |
| (none — already 8.3) | `SHORT.TXT` |

The third file is deliberate: a directory mixing long-named and
plain entries is what a real volume looks like, and the read path
has to handle a short entry with no preceding run.
