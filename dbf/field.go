package dbf

import (
	"bytes"
	"fmt"
	"io"
)

const fieldDescriptorSize = 32

// readFields reads all field descriptors until the header terminator (0x0D).
func readFields(r io.Reader) ([]Field, error) {
	fields := make([]Field, 0, 16)

	for {
		field, done, err := readField(r)
		if err != nil {
			return nil, err
		}
		if done {
			return fields, nil
		}
		fields = append(fields, field)
	}
}

// writeFields writes all field descriptors followed by the header terminator.
func writeFields(w io.Writer, fields []Field) error {
	for _, field := range fields {
		if err := writeField(w, field); err != nil {
			return err
		}
	}

	_, err := w.Write([]byte{headerTerminator})
	return err
}

// readField reads a single 32-byte field descriptor.
//
// If the header terminator (0x0D) is encountered, done is returned as true.
func readField(r io.Reader) (field Field, done bool, err error) {
	var raw [fieldDescriptorSize]byte

	if _, err = io.ReadFull(r, raw[:1]); err != nil {
		return
	}

	if raw[0] == headerTerminator {
		done = true
		return
	}

	if _, err = io.ReadFull(r, raw[1:]); err != nil {
		return
	}

	field = Field{
		Name:     decodeFieldName(raw[:MaxFieldNameLength]),
		Type:     FieldType(raw[11]),
		Length:   raw[16],
		Decimals: raw[17],
	}

	if !isSupportedType(field.Type) {
		err = fmt.Errorf(
			"unsupported field type %q for field %q",
			rune(field.Type),
			field.Name,
		)
		return
	}

	return
}

// writeField writes a single 32-byte field descriptor.
func writeField(w io.Writer, field Field) error {
	var raw [fieldDescriptorSize]byte

	encodeFieldName(raw[:MaxFieldNameLength], field.Name)

	raw[11] = byte(field.Type)

	// Bytes 12..15 contain the field data address.
	//
	// dBASE III+ recomputes this value when opening the table.
	// Clipper ignores it on disk, so we leave it zero.

	raw[16] = field.Length
	raw[17] = field.Decimals

	// Bytes 18..31 are reserved and remain zero.

	_, err := w.Write(raw[:])
	return err
}

func decodeFieldName(raw []byte) string {
	if i := bytes.IndexByte(raw, 0); i >= 0 {
		raw = raw[:i]
	}

	return string(bytes.TrimRight(raw, " "))
}

func encodeFieldName(dst []byte, name string) {
	if len(dst) != MaxFieldNameLength {
		panic("encodeFieldName: invalid destination length")
	}

	clear(dst)

	copy(dst, name)
}