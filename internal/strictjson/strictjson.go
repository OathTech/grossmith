// Package strictjson reads one unambiguous JSON document. encoding/json
// otherwise accepts duplicate object names, silently replacing or merging
// earlier values. That behavior can hide contradictory observation evidence.
package strictjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// Unmarshal rejects duplicate object names, unknown or incorrectly cased
// struct fields, fixed-size arrays of the wrong length, and anything after
// the document except whitespace.
func Unmarshal(data []byte, dst any) error {
	return unmarshal(data, dst, false)
}

// UnmarshalExtensible allows unknown fields in external protocols. Recognized
// struct fields still require exact spelling: an unknown "Status" must never
// overwrite a recognized "status". Open map keys remain case-sensitive.
func UnmarshalExtensible(data []byte, dst any) error {
	return unmarshal(data, dst, true)
}

func unmarshal(data []byte, dst any, extensible bool) error {
	if err := validateSyntax(data); err != nil {
		return err
	}
	t := reflect.TypeOf(dst)
	if t == nil || t.Kind() != reflect.Pointer || reflect.ValueOf(dst).IsNil() {
		return fmt.Errorf("JSON destination must be a non-nil pointer")
	}
	walk := json.NewDecoder(bytes.NewReader(data))
	walk.UseNumber()
	if err := value(walk, t.Elem(), extensible); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if !extensible {
		dec.DisallowUnknownFields()
	}
	return dec.Decode(dst)
}

// Validate checks syntax and duplicate names without prescribing a schema.
// It is useful when reading selected fields from an external protocol that
// explicitly permits unknown fields. Names are compared after unescaping.
func Validate(data []byte) error {
	if err := validateSyntax(data); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	// The structural walk must not round integers or reject large JSON
	// numbers merely because they do not fit in float64.
	dec.UseNumber()
	return value(dec, nil, true)
}

func validateSyntax(data []byte) error {
	// Besides rejecting trailing tokens, json.Valid enforces the standard
	// decoder's nesting limit before the recursive token walk begins.
	if !json.Valid(data) {
		return fmt.Errorf("invalid JSON document (malformed or trailing data)")
	}
	return validUnicode(data)
}

// encoding/json replaces invalid UTF-8 and unpaired UTF-16 escapes with
// U+FFFD. Evidence readers must reject those inputs instead of changing
// their string values during decoding. json.Valid already checked syntax.
func validUnicode(data []byte) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("invalid UTF-8 in JSON document")
	}
	for i := 0; i < len(data); i++ {
		if data[i] != '"' {
			continue
		}
		for i++; data[i] != '"'; i++ {
			if data[i] != '\\' {
				continue
			}
			i++
			if data[i] != 'u' {
				continue
			}
			n, _ := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
			i += 4
			if n >= 0xdc00 && n <= 0xdfff {
				return fmt.Errorf("unpaired Unicode surrogate at byte %d", i)
			}
			if n < 0xd800 || n > 0xdbff {
				continue
			}
			if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
				return fmt.Errorf("unpaired Unicode surrogate at byte %d", i)
			}
			low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return fmt.Errorf("unpaired Unicode surrogate at byte %d", i)
			}
			i += 6
		}
	}
	return nil
}

func value(dec *json.Decoder, typ reflect.Type, extensible bool) error {
	for typ != nil && typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		var fields map[string]reflect.Type
		if typ != nil && typ.Kind() == reflect.Struct {
			var err error
			fields, err = schemaFields(typ)
			if err != nil {
				return err
			}
		}
		seen := map[string]bool{}
		for dec.More() {
			tok, err := dec.Token()
			if err != nil {
				return err
			}
			key := tok.(string) // json.Valid established the object grammar.
			if seen[key] {
				return fmt.Errorf("duplicate JSON field %q at byte %d", key, dec.InputOffset())
			}
			seen[key] = true
			var child reflect.Type
			if fields != nil {
				child = fields[key]
				if child == nil {
					for name := range fields {
						// EqualFold includes the Unicode aliases recognized
						// by encoding/json (for example ſ/status and K/kind).
						if strings.EqualFold(key, name) {
							return fmt.Errorf("JSON field %q must use exact spelling %q", key, name)
						}
					}
					if !extensible {
						return fmt.Errorf("json: unknown field %q", key)
					}
				}
			} else if typ != nil && typ.Kind() == reflect.Map {
				child = typ.Elem()
			}
			if err := value(dec, child, extensible); err != nil {
				return err
			}
		}
	case '[':
		var child reflect.Type
		if typ != nil && (typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array) {
			child = typ.Elem()
		}
		n := 0
		for dec.More() {
			if err := value(dec, child, extensible); err != nil {
				return err
			}
			n++
		}
		// encoding/json truncates a long array and zero-fills a short one
		// when decoding into a Go fixed-size array; either would change the
		// recorded value, so the element count must match exactly.
		if typ != nil && typ.Kind() == reflect.Array && n != typ.Len() {
			return fmt.Errorf("JSON array has %d elements, want exactly %d for %s at byte %d", n, typ.Len(), typ, dec.InputOffset())
		}
	}
	_, err = dec.Token() // matching close, established by json.Valid.
	return err
}

var fieldCache sync.Map // reflect.Type -> immutable map[string]reflect.Type

// Evidence schemas use explicit fields. Refuse implicit embedded fields rather
// than approximate encoding/json's promotion and dominance rules. A named JSON
// field containing a struct is supported, including a tagged embedded field.
func schemaFields(t reflect.Type) (map[string]reflect.Type, error) {
	if fields, ok := fieldCache.Load(t); ok {
		return fields.(map[string]reflect.Type), nil
	}
	fields := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if f.Anonymous && name == "" {
			return nil, fmt.Errorf("JSON schema %s needs an explicit field name for embedded %s", t, f.Name)
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		if fields[name] != nil {
			return nil, fmt.Errorf("JSON schema %s has duplicate field %q", t, name)
		}
		fields[name] = f.Type
	}
	fieldCache.Store(t, fields)
	return fields, nil
}
