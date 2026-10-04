package observe

// Single-pass Value decoding. A Value nests arbitrarily (interface
// payloads, elements, fields, map entries), and encoding/json hands an
// UnmarshalJSON method the raw bytes of its whole subtree. When every
// level validated those bytes and then decoded them again, a nesting of
// depth d cost O(d) scans of O(d) bytes: a 124 KB, 2000-deep interface
// chain took 4.6 s. Here the outermost Value validates its bytes once and
// a token walk builds every nested Value directly, so the cost is linear
// in the input while each level keeps the strict rules:
//
//   - exact field names (aliases such as "Int" or "ſtr" are refused),
//     no unknown fields, no duplicate names;
//   - no explicit null for any field — an omitted field and a null one
//     used to decode to the same zero and so matched a real zero;
//   - JSON types per field (strings, booleans, integers without fraction
//     or exponent, arrays of objects);
//   - one canonical string payload (str or strBytes, not both), with
//     strBytes holding canonical base64 of invalid UTF-8.
//
// Structural rules that depend on the kind (which payload a kind may
// carry, container lengths, map key order) remain in checkValue.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"grossmith/internal/strictjson"
)

// valueFieldNames is the closed set of Value wire names.
var valueFieldNames = []string{
	"kind", "goType", "bool", "int", "uint", "str", "strBytes",
	"len", "elems", "fields", "entries", "dynType", "payload",
}

// subtreeScans counts whole-subtree validations so a test can prove each
// outermost Value is scanned once, independent of its nesting depth.
var subtreeScans atomic.Int64

func decodeValueDocument(data []byte) (Value, error) {
	// Syntax, trailing data, Unicode and duplicate names for the whole
	// subtree, checked once here and never again for nested values.
	subtreeScans.Add(1)
	if err := strictjson.Validate(data); err != nil {
		return Value{}, fmt.Errorf("observe: %w", err)
	}
	d := valueDecoder{json.NewDecoder(bytes.NewReader(data))}
	d.dec.UseNumber()
	v, err := d.value("value")
	if err != nil {
		return Value{}, err
	}
	return v, nil
}

type valueDecoder struct{ dec *json.Decoder }

// next reads the token for field, refusing an explicit null.
func (d valueDecoder) next(field string) (json.Token, error) {
	tok, err := d.dec.Token()
	if err != nil {
		return nil, err
	}
	if tok == nil {
		return nil, fmt.Errorf("observe: %s must not be null", field)
	}
	return tok, nil
}

func (d valueDecoder) open(field string, delim json.Delim, what string) error {
	tok, err := d.next(field)
	if err != nil {
		return err
	}
	if tok != delim {
		return fmt.Errorf("observe: %s must be %s", field, what)
	}
	return nil
}

// close consumes the delimiter json.Valid already established.
func (d valueDecoder) close() error {
	_, err := d.dec.Token()
	return err
}

func (d valueDecoder) key(seen map[string]bool, allowed []string) (string, error) {
	tok, err := d.dec.Token()
	if err != nil {
		return "", err
	}
	key := tok.(string) // json.Valid established the object grammar.
	if seen[key] {
		return "", fmt.Errorf("observe: duplicate JSON field %q", key)
	}
	seen[key] = true
	for _, name := range allowed {
		if key == name {
			return key, nil
		}
	}
	for _, name := range allowed {
		// EqualFold includes the Unicode aliases encoding/json folds.
		if strings.EqualFold(key, name) {
			return "", fmt.Errorf("observe: JSON field %q must use exact spelling %q", key, name)
		}
	}
	return "", fmt.Errorf("observe: json: unknown field %q", key)
}

func (d valueDecoder) string(field string) (string, error) {
	tok, err := d.next(field)
	if err != nil {
		return "", err
	}
	s, ok := tok.(string)
	if !ok {
		return "", fmt.Errorf("observe: %s must be a JSON string", field)
	}
	return s, nil
}

func (d valueDecoder) number(field string) (string, error) {
	tok, err := d.next(field)
	if err != nil {
		return "", err
	}
	n, ok := tok.(json.Number)
	if !ok {
		return "", fmt.Errorf("observe: %s must be a JSON integer", field)
	}
	return string(n), nil
}

