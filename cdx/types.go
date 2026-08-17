// Package cdx reads and writes FoxPro 2-compatible compound index
// (.CDX) files. A CDX file holds one or more named indexes ("tags")
// in a single physical file, using 512-byte blocks and compact
// keys (prefix + trailing-blank compression).
//
// Structure: one file-level tag directory (itself a compact-index
// tree) whose leaves point at the root nodes of the individual tag
// trees. Each tag tree is a compact index over its own keys.
//
// Format reference: MS Learn aa975346 (compact index) and aa975347
// (compound index). Verified against a Clipper-generated CDX under
// the oracle described in docs/CLIPPER_ORACLE.md.
//
// This package handles MACHINE-collation CDX files only, which is
// the only collation Clipper 5.2e's DBFCDX.LIB can produce and
// what FoxPro 2.x wrote by default. Files with a non-MACHINE
// collation identifier are refused on Open with ErrUnsupportedCollation
// rather than opened and traversed in the wrong order.
package cdx

import "errors"

// BlockSize is the fixed block/page size of a CDX file. Every node,
// including the file header, occupies exactly this many bytes.
const BlockSize = 512

// Header options bit flags (offset 14 of the header).
const (
	optUnique      = 0x01
	optHasFOR      = 0x08
	optCompact     = 0x20
	optCompoundHdr = 0x40
)

// Node attribute bit flags (offset 0 of any node).
const (
	nodeIndex = 0x00 // interior node, not root
	nodeRoot  = 0x01 // root of the tree
	nodeLeaf  = 0x02 // leaf node (exterior); may also be root when tree is one page
)

// Ascending indicator values at header offset 502-503.
const (
	orderAscending  = 0x0000
	orderDescending = 0x0001
)

// Errors returned by Open and traversal operations.
var (
	// ErrNotCDX is returned when the file is too small or has no
	// compound-header flag set on its top-level header.
	ErrNotCDX = errors.New("cdx: not a compound index file")

	// ErrUnsupportedCollation is returned when a CDX carries a
	// collation identifier other than MACHINE. See package docs.
	ErrUnsupportedCollation = errors.New("cdx: non-MACHINE collation not supported")

	// ErrMalformedNode is returned when a node fails structural
	// checks (nkeys too large, pointers past EOF, negative bit
	// counts, etc.).
	ErrMalformedNode = errors.New("cdx: malformed node")

	// ErrTagNotFound is returned by Tag when the requested name
	// does not exist in the compound file.
	ErrTagNotFound = errors.New("cdx: tag not found")
)
