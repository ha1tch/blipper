BYCODE.IDX generated 2026-07-23 by Clipper 5.2e's DBFCDX driver
(`INDEX ON CODE TO BYCODE` against a 4-record C(10) table:
DELTA, ALPHA, CHARLIE, BRAVO). See docs/RESEARCH_NOTES.md finding 2.

Confirms: header fields (root=1024/page 2, keylen=10, opts=0x20
compact), and the compact leaf layout (bit-packed 3-byte entries,
recno/dup/trail bit widths 16/4/4, keys packed backward from the
page end).
