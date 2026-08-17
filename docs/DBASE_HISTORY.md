# The early history of dBASE

Version: 0.9.25
Last reviewed: 2026-07-24

## Provenance

Restated from a Usenet posting by Romain Strieff (dBVIPS), which
itself is drawn from *dBASE Language Reference with Annotations*
(ISBN 0-679-79173-6, Borland Press), credited to Masterson, with
Ratliff and Long. Ratliff and Long are C. Wayne Ratliff and Jeb
Long — the two engineers the account is about. Retrieved
2026-07-24 from a document in danger of disappearing along with
the rest of the pre-Microsoft dBASE web presence; the posting's
own footer points at `dbase.com/cnt/newsguid.htm`, itself now
gone.

Unlike the other documents in this directory, this one has no
bytes to verify — it is a narrative source, kept for the same
reason as the others: the primary account is fragile and worth
having a copy of in a place more durable than a mailing list
archive. Wording below is a restatement, not a copy; the source
posting also flags its own likely typo (1992 for 1982), corrected
here.

**Durability of the original: poor.** A single Usenet posting
quoting an out-of-print book, referencing a vendor page that no
longer resolves.

---

## Why this belongs in blipper's documentation

Everything else in this directory describes what the DBF family
*is*, byte by byte. This describes why it has the shape it does —
and two details bear directly on decisions blipper has already
made.

**The B-tree lineage predates every index format blipper
implements.** Ratliff was building B-tree indexing into the
product in 1980, upgraded to B\*-trees before the first commercial
release. NTX, NDX, CDX, IDX and MDX are all downstream of that
choice, made a decade before any of them existed as named formats.

**The name "dBASE" itself was never Ratliff's**, nor was
"Ashton-Tate" — both came from an advertising campaign built
around a product Harris Computers had forced a rename of. That is
a concrete data point for the "facts, not names" position blipper
has taken throughout this session's licensing questions: the
*format* is Ratliff's independent engineering work from 1978, two
renames removed from any corporate identity that later claimed
it.

---

## The account

### Origins at JPL, 1975–1978

C. Wayne Ratliff worked at the Jet Propulsion Laboratory, on the
ground-support database for Viking lander telemetry from Mars.
The tool available, MFILE, was punch-card driven and slow enough
that the limitation itself was the motivation for what followed.

Arthur C. Clarke's *2001: A Space Odyssey* pushed Ratliff toward
reading about artificial intelligence, natural language, and
database management on his own time, and he began working
original B-tree and pattern-searching ideas into code at work.

In 1976 he built an IMSAI 8080 — over a year of assembling it
himself, down to the solder joints — running PTDOS, a disk
operating system from Processor Technology, since CP/M did not
yet exist. He began writing in 8080 assembly.

His first home project was a small database manager, modelled
loosely on Viking's MFILE and influenced by the manual for JPL's
own Data-Management and Information System (JPLDIS), which he had
read but not yet used. The IMSAI's 48 KB of memory, further
reduced once the operating system loaded, was the practical
constraint the whole design worked within.

**The first DBF file was created around midnight on 29 January
1978**, the night Ratliff finished the `CREATE` command. `DISPLAY
STRUCTURE`, `DISPLAY`, `APPEND`, and `EDIT` followed within
months, along with BCD floating-point arithmetic.

### Vulcan, 1978–1980

After a three-month hardware failure, Ratliff returned to the
project with a broader ambition: real programming constructs,
borrowed deliberately from elsewhere. `?` and `INPUT` came from
BASIC, `DO WHILE` from structured FORTRAN, `ACCEPT` from COBOL.
Others, in his own words, "came from thin air."

He called it **Vulcan**. The premise was considered implausible
at the time — nobody expected the new hobbyist microcomputers to
be trustworthy enough for data that mattered.

In 1979, Ratliff showed Vulcan to Jeb Long, the lead developer of
JPLDIS, who saw commercial potential partly *because* Vulcan
shared enough with JPLDIS to be familiar to JPL's own staff. JPL
licensed Vulcan and asked for changes bringing it closer to
JPLDIS — the first time Ratliff had actually used the system his
program had been informed by, rather than only read its manual.

Ratliff moved the program to CP/M, and — prompted by MicroPro's
DataStar pushing full-screen interaction into the market —
rebuilt `EDIT`, `APPEND` and `INSERT` for full-screen use, added
the first `.FMT` format file, and put B-tree indexing at the
centre of the product.

Vulcan sold 61 copies in nine months, enough to draw the attention
of marketers George Lashlee and Hal Pawluk, who secured a one-year
exclusive right to sell it in August 1980.

### Becoming dBASE II, and Ashton-Tate

Harris Computers held prior claim to the name "Vulcan" for an
operating system and threatened action over market confusion. The
product was renamed **dBASE II**. Pawluk produced the advertising
line "dBASE II vs. The Bilge Pumps" — and, in the same stroke,
invented the company name **Ashton-Tate**, which had not existed
until that campaign.

Before the first sale, Ratliff upgraded the indexes again, to
**B\*-trees**, added the `@` command, and improved the overlay
structure. George Tate began giving copies away for evaluation
ahead of the February 1981 general release, after which sales grew
quickly.

### dBASE III and beyond

Jeb Long joined full-time in late 1981, having taken over JPLDIS
after its original author left unfinished work — Ratliff's
assessment of him: *"a week for Jeb is about equal to three months
for the average programmer."* Long ported the screen driver for
Osborne machines and translated the 8080 codebase to 8086, timed
to the IBM PC's arrival.

Ratliff left Martin Marietta in 1982 to work on dBASE full-time.
Competitive pressure from Microrim's R:Base pushed a rapid
development effort for **dBASE III**, led by Ratliff with Jordan
Brown, Alastair Dallas, John Gieselman, Jeb Long, Chip Morton and
Jim Rowe. It shipped in June 1984 and became the format's de facto
standard — the version blipper's own oracle-verified `0x03`/`0x83`
support traces back to.

---

## What this adds to the technical record

A few points worth holding alongside `docs/DBASE_FORMAT.md`:

- **The format existed before the company that would own it.**
  Vulcan, 1978–1980, predates Ashton-Tate's founding.
- **The indexing lineage is older than any named format blipper
  implements.** B-trees in 1980, B\*-trees before the 1981 release
  — years before NDX, NTX, CDX, IDX or MDX existed as distinct
  formats with the byte layouts `docs/INDEX_FORMATS.md` describes.
- **dBASE III (1984) is the release the whole family's `0x03`
  version byte represents**, and the one every subsequent lineage
  — FoxBASE, FoxPro, Clipper — built compatibility against.

---

## Cross-references

- `docs/DBASE_FORMAT.md` — the byte-level table and memo formats
- `docs/INDEX_FORMATS.md` — NDX, NTX, CDX, IDX, MDX, including the
  B\*-tree structure this account traces to 1980
- `docs/RESEARCH_NOTES.md` — durability assessments for the other
  fragile sources this project depends on
