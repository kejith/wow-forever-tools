package sv

import "math/bits"

// luaOrder puts the fields of a table built by a constructor into the order Lua 5.4 iterates them (lua_next,
// which lupa's keys() and so the Python scripts follow): the array part (the positional fields 1..na) first,
// then the hash part in the order of its nodes. Where a key lands in the hash part follows from the
// constructor: the parser sizes the part for the nh record fields (2^ceil(log2 nh) nodes), and every record
// field is inserted in the order of the file (luaH_newkey with its collision rule).
//
// Integer and boolean keys hash the same in every Lua process; string keys do not (Lua 5.4 seeds their hash
// per process), so a table with a string key, or a non-integral float key, keeps the order of the file: the
// Python order is random there anyway.
func (t *Table) luaOrder(keyed []any, na int64) {
	if len(keyed) == 0 {
		return
	}
	for _, k := range keyed {
		switch k.(type) {
		case int64, bool:
		default:
			return
		}
	}
	size := 1
	if n := len(keyed); n > 1 {
		size = 1 << bits.Len(uint(n-1))
	}
	type node struct {
		key  any
		used bool
		next int
	}
	nodes := make([]node, size)
	at := map[any]int{}
	mod := uint64((size - 1) | 1)
	mainPos := func(k any) int {
		switch x := k.(type) {
		case int64:
			return int(uint64(x) % mod)
		case bool:
			if x {
				return 1 & (size - 1)
			}
			return 0
		}
		return 0
	}
	lastFree := size
	for _, k := range keyed {
		if i, ok := k.(int64); ok && i >= 1 && i <= na {
			continue // an array slot
		}
		if _, ok := at[k]; ok {
			continue // set again: the node keeps its place
		}
		mp := mainPos(k)
		if nodes[mp].used {
			f := -1
			for lastFree > 0 {
				lastFree--
				if !nodes[lastFree].used {
					f = lastFree
					break
				}
			}
			if f < 0 {
				return // the part is full: Lua would rehash; cannot happen for a constructor
			}
			other := mainPos(nodes[mp].key)
			if other != mp {
				// The colliding node is out of its main position: move it to the free node.
				for other+nodes[other].next != mp {
					other += nodes[other].next
				}
				nodes[other].next = f - other
				nodes[f] = nodes[mp]
				at[nodes[f].key] = f
				if nodes[mp].next != 0 {
					nodes[f].next += mp - f
					nodes[mp].next = 0
				}
			} else {
				// The colliding node is in its main position: the new key goes to the free node.
				if nodes[mp].next != 0 {
					nodes[f].next = mp + nodes[mp].next - f
				} else {
					nodes[f].next = 0
				}
				nodes[mp].next = f - mp
				mp = f
			}
		}
		nodes[mp].key, nodes[mp].used = k, true
		at[k] = mp
	}
	var order []any
	for i := int64(1); i <= na; i++ {
		if t.Has(i) {
			order = append(order, i)
		}
	}
	for _, n := range nodes {
		if n.used && t.Has(n.key) {
			order = append(order, n.key)
		}
	}
	if len(order) != len(t.keys) {
		return // a key the simulation does not know (a positional and a record field for one key): keep the file
	}
	vals := make([]any, len(order))
	for i, k := range order {
		vals[i] = t.Get(k)
	}
	t.keys, t.vals = order, vals
	for i, k := range order {
		t.index[k] = i
	}
}
