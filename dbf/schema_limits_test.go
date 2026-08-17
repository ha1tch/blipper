package dbf

import "testing"

// Clipper wrote production tables with more than 128 fields; an
// earlier invented MaxFields = 128 rejected them. Register item T-06.
// Evidence: CASH.DBF and CASH0198.DBF (159 fields), TERMINAL.DBF and
// TERM0198.DBF (156 fields), all version 0x03 dBASE III+.
func TestSchemaAcceptsMoreThan128Fields(t *testing.T) {
	for _, n := range []int{129, 156, 159} {
		schema := Schema{Fields: make([]Field, n)}
		for i := range schema.Fields {
			schema.Fields[i] = Field{
				Name:   fieldName(i),
				Type:   Character,
				Length: 4,
			}
		}

		if err := schema.Validate(); err != nil {
			t.Fatalf("%d fields rejected: %v", n, err)
		}
	}
}

func TestSchemaRejectsBeyondHeaderCapacity(t *testing.T) {
	schema := Schema{Fields: make([]Field, MaxFields+1)}
	for i := range schema.Fields {
		schema.Fields[i] = Field{Name: fieldName(i), Type: Character, Length: 1}
	}

	if err := schema.Validate(); err == nil {
		t.Fatal("expected rejection beyond the header's uint16 capacity")
	}
}

// RecordSize returns uint16 and would silently wrap; Validate must
// reject an oversize record before that can happen.
func TestSchemaRejectsOversizeRecord(t *testing.T) {
	// 300 fields x 250 bytes = 75000, past the uint16 record size.
	schema := Schema{Fields: make([]Field, 300)}
	for i := range schema.Fields {
		schema.Fields[i] = Field{Name: fieldName(i), Type: Character, Length: 250}
	}

	if err := schema.Validate(); err == nil {
		t.Fatal("expected rejection of a record larger than 65535 bytes")
	}
}

// fieldName produces distinct, valid field names: F0 .. F2046.
func fieldName(i int) string {
	const digits = "0123456789"

	name := []byte{'F'}
	if i == 0 {
		return "F0"
	}

	var rev []byte
	for i > 0 {
		rev = append(rev, digits[i%10])
		i /= 10
	}

	for j := len(rev) - 1; j >= 0; j-- {
		name = append(name, rev[j])
	}

	return string(name)
}
