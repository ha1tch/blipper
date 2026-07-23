package dbf

import (
	"fmt"
	"io"
)

// Open reads the header and field descriptors of an existing DBF file
// and returns a Table positioned over it.
//
// The on-disk record size must agree with the record size computed
// from the field descriptors. The on-disk header size is authoritative
// for locating record data: some writers pad the header beyond the
// minimum, and Open honours that.
//
// Open does not close rw; the caller retains ownership of the
// underlying file.
func Open(rw io.ReadWriteSeeker) (*Table, error) {
	if _, err := rw.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	header, info, err := readHeader(rw)
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}

	fields, err := readFields(rw)
	if err != nil {
		return nil, fmt.Errorf("reading field descriptors: %w", err)
	}

	schema := Schema{Fields: fields}

	if err := schema.Validate(); err != nil {
		return nil, fmt.Errorf("invalid schema: %w", err)
	}

	if got, want := info.recordSize, schema.RecordSize(); got != want {
		return nil, fmt.Errorf(
			"header record size %d disagrees with field descriptors (%d)",
			got,
			want,
		)
	}

	if info.headerSize < schema.HeaderSize() {
		return nil, fmt.Errorf(
			"header size %d too small for %d fields",
			info.headerSize,
			len(fields),
		)
	}

	return &Table{
		rw:          rw,
		header:      header,
		schema:      schema,
		recordCount: info.recordCount,
		recordStart: int64(info.headerSize),
	}, nil
}
