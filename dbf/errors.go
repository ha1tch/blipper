package dbf

import "errors"

var (
	// ErrEOF indicates a record number beyond the end of the table.
	ErrEOF = errors.New("end of file")

	// ErrInvalidRecord indicates a record that cannot be decoded.
	ErrInvalidRecord = errors.New("invalid record")

	// ErrUnsupported indicates a feature this package does not implement.
	ErrUnsupported = errors.New("unsupported feature")
)
