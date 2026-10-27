// Package jsoncompat preserves JSON fields that a decoder does not model.
package jsoncompat

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
)

// Fields is a set of unmodeled JSON object fields.
type Fields map[string]json.RawMessage

// UnmarshalUnknown decodes data into target and returns fields not owned by
// prototype. prototype can be any value of the same concrete type as target.
func UnmarshalUnknown(data []byte, target any, prototype any) (Fields, error) {
	if err := json.Unmarshal(data, target); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	return UnknownFields(fields, prototype), nil
}

// UnknownFields removes and returns fields not owned by prototype.
func UnknownFields(fields map[string]json.RawMessage, prototype any) Fields {
	known := KnownFields(reflect.TypeOf(prototype))
	var unknown Fields
	for name, value := range fields {
		if known[name] {
			continue
		}
		if unknown == nil {
			unknown = make(Fields)
		}
		unknown[name] = CloneRaw(value)
	}
	return unknown
}

// MarshalWithFields marshals value and adds unmodeled fields it does not own.
func MarshalWithFields(value any, extras Fields) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(extras) == 0 {
		return data, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for name, value := range extras {
		if _, exists := fields[name]; !exists {
			fields[name] = value
		}
	}
	return json.Marshal(fields)
}

// MarshalIndentWithFields is MarshalWithFields with indentation applied.
func MarshalIndentWithFields(value any, extras Fields, prefix, indent string) ([]byte, error) {
	data, err := MarshalWithFields(value, extras)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, data, prefix, indent); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// KnownFields returns the top-level JSON field names owned by t.
func KnownFields(t reflect.Type) map[string]bool {
	fields := make(map[string]bool, t.NumField())
	for i := range t.NumField() {
		field := t.Field(i)
		if field.Anonymous {
			for name := range KnownFields(field.Type) {
				fields[name] = true
			}
			continue
		}
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		fields[strings.Split(tag, ",")[0]] = true
	}
	return fields
}

// Clone returns an independent copy of fields.
func Clone(fields Fields) Fields {
	if fields == nil {
		return nil
	}
	out := make(Fields, len(fields))
	for name, value := range fields {
		out[name] = CloneRaw(value)
	}
	return out
}

// CloneRaw returns an independent copy of a raw JSON value.
func CloneRaw(value json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), value...)
}

// CloneRawMessages returns an independent copy of a raw JSON record list.
func CloneRawMessages(values []json.RawMessage) []json.RawMessage {
	if values == nil {
		return nil
	}
	out := make([]json.RawMessage, len(values))
	for i, value := range values {
		out[i] = CloneRaw(value)
	}
	return out
}
