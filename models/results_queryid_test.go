package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestScanResultDataQueryIDLossless(t *testing.T) {
	for _, raw := range []string{"9007199254740993", "18446744073709551615", "7433501213056403345"} {
		for _, wire := range []string{raw, `"` + raw + `"`} {
			var r ScanResult
			if err := json.Unmarshal([]byte(`{"type":"sast","data":{"queryId":`+wire+`,"queryName":"Q"}}`), &r); err != nil {
				t.Fatal(err)
			}
			got, ok := r.ScanResultData.QueryIDString()
			if !ok || got != raw {
				t.Fatalf("wire %s: got %q ok=%v", wire, got, ok)
			}
			if r.ScanResultData.QueryName != "Q" {
				t.Fatal("other fields must still decode")
			}
			out, err := json.Marshal(r.ScanResultData)
			if err != nil || !strings.Contains(string(out), raw) {
				t.Fatalf("round trip %s: %s %v", wire, out, err)
			}
		}
	}
}

func TestScanResultDataQueryIDInexactOrMissing(t *testing.T) {
	if _, ok := (ScanResultData{QueryID: float64(9007199254740993)}).QueryIDString(); ok {
		t.Fatal("a float64 above 2^53 is not exact")
	}
	if s, ok := (ScanResultData{QueryID: float64(42)}).QueryIDString(); !ok || s != "42" {
		t.Fatalf("small float64: %q %v", s, ok)
	}
	if _, ok := (ScanResultData{}).QueryIDString(); ok {
		t.Fatal("missing id")
	}
	var d ScanResultData
	if err := json.Unmarshal([]byte(`{"queryId":1.5}`), &d); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.QueryIDString(); ok {
		t.Fatal("non-integer id is not an id")
	}
}

func TestScanResultDataUnmarshalNullIsNoop(t *testing.T) {
	d := ScanResultData{QueryName: "keep", QueryID: json.Number("7")}
	if err := d.UnmarshalJSON([]byte("null")); err != nil {
		t.Fatal(err)
	}
	if d.QueryName != "keep" || d.QueryID != json.Number("7") {
		t.Fatalf("null must leave the struct untouched: %+v", d)
	}
	var r ScanResult
	r.ScanResultData.QueryName = "keep"
	if err := json.Unmarshal([]byte(`{"data":null}`), &r); err != nil || r.ScanResultData.QueryName != "keep" {
		t.Fatalf("data:null: %+v %v", r.ScanResultData, err)
	}
}
