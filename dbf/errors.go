package dbf

import "errors"

var (
	// ErrEOF indicates a record number beyond the end of the table.
	ErrEOF = errors.New("end of file")

	// ErrInvalidRecord indicates a record that cannot be decoded.
	ErrInvalidRecord = errors.New("invalid record")

	// ErrUnsupported indicates a feature this package does not implement.
	ErrUnsupported = errors.New("unsupported feature")

	// ErrNumericOverflow indicates a numeric value too wide for its
	// field, or a field holding Clipper's asterisk overflow marker.
	// This package reports overflow rather than filling with
	// asterisks as Clipper does, which is a deliberate divergence:
	// silently storing an unrecoverable value is worse than failing.
	ErrNumericOverflow = errors.New("numeric overflow")
)
