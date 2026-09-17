// Package strictjson reads one unambiguous JSON document. encoding/json
// otherwise accepts duplicate object names, silently replacing or merging
// earlier values. That behavior can hide contradictory observation evidence.
package strictjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"unicode/utf8"
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
	if err := validUnicode(data); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	// The structural walk must not round integers or reject large JSON
	// numbers merely because they do not fit in float64.
	dec.UseNumber()
	return value(dec)
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