func (d valueDecoder) value(field string) (Value, error) {
	if err := d.open(field, '{', "a JSON object"); err != nil {
		return Value{}, err
	}
	var v Value
	var strSeen, bytesSeen bool
	seen := map[string]bool{}
	for d.dec.More() {
		key, err := d.key(seen, valueFieldNames)
		if err != nil {
			return Value{}, err
		}
		switch key {
		case "kind":
			v.Kind, err = d.string(key)
		case "goType":
			v.GoType, err = d.string(key)
		case "dynType":
			v.DynType, err = d.string(key)
		case "bool":
			var tok json.Token
			if tok, err = d.next(key); err == nil {
				b, ok := tok.(bool)
				if !ok {
					return Value{}, fmt.Errorf("observe: bool must be a JSON boolean")
				}
				v.Bool = b
			}
		case "int":
			var n string
			if n, err = d.number(key); err == nil {
				if v.Int, err = strconv.ParseInt(n, 10, 64); err != nil {
					return Value{}, fmt.Errorf("observe: int %s is not an int64", n)
				}
			}
		case "uint":
			var n string
			if n, err = d.number(key); err == nil {
				if v.Uint, err = strconv.ParseUint(n, 10, 64); err != nil {
					return Value{}, fmt.Errorf("observe: uint %s is not a uint64", n)
				}
			}
		case "len":
			var n string
			if n, err = d.number(key); err == nil {
				l, perr := strconv.ParseInt(n, 10, strconv.IntSize)
				if perr != nil {
					return Value{}, fmt.Errorf("observe: len %s is not an int", n)
				}
				v.Len = int(l)
			}
		case "str":
			strSeen = true
			v.Str, err = d.string(key)
		case "strBytes":
			bytesSeen = true
			var text string
			if text, err = d.string(key); err == nil {
				v.StrBytes, err = canonicalBase64(key, text)
			}
		case "elems":
			if err = d.open(key, '[', "a JSON array"); err == nil {
				v.Elems = []Value{}
				for d.dec.More() {
					e, eerr := d.value("elems element")
					if eerr != nil {
						return Value{}, eerr
					}
					v.Elems = append(v.Elems, e)
				}
				err = d.close()
			}
		case "fields":
			v.Fields, err = d.fields()
		case "entries":
			v.Entries, err = d.entries()
		case "payload":
			var p Value
			if p, err = d.value(key); err == nil {
				v.Payload = &p
			}
		}
		if err != nil {
			return Value{}, err
		}
	}
	if err := d.close(); err != nil {
		return Value{}, err
	}
	if strSeen && bytesSeen {
		return Value{}, fmt.Errorf("observe: string value carries both str and strBytes payloads")
	}
	if strSeen || bytesSeen {
		if v.Kind != "string" {
			return Value{}, fmt.Errorf("observe: kind %q carries a string payload", v.Kind)
		}
		if bytesSeen && utf8.Valid(v.StrBytes) {
			return Value{}, fmt.Errorf("observe: strBytes requires invalid UTF-8 bytes")
		}
	}
	return v, nil
}

func (d valueDecoder) fields() ([]Field, error) {
	if err := d.open("fields", '[', "a JSON array"); err != nil {
		return nil, err
	}
	out := []Field{}
	for d.dec.More() {
		if err := d.open("fields element", '{', "a JSON object"); err != nil {
			return nil, err
		}
		var f Field
		seen := map[string]bool{}
		for d.dec.More() {
			key, err := d.key(seen, []string{"name", "value"})
			if err != nil {
				return nil, err
			}
			if key == "name" {
				f.Name, err = d.string("field name")
			} else {
				f.Value, err = d.value("field value")
			}
			if err != nil {
				return nil, err
			}
		}
		if err := d.close(); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, d.close()
}

func (d valueDecoder) entries() ([]Entry, error) {
	if err := d.open("entries", '[', "a JSON array"); err != nil {
		return nil, err
	}
	out := []Entry{}
	for d.dec.More() {
		if err := d.open("entries element", '{', "a JSON object"); err != nil {
			return nil, err
		}
		var e Entry
		seen := map[string]bool{}
		for d.dec.More() {
			key, err := d.key(seen, []string{"key", "value"})
			if err != nil {
				return nil, err
			}
			if key == "key" {
				e.Key, err = d.value("entry key")
			} else {
				e.Value, err = d.value("entry value")
			}
			if err != nil {
				return nil, err
			}
		}
		if err := d.close(); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, d.close()
}

// canonicalBase64 decodes standard padded base64 and requires the text to
// be exactly the encoding of its bytes. base64.StdEncoding alone accepts
// non-zero padding bits ("wh==" decodes like "wg==") and skips embedded
// CR/LF, so one byte string would otherwise have several spellings.
func canonicalBase64(field, text string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("observe: %s is not base64: %w", field, err)
	}
	if base64.StdEncoding.EncodeToString(b) != text {
		return nil, fmt.Errorf("observe: %s %q is not canonical base64 (want %q)", field, text, base64.StdEncoding.EncodeToString(b))
	}
	return b, nil
}
