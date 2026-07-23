package dbf

import (
	"fmt"
	"strings"
	"time"
)

// NewRecord creates an empty record initialized according to the schema.
//
// Values are initialized with the zero value appropriate for each field.
func NewRecord(schema Schema) Record {
	values := make([]any, len(schema.Fields))

	for i, field := range schema.Fields {
		values[i] = zeroValue(field)
	}

	return Record{
		Values: values,
	}
}

// Get returns the value of a field by name.
//
// Field names are matched case-insensitively.
func (r Record) Get(schema Schema, name string) (any, error) {
	index, err := schemaFieldIndex(schema, name)
	if err != nil {
		return nil, err
	}

	return r.GetIndex(index)
}

// Set updates a field value by name.
//
// Field names are matched case-insensitively.
func (r *Record) Set(schema Schema, name string, value any) error {
	index, err := schemaFieldIndex(schema, name)
	if err != nil {
		return err
	}

	return r.SetIndex(schema, index, value)
}

// GetIndex returns the value of a field by index.
func (r Record) GetIndex(index int) (any, error) {
	if index < 0 || index >= len(r.Values) {
		return nil, fmt.Errorf("field index %d out of range", index)
	}

	return r.Values[index], nil
}

// SetIndex updates a field value by index.
func (r *Record) SetIndex(schema Schema, index int, value any) error {
	if index < 0 || index >= len(schema.Fields) {
		return fmt.Errorf("field index %d out of range", index)
	}

	if len(r.Values) != len(schema.Fields) {
		return fmt.Errorf("record does not match schema")
	}

	if !validValue(schema.Fields[index], value) {
		return fmt.Errorf(
			"invalid value %v for field %q",
			value,
			schema.Fields[index].Name,
		)
	}

	r.Values[index] = value

	return nil
}

func schemaFieldIndex(schema Schema, name string) (int, error) {
	target := strings.ToUpper(name)

	for i, field := range schema.Fields {
		if strings.ToUpper(field.Name) == target {
			return i, nil
		}
	}

	return -1, fmt.Errorf("field %q not found", name)
}

func zeroValue(f Field) any {
	switch f.Type {
	case Character:
		return ""

	case Numeric:
		if f.Decimals == 0 {
			return int64(0)
		}
		return float64(0)

	case Float:
		return float64(0)

	case Logical:
		return false

	case Date:
		return time.Time{}

	case Memo:
		return ""

	default:
		return nil
	}
}

func validValue(field Field, value any) bool {
	if value == nil {
		return true
	}

	switch field.Type {

	case Character:
		_, ok := value.(string)
		return ok

	case Numeric, Float:
		switch value.(type) {
		case int,
			int8,
			int16,
			int32,
			int64,
			uint,
			uint8,
			uint16,
			uint32,
			uint64,
			float32,
			float64:
			return true
		default:
			return false
		}

	case Logical:
		_, ok := value.(bool)
		return ok

	case Date:
		_, ok := value.(time.Time)
		return ok

	case Memo:
		_, ok := value.(string)
		return ok

	default:
		return false
	}
}
