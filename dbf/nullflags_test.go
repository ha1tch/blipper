package dbf

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNullFlagsOracle is the decisive test: real VFP 9 data,
// checked against every record, for two fields whose actual
// nullness is independently known (verified during derivation by
// comparing against blank field content — see nullflags.go).
func TestNullFlagsOracle(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "vfp", "ORDERS.DBF"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	tbl, err := Open(f)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	schema := tbl.Schema()
	shipRegionIdx, err := schemaFieldIndex(schema, "SHIPREGION")
	if err != nil {
		t.Fatalf("SHIPREGION not found: %v", err)
	}

	var nullCount, notNullCount int
	for recno := uint32(1); recno <= tbl.RecordCount(); recno++ {
		rec, err := tbl.Get(recno)
		if err != nil {
			continue // deleted or gap; skip
		}
		isNull, err := rec.IsNull(schema, "SHIPREGION")
		if err != nil {
			t.Fatalf("IsNull(SHIPREGION) record %d: %v", recno, err)
		}
		v, _ := rec.Get(schema, "SHIPREGION")
		content, _ := v.(string)
		blank := content == ""
		_ = shipRegionIdx

		if isNull {
			nullCount++
		} else {
			notNullCount++
		}
		// Every record where SHIPREGION is genuinely blank in the
		// real vendor data must report null=true, and vice versa.
		// This is the exact correlation used to derive the bit
		// ordering in the first place, now checked through the
		// public API rather than the derivation script.
		if isNull != blank {
			t.Errorf("record %d: IsNull=%v but content=%q (blank=%v) — mismatch",
				recno, isNull, content, blank)
		}
	}
	if nullCount == 0 || notNullCount == 0 {
		t.Fatalf("expected a mix of null and non-null SHIPREGION values, got %d null / %d not-null",
			nullCount, notNullCount)
	}
	t.Logf("SHIPREGION: %d null, %d not null, all matched", nullCount, notNullCount)
}

// TestNullFlagsSecondField cross-checks a second field
// (SHIPPEDDAT) in the same table, and a field in a different
// table (suppliers.dbf/REGION), to confirm the bit-ordering rule
// generalises rather than being a one-field coincidence.
func TestNullFlagsSecondField(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "vfp", "ORDERS.DBF"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	tbl, err := Open(f)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	schema := tbl.Schema()

	var nullCount int
	for recno := uint32(1); recno <= tbl.RecordCount(); recno++ {
		rec, err := tbl.Get(recno)
		if err != nil {
			continue
		}
		isNull, err := rec.IsNull(schema, "SHIPPEDDAT")
		if err != nil {
			t.Fatalf("IsNull(SHIPPEDDAT) record %d: %v", recno, err)
		}
		v, _ := rec.Get(schema, "SHIPPEDDAT")
		dt, _ := v.(interface{ IsZero() bool })
		blank := dt != nil && dt.IsZero()
		if isNull {
			nullCount++
		}
		if isNull != blank {
			t.Errorf("record %d: IsNull(SHIPPEDDAT)=%v but zero-date=%v", recno, isNull, blank)
		}
	}
	if nullCount == 0 {
		t.Fatal("expected some null SHIPPEDDAT values")
	}
}

func TestNullFlagsSuppliersTable(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "vfp", "SUPPLIERS.DBF"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	tbl, err := Open(f)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	schema := tbl.Schema()

	var nullCount, notNullCount int
	for recno := uint32(1); recno <= tbl.RecordCount(); recno++ {
		rec, err := tbl.Get(recno)
		if err != nil {
			continue
		}
		isNull, err := rec.IsNull(schema, "REGION")
		if err != nil {
			t.Fatalf("IsNull(REGION) record %d: %v", recno, err)
		}
		v, _ := rec.Get(schema, "REGION")
		content, _ := v.(string)
		blank := content == ""
		if isNull {
			nullCount++
		} else {
			notNullCount++
		}
		if isNull != blank {
			t.Errorf("record %d: IsNull(REGION)=%v but content=%q", recno, isNull, content)
		}
	}
	if nullCount == 0 || notNullCount == 0 {
		t.Fatalf("expected a mix, got %d null / %d not-null", nullCount, notNullCount)
	}
}

// TestIsNullRejectsNonNullableField guards the API surface.
func TestIsNullRejectsNonNullableField(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "vfp", "ORDERS.DBF"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	tbl, err := Open(f)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	rec, err := tbl.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// ORDERID / the primary key is typically not nullable.
	schema := tbl.Schema()
	nonNullable := ""
	for _, fld := range schema.Fields {
		if !fld.Nullable && !fld.SystemColumn {
			nonNullable = fld.Name
			break
		}
	}
	if nonNullable == "" {
		t.Skip("no non-nullable field found to test against")
	}
	if _, err := rec.IsNull(schema, nonNullable); err == nil {
		t.Errorf("IsNull(%s) succeeded on a non-nullable field", nonNullable)
	}
}
