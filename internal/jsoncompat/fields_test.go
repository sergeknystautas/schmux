package jsoncompat

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestFieldsPreserveUnknownAndKnownEmbedded(t *testing.T) {
	type embedded struct {
		Known string `json:"known"`
	}
	type outer struct {
		embedded
		Value string `json:"value,omitempty"`
	}
	var got outer
	extras, err := UnmarshalUnknown([]byte(`{"known":"k","value":"v","future":{"keep":[1]}}`), &got, outer{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Known != "k" || got.Value != "v" {
		t.Fatalf("decoded value = %+v", got)
	}
	_, knownIsExtra := extras["known"]
	_, valueIsExtra := extras["value"]
	if knownIsExtra || valueIsExtra {
		t.Fatalf("known fields treated as unknown: %+v", extras)
	}
	var future struct {
		Keep []int `json:"keep"`
	}
	if err := json.Unmarshal(extras["future"], &future); err != nil || len(future.Keep) != 1 {
		t.Fatalf("future field = %s (err %v)", extras["future"], err)
	}

	data, err := MarshalIndentWithFields(got, extras, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var round struct {
		Known  string          `json:"known"`
		Value  string          `json:"value"`
		Future json.RawMessage `json:"future"`
	}
	if err := json.Unmarshal(data, &round); err != nil || round.Known != "k" || round.Value != "v" || !bytes.Contains(round.Future, []byte("keep")) {
		t.Fatalf("round trip = %s (err %v)", data, err)
	}
}
