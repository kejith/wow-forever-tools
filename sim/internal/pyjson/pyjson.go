// Package pyjson writes JSON the way the Python bridge does (json.dumps(obj, ensure_ascii=False)): ", " and
// ": " as separators, floats as Python's repr (2.0 stays 2.0), NaN and Infinity as Python writes them, objects
// in the order they were built.
//
// Values: nil, bool, ints, floats, string, Obj (an ordered object), []any and typed slices, *sv.Table (as
// lua_to_py makes it), sv.Null, and map[string]any (keys sorted: a Go map has no order). Anything else goes
// through encoding/json.
package pyjson

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"strconv"

	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
)

// Obj is an object whose keys keep the order they were added in, like a Python dict.
type Obj struct {
	keys []string
	vals map[string]any
}

// New is an empty object; pairs are key, value, key, value, …
func New(pairs ...any) *Obj {
	o := &Obj{vals: map[string]any{}}
	for i := 0; i+1 < len(pairs); i += 2 {
		o.Set(pairs[i].(string), pairs[i+1])
	}
	return o
}

// Set sets a key; a key set again keeps its place.
func (o *Obj) Set(k string, v any) *Obj {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
	return o
}

// Get is the value of a key.
func (o *Obj) Get(k string) any { return o.vals[k] }

// Keys are the keys in order.
func (o *Obj) Keys() []string { return o.keys }

// MarshalJSON lets encoding/json write an Obj too (compact, in order).
func (o *Obj) MarshalJSON() ([]byte, error) { return Encode(o) }

// Encode writes a value as Python's json.dumps(value, ensure_ascii=False).
func Encode(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := write(&b, v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func write(b *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil, sv.Null:
		b.WriteString("null")
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		writeString(b, x)
	case float64:
		writeFloat(b, x)
	case float32:
		writeFloat(b, float64(x))
	case int:
		b.WriteString(strconv.Itoa(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case int32:
		b.WriteString(strconv.FormatInt(int64(x), 10))
	case uint64:
		b.WriteString(strconv.FormatUint(x, 10))
	case *Obj:
		if x == nil {
			b.WriteString("null")
			return nil
		}
		b.WriteByte('{')
		for i, k := range x.keys {
			if i > 0 {
				b.WriteString(", ")
			}
			writeString(b, k)
			b.WriteString(": ")
			if err := write(b, x.vals[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	case *sv.Table:
		if x == nil {
			b.WriteString("null")
			return nil
		}
		if x.IsList() {
			return writeList(b, x.List())
		}
		b.WriteByte('{')
		for i, k := range x.Keys() {
			if i > 0 {
				b.WriteString(", ")
			}
			writeString(b, sv.KeyString(k))
			b.WriteString(": ")
			if err := write(b, x.Values()[i]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	case []any:
		return writeList(b, x)
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteString(", ")
			}
			writeString(b, k)
			b.WriteString(": ")
			if err := write(b, x[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
			if rv.Kind() == reflect.Slice && rv.IsNil() {
				b.WriteString("[]")
				return nil
			}
			items := make([]any, rv.Len())
			for i := range items {
				items[i] = rv.Index(i).Interface()
			}
			return writeList(b, items)
		}
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		b.Write(data)
	}
	return nil
}

func writeList(b *bytes.Buffer, items []any) error {
	b.WriteByte('[')
	for i, v := range items {
		if i > 0 {
			b.WriteString(", ")
		}
		if err := write(b, v); err != nil {
			return err
		}
	}
	b.WriteByte(']')
	return nil
}

func writeFloat(b *bytes.Buffer, f float64) {
	switch {
	case math.IsNaN(f):
		b.WriteString("NaN")
	case math.IsInf(f, 1):
		b.WriteString("Infinity")
	case math.IsInf(f, -1):
		b.WriteString("-Infinity")
	default:
		b.WriteString(pyfmt.Float(f))
	}
}

// writeString escapes like json.dumps with ensure_ascii=False: \" \\ \n \r \t \b \f and \u00XX for the other
// control characters; everything else as it is.
func writeString(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 {
				b.WriteString(`\u00`)
				b.WriteByte("0123456789abcdef"[r>>4])
				b.WriteByte("0123456789abcdef"[r&15])
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}
