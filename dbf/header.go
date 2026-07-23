package dbf

import (
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

const (
	fileHeaderSize = 32

	headerTerminator = 0x0D
	fileTerminator   = 0x1A

	dbfVersion = 0x03 // dBASE III+
)

// headerInfo carries the physical bookkeeping read from a DBF header.
//
// These values belong to the implementation and never surface in the
// public API.
type headerInfo struct {
	recordCount uint32
	headerSize  uint16
	recordSize  uint16
}

// readHeader reads the 32-byte DBF file header.
//
// It returns the logical header metadata together with the physical
// bookkeeping stored in the file.
func readHeader(r io.Reader) (Header, headerInfo, error) {
	var raw [fileHeaderSize]byte

	if _, err := io.ReadFull(r, raw[:]); err != nil {
		return Header{}, headerInfo{}, err
	}

	if raw[0] != dbfVersion {
		return Header{}, headerInfo{},
			fmt.Errorf("unsupported DBF version 0x%02X", raw[0])
	}

	header := Header{
		LastUpdate: decodeHeaderDate(
			raw[1],
			raw[2],
			raw[3],
		),
		CodePage: raw[29],
	}

	info := headerInfo{
		recordCount: binary.LittleEndian.Uint32(raw[4:8]),
		headerSize:  binary.LittleEndian.Uint16(raw[8:10]),
		recordSize:  binary.LittleEndian.Uint16(raw[10:12]),
	}

	return header, info, nil
}

// writeHeader writes the 32-byte DBF file header.
//
// headerSize and recordSize are passed explicitly rather than being
// recomputed from a schema: a file opened with a padded header keeps
// its original header size when the header is rewritten.
func writeHeader(
	w io.Writer,
	header Header,
	headerSize uint16,
	recordSize uint16,
	recordCount uint32,
) error {

	var raw [fileHeaderSize]byte

	raw[0] = dbfVersion

	encodeHeaderDate(header.LastUpdate, raw[1:4])

	binary.LittleEndian.PutUint32(
		raw[4:8],
		recordCount,
	)

	binary.LittleEndian.PutUint16(
		raw[8:10],
		headerSize,
	)

	binary.LittleEndian.PutUint16(
		raw[10:12],
		recordSize,
	)

	// bytes 12..13
	// Reserved.

	// byte 14
	// Incomplete transaction flag.
	// Always zero.

	// byte 15
	// Encryption flag.
	// Always zero.

	// bytes 16..27
	// Reserved.

	// byte 28
	// Production MDX flag.
	// Clipper NTX tables leave this zero.

	raw[29] = header.CodePage

	// bytes 30..31
	// Reserved.

	_, err := w.Write(raw[:])
	return err
}

func decodeHeaderDate(year, month, day byte) time.Time {
	if year == 0 && month == 0 && day == 0 {
		return time.Time{}
	}

	return time.Date(
		1900+int(year),
		time.Month(month),
		int(day),
		0,
		0,
		0,
		0,
		time.UTC,
	)
}

func encodeHeaderDate(t time.Time, dst []byte) {
	if len(dst) != 3 {
		panic("encodeHeaderDate: destination must be exactly 3 bytes")
	}

	if t.IsZero() {
		clear(dst)
		return
	}

	year := t.Year()

	if year < 1900 || year > 2155 {
		panic(fmt.Sprintf("year %d out of DBF range", year))
	}

	dst[0] = byte(year - 1900)
	dst[1] = byte(t.Month())
	dst[2] = byte(t.Day())
}
