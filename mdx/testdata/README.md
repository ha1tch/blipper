ACCT_REC.MDX, from Borland dBASE 5.0 for DOS (1994), already
vendored at dbf/testdata/dbase5/full/. Copied here as the direct
oracle fixture: 3 tags (CUST_ID character, OLDBALANCE numeric,
INVOICE_NO character) over 5 records, each a single-leaf tree.

Confirms the file header, tag directory, tag header, and — the
one undocumented detail found empirically — that a leaf entry's
stride is the tag header's ItemSize field (align4(4+KeySize)),
not simply 4+KeySize.
