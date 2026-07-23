package dbf

import (
	"fmt"
	"io"
	"time"
)

// Create writes a new, empty DBF file to rw and returns a Table
// positioned over it.
//
// The resulting file contains the header, the field descriptors, the
// header terminator and the end-of-file marker, and is readable by
// dBASE III+ and Clipper 5.x.
//
// Create does not close rw; the caller retains ownership of the
// underlying file.
func Create(rw io.ReadWriteSeeker, schema Schema) (*Table, error) {
	if err := schema.Validate(); err != nil {
		return nil, fmt.Errorf("invalid schema: %w", err)
	}

	header := Header{
		LastUpdate: time.Now(),
	}

	if _, err := rw.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	if err := writeHeader(rw, header, schema.HeaderSize(), schema.RecordSize(), 0); err != nil {
		return nil, fmt.Errorf("writing header: %w", err)
	}

	if err := writeFields(rw, schema.Fields); err != nil {
		return nil, fmt.Errorf("writing field descriptors: %w", err)
	}

	if _, err := rw.Write([]byte{fileTerminator}); err != nil {
		return nil, fmt.Errorf("writing EOF marker: %w", err)
	}

	return &Table{
		rw:          rw,
		header:      header,
		schema:      schema,
		recordCount: 0,
		recordStart: int64(schema.HeaderSize()),
	}, nil
}
