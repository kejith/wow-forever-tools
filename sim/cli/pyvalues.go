package cli

import (
	"reflect"

	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
)

// Python's view of the values the tools return (pyjson objects, lists, SavedVariables tables), for the
// reports that print them.

// pySpec is format(v, spec); the specs are literals of this package, so a bad one is a bug.
func pySpec(v any, spec string) string {
	s, err := pyfmt.Spec(v, spec)
	if err != nil {
		panic(err)
	}
	return s
}

// pyCut is s[:n] (code points).
func pyCut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// pySliceEnd is the end of Python's xs[:top] for a list of n.
func pySliceEnd(n, top int) int {
	if top < 0 {
		top += n
		if top < 0 {
			return 0
		}
		return top
	}
	if top > n {
		return n
	}
	return top
}

// pyGet is d.get(k) of an object (nil for anything else).
func pyGet(d any, k string) any {
	switch x := d.(type) {
	case *pyjson.Obj:
		if x != nil {
			return x.Get(k)
		}
	case *sv.Table:
		if x != nil {
			return x.Get(k)
		}
	case map[string]any:
		return x[k]
	}
	return nil
}

// pyList are the entries of a list (none for anything else).
func pyList(v any) []any {
	switch x := v.(type) {
	case nil:
		return nil
	case []any:
		return x
	case *sv.Table:
		if x == nil {
			return nil
		}
		return x.List()
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return nil
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out
}

// pyLen is len() of a list, an object or a table.
func pyLen(v any) int {
	switch x := v.(type) {
	case *pyjson.Obj:
		if x == nil {
			return 0
		}
		return len(x.Keys())
	case interface{ Len() int }:
		if rv := reflect.ValueOf(v); rv.Kind() == reflect.Pointer && rv.IsNil() {
			return 0
		}
		return x.Len()
	}
	return len(pyList(v))
}

// pyTruthy is bool(v).
func pyTruthy(v any) bool {
	switch x := v.(type) {
	case *pyjson.Obj:
		return x != nil && len(x.Keys()) > 0
	case []any, []string:
		return pyLen(x) > 0
	}
	return sv.Truthy(v)
}
