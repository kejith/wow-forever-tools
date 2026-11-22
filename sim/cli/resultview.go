package cli

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/internal/sv"
)

// rview reads a computed result (pyjson objects, lists, numbers) the way the scripts read their dicts:
// r["key"], r.get("key"), truthiness, str().
type rview struct{ v any }

// at is r[key] (or r.get(key)): a missing key reads as None.
func (r rview) at(key string) rview {
	switch x := r.v.(type) {
	case *pyjson.Obj:
		if x != nil {
			return rview{x.Get(key)}
		}
	case *sv.Table:
		if x != nil {
			return rview{x.Get(key)}
		}
	case map[string]any:
		return rview{x[key]}
	}
	return rview{}
}

// has is `key in r`.
func (r rview) has(key string) bool {
	switch x := r.v.(type) {
	case *pyjson.Obj:
		if x != nil {
			for _, k := range x.Keys() {
				if k == key {
					return true
				}
			}
		}
	case map[string]any:
		_, ok := x[key]
		return ok
	case *sv.Table:
		return x != nil && x.Get(key) != nil
	}
	return false
}

// list is the items of a list (none for anything else).
func (r rview) list() []rview {
	switch x := r.v.(type) {
	case nil:
		return nil
	case []any:
		out := make([]rview, len(x))
		for i, v := range x {
			out[i] = rview{v}
		}
		return out
	case *sv.Table:
		if x == nil {
			return nil
		}
		items := x.List()
		out := make([]rview, len(items))
		for i, v := range items {
			out[i] = rview{v}
		}
		return out
	}
	rv := reflect.ValueOf(r.v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil
	}
	out := make([]rview, rv.Len())
	for i := range out {
		out[i] = rview{rv.Index(i).Interface()}
	}
	return out
}

// contains is `s in r` for a list of strings.
func (r rview) contains(s string) bool {
	for _, x := range r.list() {
		if x.v == s {
			return true
		}
	}
	return false
}

// none is `r is None`.
func (r rview) none() bool {
	switch x := r.v.(type) {
	case nil, sv.Null:
		return true
	case *pyjson.Obj:
		return x == nil
	case *float64:
		return x == nil
	}
	return false
}

// val is the value itself, with a pointer to a number read as the number (or None).
func (r rview) val() any {
	if p, ok := r.v.(*float64); ok {
		if p == nil {
			return nil
		}
		return *p
	}
	return r.v
}

// truthy is Python's bool(r).
func (r rview) truthy() bool {
	switch x := r.v.(type) {
	case *pyjson.Obj:
		return x != nil && len(x.Keys()) > 0
	case *float64:
		return x != nil && *x != 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	if rv := reflect.ValueOf(r.v); r.v != nil && rv.Kind() == reflect.Slice {
		return rv.Len() > 0
	}
	return sv.Truthy(r.v)
}

// num is the value as a number (0 for None).
func (r rview) num() float64 {
	switch x := r.val().(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case int32:
		return float64(x)
	case bool:
		if x {
			return 1
		}
	}
	return 0
}

// str is Python's str(r).
func (r rview) str() string { return pyfmt.Str(r.val()) }

// rvFormat is an f-string: pyfmt.Format with name/value pairs (rview values are unwrapped).
func rvFormat(text string, pairs ...any) string {
	values := make(map[string]any, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		v := pairs[i+1]
		if x, ok := v.(rview); ok {
			v = x.val()
		}
		values[pairs[i].(string)] = v
	}
	return pyfmt.Format(text, values)
}

// rvPairs turns rview values of i18n.T pairs into plain values.
func rvPairs(pairs ...any) []any {
	out := make([]any, len(pairs))
	for i, v := range pairs {
		if x, ok := v.(rview); ok {
			v = x.val()
		}
		out[i] = v
	}
	return out
}

// rvTable prints rows as the scripts did: every column as wide as its widest cell (code points, as len()
// counts), joined by two spaces, the end of each line stripped. right says per column whether it is
// right-aligned (rjust) instead of left-aligned (ljust).
func rvTable(out io.Writer, rows [][]string, right func(i int) bool) {
	if len(rows) == 0 {
		return
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i := range widths {
			if n := utf8.RuneCountInString(row[i]); n > widths[i] {
				widths[i] = n
			}
		}
	}
	for _, row := range rows {
		cells := make([]string, len(widths))
		for i, w := range widths {
			pad := strings.Repeat(" ", w-utf8.RuneCountInString(row[i]))
			if right != nil && right(i) {
				cells[i] = pad + row[i]
			} else {
				cells[i] = row[i] + pad
			}
		}
		fmt.Fprintln(out, strings.TrimRight(strings.Join(cells, "  "), " \t\n\r\v\f"))
	}
}
