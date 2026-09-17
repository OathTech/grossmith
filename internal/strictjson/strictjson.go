// Package strictjson reads one unambiguous JSON document. encoding/json
// otherwise accepts duplicate object names, silently replacing or merging
// earlier values. That behavior can hide contradictory observation evidence.
package strictjson

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Unmarshal rejects duplicate object names, unknown struct fields, and
// anything after the document except whitespace.
func Unmarshal(data []byte, dst any) error {
	if err := Validate(data); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// Validate checks syntax and duplicate names without prescribing a schema.
// It is useful when reading selected fields from an external protocol that
// explicitly permits unknown fields. Names are compared after unescaping.
func Validate(data []byte) error {
	// Besides rejecting trailing tokens, json.Valid enforces the standard
	// decoder's nesting limit before the recursive token walk begins.
	if !json.Valid(data) {
		return fmt.Errorf("invalid JSON document (malformed or trailing data)")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	// The structural walk must not round integers or reject large JSON
	// numbers merely because they do not fit in float64.
	dec.UseNumber()
	return value(dec)
}

func value(dec *json.Decoder) error {
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
			if err := value(dec); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err := value(dec); err != nil {
				return err
			}
		}
	}
	_, err = dec.Token() // matching close, established by json.Valid.
	return err
}
