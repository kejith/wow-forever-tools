package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
)

// node is a JSON value that keeps the order of an object's keys: the order is part of the bridge's answer
// (Python dicts keep insertion order, and the window shows some objects in that order).
type node struct {
	kind byte // 'o' object, 'a' array, 'n' number, 's' string, 'b' bool, 'z' null
	keys []string
	vals []*node
	num  float64
	raw  string // the number as written
	str  string
	b    bool
}

func parseJSON(data []byte) (*node, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	n, err := parseNode(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("more than one JSON value")
	}
	return n, nil
}

func parseNode(dec *json.Decoder) (*node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			n := &node{kind: 'o'}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				v, err := parseNode(dec)
				if err != nil {
					return nil, err
				}
				n.keys = append(n.keys, kt.(string))
				n.vals = append(n.vals, v)
			}
			_, err := dec.Token()
			return n, err
		case '[':
			n := &node{kind: 'a'}
			for dec.More() {
				v, err := parseNode(dec)
				if err != nil {
					return nil, err
				}
				n.vals = append(n.vals, v)
			}
			_, err := dec.Token()
			return n, err
		}
		return nil, fmt.Errorf("unexpected %v", t)
	case json.Number:
		f, err := strconv.ParseFloat(string(t), 64)
		return &node{kind: 'n', num: f, raw: string(t)}, err
	case string:
		return &node{kind: 's', str: t}, nil
	case bool:
		return &node{kind: 'b', b: t}, nil
	case nil:
		return &node{kind: 'z'}, nil
	}
	return nil, fmt.Errorf("unexpected token %v", tok)
}

func (n *node) get(key string) *node {
	if n == nil || n.kind != 'o' {
		return nil
	}
	for i, k := range n.keys {
		if k == key {
			return n.vals[i]
		}
	}
	return nil
}

// short is the value for a report line, cut to a readable length.
func (n *node) short() string {
	if n == nil {
		return "(missing)"
	}
	var s string
	switch n.kind {
	case 'o':
		s = fmt.Sprintf("{%d keys}", len(n.keys))
	case 'a':
		s = fmt.Sprintf("[%d items]", len(n.vals))
	case 'n':
		s = n.raw
	case 's':
		s = strconv.Quote(n.str)
	case 'b':
		s = strconv.FormatBool(n.b)
	default:
		s = "null"
	}
	return cut(s, 80)
}

// difference is one place where two answers disagree.
type difference struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"` // value, type, missing, extra, length, order
	Python string `json:"python"`
	Go     string `json:"go"`
}

// differ compares two answers. Numbers agree within a relative tolerance; paths that match ignore (or are in
// skip, the paths where two runs of the same implementation disagreed) are left out.
type differ struct {
	tol    float64
	ignore *regexp.Regexp
	skip   map[string]bool
	out    []difference
}

func (d *differ) ignored(path string) bool {
	return d.skip[path] || (d.ignore != nil && d.ignore.MatchString(path))
}

func (d *differ) add(path, kind string, a, b *node) {
	d.out = append(d.out, difference{Path: path, Kind: kind, Python: a.short(), Go: b.short()})
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func (d *differ) compare(path string, a, b *node) {
	if d.ignored(path) {
		return
	}
	if a.kind != b.kind {
		d.add(path, "type", a, b)
		return
	}
	switch a.kind {
	case 'o':
		inB := map[string]*node{}
		for i, k := range b.keys {
			inB[k] = b.vals[i]
		}
		var common []string
		for i, k := range a.keys {
			p := join(path, k)
			bv, ok := inB[k]
			if !ok {
				if !d.ignored(p) {
					d.add(p, "missing", a.vals[i], nil)
				}
				continue
			}
			common = append(common, k)
			d.compare(p, a.vals[i], bv)
		}
		inA := map[string]bool{}
		for _, k := range a.keys {
			inA[k] = true
		}
		var commonB []string
		for i, k := range b.keys {
			if !inA[k] {
				if p := join(path, k); !d.ignored(p) {
					d.add(p, "extra", nil, b.vals[i])
				}
				continue
			}
			commonB = append(commonB, k)
		}
		for i := range common {
			if common[i] != commonB[i] {
				d.out = append(d.out, difference{Path: path, Kind: "order",
					Python: fmt.Sprintf("%q first", common[i]), Go: fmt.Sprintf("%q first", commonB[i])})
				break
			}
		}
	case 'a':
		if len(a.vals) != len(b.vals) {
			d.out = append(d.out, difference{Path: path, Kind: "length",
				Python: fmt.Sprint(len(a.vals)), Go: fmt.Sprint(len(b.vals))})
		}
		for i := 0; i < len(a.vals) && i < len(b.vals); i++ {
			d.compare(fmt.Sprintf("%s[%d]", path, i), a.vals[i], b.vals[i])
		}
	case 'n':
		if diff := math.Abs(a.num - b.num); diff > d.tol*math.Max(1, math.Max(math.Abs(a.num), math.Abs(b.num))) {
			d.add(path, "value", a, b)
		}
	case 's':
		if a.str != b.str {
			d.add(path, "value", a, b)
		}
	case 'b':
		if a.b != b.b {
			d.add(path, "value", a, b)
		}
	}
}

// diffAnswers compares two answers and returns where they disagree.
func diffAnswers(a, b *node, tol float64, ignore *regexp.Regexp, skip map[string]bool) []difference {
	d := &differ{tol: tol, ignore: ignore, skip: skip}
	d.compare("", a, b)
	return d.out
}
