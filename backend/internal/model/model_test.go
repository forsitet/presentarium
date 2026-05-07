package model_test

import (
	"encoding/json"
	"testing"

	"presentarium/internal/model"
)

func TestOptionList_Value_Nil(t *testing.T) {
	var ol model.OptionList
	v, err := ol.Value()
	if err != nil {
		t.Fatalf("Value() error = %v", err)
	}
	if v != nil {
		t.Errorf("expected nil driver.Value for nil OptionList, got %v", v)
	}
}

func TestOptionList_Value_Roundtrip(t *testing.T) {
	original := model.OptionList{
		{Text: "Yes", IsCorrect: true},
		{Text: "No", IsCorrect: false, ImageURL: "http://example.com/img.png"},
	}
	v, err := original.Value()
	if err != nil {
		t.Fatalf("Value() error = %v", err)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("expected string from Value, got %T", v)
	}

	var dec model.OptionList
	if err := dec.Scan(s); err != nil {
		t.Fatalf("Scan from string error: %v", err)
	}
	if len(dec) != len(original) {
		t.Fatalf("len after roundtrip = %d, want %d", len(dec), len(original))
	}
	if dec[0].Text != "Yes" || !dec[0].IsCorrect {
		t.Errorf("first option mismatch: %+v", dec[0])
	}
	if dec[1].ImageURL != "http://example.com/img.png" {
		t.Errorf("ImageURL not preserved: %+v", dec[1])
	}
}

func TestOptionList_Scan_Bytes(t *testing.T) {
	raw := []byte(`[{"text":"a","is_correct":true}]`)
	var ol model.OptionList
	if err := ol.Scan(raw); err != nil {
		t.Fatalf("Scan([]byte) error: %v", err)
	}
	if len(ol) != 1 || ol[0].Text != "a" || !ol[0].IsCorrect {
		t.Errorf("unexpected decoded value: %+v", ol)
	}
}

func TestOptionList_Scan_Nil(t *testing.T) {
	ol := model.OptionList{{Text: "preset"}}
	if err := ol.Scan(nil); err != nil {
		t.Fatalf("Scan(nil) error: %v", err)
	}
	if ol != nil {
		t.Errorf("expected nil after Scan(nil), got %+v", ol)
	}
}

func TestOptionList_Scan_UnsupportedType(t *testing.T) {
	var ol model.OptionList
	if err := ol.Scan(42); err == nil {
		t.Error("expected error for unsupported type")
	}
}

func TestOptionList_Scan_InvalidJSON(t *testing.T) {
	var ol model.OptionList
	if err := ol.Scan("not json"); err == nil {
		t.Error("expected error for invalid JSON input")
	}
}

func TestOptionList_JSONMarshalRoundtrip(t *testing.T) {
	// Sanity: the exposed JSON tags should round-trip via encoding/json directly.
	src := model.OptionList{{Text: "hello", IsCorrect: true}}
	b, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got model.OptionList
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got[0].Text != "hello" || !got[0].IsCorrect {
		t.Errorf("roundtrip mismatch: %+v", got)
	}
}
