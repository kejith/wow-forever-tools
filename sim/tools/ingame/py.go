package ingame

import (
	"math"
	"reflect"
	"strings"
	"unicode"

	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/internal/sv"
)

// The results of the other tools are *pyjson.Obj trees (and *sv.Table where they pass a snapshot table on);
// the tests feed plain decoded JSON (map[string]any). These helpers read all of them the way the Python
// script reads its dicts.

// get is d.get(k).
func get(d any, k string) any {
	switch x := d.(type) {
	case *pyjson.Obj:
		if x != nil {
			return x.Get(k)
		}
	case map[string]any:
		return x[k]
	case *sv.Table:
		if x != nil {
			return x.Get(k)
		}
	}
	return nil
}

// items is `v or []` iterated: the entries of a list.
func items(v any) []any {
	switch x := v.(type) {
	case nil:
		return nil
	case []any:
		return x
	case *sv.Table:
		if x == nil {
			return nil
		}
		return x.Iter()
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out
}

// truthy is Python's bool().
func truthy(v any) bool {
	switch x := v.(type) {
	case *pyjson.Obj:
		return x != nil && len(x.Keys()) > 0
	case map[string]any:
		return len(x) > 0
	case *intDict:
		return x != nil && len(x.keys) > 0
	case nil, bool, int64, float64, int, string, *sv.Table, sv.Null:
		return sv.Truthy(v)
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array || rv.Kind() == reflect.Map {
		return rv.Len() > 0
	}
	return true
}

// num is a number as a float (0 for anything else): `x or 0` before a comparison.
func num(v any) float64 {
	if f, ok := sv.Num(v); ok {
		return f
	}
	if b, ok := v.(bool); ok && b {
		return 1
	}
	return 0
}

// isNum: an int or a float, not a bool.
func isNum(v any) bool { _, ok := sv.Num(v); return ok }

// toInt is Python's int(): a float is cut toward zero, a numeric string parsed.
func toInt(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	case bool:
		if x {
			return 1
		}
	case string:
		n, _ := pyfmt.ParseInt(x)
		return n
	}
	return 0
}

// pyRound is round(x) for an int or a float: an int (half to even).
func pyRound(v any) int64 {
	if f, ok := v.(float64); ok {
		return pyfmt.RoundInt(f)
	}
	return toInt(v)
}

// isStr: v == "text" in Python is only true for a string.
func isStr(v any, text string) bool { s, ok := v.(string); return ok && s == text }

// or0 is `v or 0`.
func or0(v any) any {
	if truthy(v) {
		return v
	}
	return int64(0)
}

// spec is f"{v:<spec>}".
func spec(v any, s string) string {
	out, err := pyfmt.Spec(v, s)
	if err != nil {
		return pyfmt.Str(v)
	}
	return out
}

// isSpace is Python's str.isspace() for one character (\s in a str pattern): Go's unicode.IsSpace without the
// four separators \x1c to \x1f.
func isSpace(r rune) bool { return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f) }

// clean is ingame.clean: one line, and no "|" that starts a UI escape code.
func clean(v any) string {
	text := ""
	if truthy(v) {
		text = pyfmt.Str(v)
	}
	var b strings.Builder
	inSpace := false
	for _, r := range text {
		if isSpace(r) {
			if !inSpace {
				b.WriteByte(' ')
			}
			inSpace = true
			continue
		}
		inSpace = false
		b.WriteRune(r)
	}
	return strings.TrimFunc(strings.ReplaceAll(b.String(), "|", "||"), isSpace)
}

// isWhole is float(v).is_integer().
func isWhole(v any) bool {
	f := num(v)
	return !math.IsInf(f, 0) && f == math.Trunc(f)
}
