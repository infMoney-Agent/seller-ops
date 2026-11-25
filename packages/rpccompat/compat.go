// Package rpccompat projects typed messages onto the existing JSON contract.
// Only unmodeled source fields, explicit nulls and exact timestamp spelling live
// in legacy_fields. Every RPC still has a concrete request and response schema.
package rpccompat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"strings"
)

func normalize(v any) (any, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var out any
	e = d.Decode(&out)
	return out, e
}
func legacyName(f protoreflect.FieldDescriptor) string {
	if strings.HasPrefix(string(f.ParentFile().Package()), "sellerops.auth.") {
		return f.JSONName()
	}
	return string(f.Name())
}
func enumText(v protoreflect.EnumValueDescriptor) string {
	n := string(v.Name())
	prefix := ""
	for i, c := range string(v.Parent().Name()) {
		if c >= 'A' && c <= 'Z' && i > 0 {
			prefix += "_"
		}
		prefix += strings.ToUpper(string(c))
	}
	return strings.ToLower(strings.TrimPrefix(n, prefix+"_"))
}
func prepareScalar(f protoreflect.FieldDescriptor, v any) (any, error) {
	if f.Kind() == protoreflect.EnumKind {
		if s, ok := v.(string); ok {
			vs := f.Enum().Values()
			for i := 0; i < vs.Len(); i++ {
				if enumText(vs.Get(i)) == s || string(vs.Get(i).Name()) == s {
					return string(vs.Get(i).Name()), nil
				}
			}
			return nil, fmt.Errorf("unknown %s value", f.FullName())
		}
	}
	if f.Kind() == protoreflect.MessageKind {
		name := string(f.Message().FullName())
		if name == "google.protobuf.Struct" || name == "google.protobuf.Value" || name == "google.protobuf.Timestamp" {
			return v, nil
		}
		return prepare(f.Message(), v)
	}
	return v, nil
}
func prepare(md protoreflect.MessageDescriptor, v any) (any, error) {
	row, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("object required for %s", md.FullName())
	}
	out := map[string]any{}
	extra := map[string]any{}
	omitted := []string{}
	known := map[string]bool{}
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		name := string(f.Name())
		if name == "legacy_fields" || name == "omitted_fields" {
			continue
		}
		key := legacyName(f)
		known[key] = true
		value, present := row[key]
		if !present {
			omitted = append(omitted, key)
			continue
		}
		if value == nil {
			extra[key] = nil
			continue
		}
		var item any
		var err error
		if f.IsList() {
			values, ok := value.([]any)
			if !ok {
				return nil, fmt.Errorf("array required for %s", f.FullName())
			}
			items := []any{}
			for _, entry := range values {
				converted, e := prepareScalar(f, entry)
				if e != nil {
					return nil, e
				}
				items = append(items, converted)
			}
			item = items
		} else if f.IsMap() {
			item = value
		} else {
			item, err = prepareScalar(f, value)
		}
		if err != nil {
			return nil, err
		}
		out[f.JSONName()] = item
		if f.Kind() == protoreflect.MessageKind && string(f.Message().FullName()) == "google.protobuf.Timestamp" {
			extra[key] = value
		}
	}
	for k, v := range row {
		if !known[k] {
			extra[k] = v
		}
	}
	if fields.ByName("legacy_fields") != nil {
		out["legacyFields"] = extra
	}
	if fields.ByName("omitted_fields") != nil {
		out["omittedFields"] = omitted
	}
	return out, nil
}
func FromLegacy(dst proto.Message, value any) error {
	v, e := normalize(value)
	if e != nil {
		return e
	}
	v, e = prepare(dst.ProtoReflect().Descriptor(), v)
	if e != nil {
		return e
	}
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return protojson.Unmarshal(b, dst)
}
func restoreScalar(f protoreflect.FieldDescriptor, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	if f.Kind() == protoreflect.EnumKind {
		s, ok := v.(string)
		if !ok {
			return v, nil
		}
		vs := f.Enum().Values()
		for i := 0; i < vs.Len(); i++ {
			if string(vs.Get(i).Name()) == s {
				return enumText(vs.Get(i)), nil
			}
		}
		return nil, fmt.Errorf("unknown enum %s", f.FullName())
	}
	if f.Kind() == protoreflect.Int64Kind || f.Kind() == protoreflect.Uint64Kind || f.Kind() == protoreflect.Sint64Kind || f.Kind() == protoreflect.Fixed64Kind || f.Kind() == protoreflect.Sfixed64Kind {
		if s, ok := v.(string); ok {
			return json.Number(s), nil
		}
	}
	if f.Kind() == protoreflect.MessageKind {
		n := string(f.Message().FullName())
		if n == "google.protobuf.Struct" || n == "google.protobuf.Value" || n == "google.protobuf.Timestamp" {
			return v, nil
		}
		return restore(f.Message(), v)
	}
	return v, nil
}
func restore(md protoreflect.MessageDescriptor, v any) (any, error) {
	row, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("object required")
	}
	out := map[string]any{}
	fs := md.Fields()
	for i := 0; i < fs.Len(); i++ {
		f := fs.Get(i)
		if f.Name() == "legacy_fields" || f.Name() == "omitted_fields" {
			continue
		}
		value, present := row[f.JSONName()]
		if !present {
			continue
		}
		var result any
		var e error
		if f.IsList() {
			items := []any{}
			if arr, ok := value.([]any); ok {
				for _, entry := range arr {
					x, err := restoreScalar(f, entry)
					if err != nil {
						return nil, err
					}
					items = append(items, x)
				}
			}
			result = items
		} else if f.IsMap() {
			entries := map[string]any{}
			if values, ok := value.(map[string]any); ok {
				for key, v := range values {
					converted, err := restoreScalar(f.MapValue(), v)
					if err != nil {
						return nil, err
					}
					entries[key] = converted
				}
			}
			result = entries
		} else {
			result, e = restoreScalar(f, value)
		}
		if e != nil {
			return nil, e
		}
		out[legacyName(f)] = result
	}
	if extra, ok := row["legacyFields"].(map[string]any); ok {
		for k, v := range extra {
			out[k] = v
		}
	}
	if absent, ok := row["omittedFields"].([]any); ok {
		for _, key := range absent {
			delete(out, fmt.Sprint(key))
		}
	}
	return out, nil
}
func Legacy(src proto.Message) (map[string]any, error) {
	b, e := (protojson.MarshalOptions{EmitUnpopulated: true}).Marshal(src)
	if e != nil {
		return nil, e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var raw any
	if e = d.Decode(&raw); e != nil {
		return nil, e
	}
	if src.ProtoReflect().Descriptor().FullName() == "google.protobuf.Struct" {
		return raw.(map[string]any), nil
	}
	v, e := restore(src.ProtoReflect().Descriptor(), raw)
	if e != nil {
		return nil, e
	}
	return v.(map[string]any), nil
}
