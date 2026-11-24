// Package sv reads the files the game writes: SavedVariables (Lua table constructors, in place of running
// them in lupa as scripts/upgrades.load_sv does) and Auctionator's price database (CBOR inside a Lua string,
// as scripts/writs.load_prices reads it).
//
// Values are nil, bool, int64 (a Lua integer), float64 (a Lua float), string and *Table. A table keeps its
// fields in the order of the file. Turned into JSON it follows upgrades.lua_to_py: a table whose keys are
// exactly 1..n is a list, every other table an object (an empty table too); package pyjson writes it.
package sv

import (
	"math"
	"sort"

	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
)

// Table is a Lua table with its fields in the order they were set.
type Table struct {
	keys  []any
	vals  []any
	index map[any]int
	list  bool // a CBOR array: a list even when empty (a Lua table is a list only with keys 1..n)
}

// NewTable is an empty table.
func NewTable() *Table { return &Table{index: map[any]int{}} }

// NewList is an empty list (Python's []), as a CBOR array decodes.
func NewList() *Table { return &Table{index: map[any]int{}, list: true} }

// Iter is what Python's `for x in value` walks: the values of a list, the keys of anything else.
func (t *Table) Iter() []any {
	if t.IsList() {
		return t.List()
	}
	return t.Keys()
}

func normKey(k any) any {
	switch x := k.(type) {
	case int:
		return int64(x)
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1<<63 {
			return int64(x) // Lua 5.4 stores a float key with an integer value as that integer
		}
	}
	return k
}

// Set sets a field; nil removes it. A field set again keeps its place.
func (t *Table) Set(k, v any) {
	k = normKey(k)
	if i, ok := t.index[k]; ok {
		if v == nil {
			t.remove(i)
			return
		}
		t.vals[i] = v
		return
	}
	if v == nil {
		return
	}
	t.index[k] = len(t.keys)
	t.keys = append(t.keys, k)
	t.vals = append(t.vals, v)
}

func (t *Table) remove(i int) {
	delete(t.index, t.keys[i])
	t.keys = append(t.keys[:i], t.keys[i+1:]...)
	t.vals = append(t.vals[:i], t.vals[i+1:]...)
	for j := i; j < len(t.keys); j++ {
		t.index[t.keys[j]] = j
	}
}

// Len is the number of fields.
func (t *Table) Len() int {
	if t == nil {
		return 0
	}
	return len(t.keys)
}

// Keys are the keys in order.
func (t *Table) Keys() []any {
	if t == nil {
		return nil
	}
	return t.keys
}

// Values are the values in the order of the keys.
func (t *Table) Values() []any {
	if t == nil {
		return nil
	}
	return t.vals
}

// Get is a field; nil if the table is nil or has no such field. Keys are strings or integers.
func (t *Table) Get(k any) any {
	if t == nil {
		return nil
	}
	if i, ok := t.index[normKey(k)]; ok {
		return t.vals[i]
	}
	return nil
}

// Has tells whether the field exists (Python's `k in d`).
func (t *Table) Has(k any) bool {
	if t == nil {
		return false
	}
	_, ok := t.index[normKey(k)]
	return ok
}

// Tab is a field that is a table, else nil.
func (t *Table) Tab(k any) *Table { x, _ := t.Get(k).(*Table); return x }

// Str is a field that is a string, else "".
func (t *Table) Str(k any) string { x, _ := t.Get(k).(string); return x }

// Num is a field that is a number, as a float.
func (t *Table) Num(k any) (float64, bool) { return Num(t.Get(k)) }

// IsList tells whether the keys are exactly 1..n (and there is at least one): lua_to_py makes such a
// table a list.
func (t *Table) IsList() bool {
	if t != nil && t.list {
		return true
	}
	if t.Len() == 0 {
		return false
	}
	seen := make([]bool, len(t.keys))
	for _, k := range t.keys {
		i, ok := k.(int64)
		if !ok || i < 1 || i > int64(len(t.keys)) || seen[i-1] {
			return false
		}
		seen[i-1] = true
	}
	return true
}

// List is the values of a list table in key order (1..n), or the values in file order for any other table.
func (t *Table) List() []any {
	if !t.IsList() {
		return t.Values()
	}
	out := make([]any, len(t.keys))
	for i, k := range t.keys {
		out[k.(int64)-1] = t.vals[i]
	}
	return out
}

// Tables is the values of List that are tables.
func (t *Table) Tables() []*Table {
	var out []*Table
	for _, v := range t.List() {
		if x, ok := v.(*Table); ok {
			out = append(out, x)
		}
	}
	return out
}

// SortedKeys is the keys with integers first (ascending), then the others by their text.
func (t *Table) SortedKeys() []any {
	keys := append([]any(nil), t.Keys()...)
	sort.SliceStable(keys, func(i, j int) bool {
		a, aInt := keys[i].(int64)
		b, bInt := keys[j].(int64)
		if aInt != bInt {
			return aInt
		}
		if aInt {
			return a < b
		}
		return pyfmt.Str(keys[i]) < pyfmt.Str(keys[j])
	})
	return keys
}

// Num is a number as a float; false for anything else (a bool is not a number here, unlike in Python).
func Num(v any) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case int:
		return float64(x), true
	}
	return 0, false
}

// Truthy is Python's bool() of a value read from a file: nil, false, 0, "" and an empty table are false.
func Truthy(v any) bool {
	switch x := v.(type) {
	case nil, Null:
		return false
	case bool:
		return x
	case int64:
		return x != 0
	case float64:
		return x != 0
	case int:
		return x != 0
	case string:
		return x != ""
	case *Table:
		return x.Len() > 0
	}
	return true
}

// KeyString is the text of a key in JSON, as json.dumps writes a Python dict key.
func KeyString(k any) string {
	switch x := k.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return pyfmt.Float(x)
	}
	return pyfmt.Str(k)
}
