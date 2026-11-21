package main

import (
	"regexp"
	"testing"
)

func mustParse(t *testing.T, s string) *node {
	t.Helper()
	n, err := parseJSON([]byte(s))
	if err != nil {
		t.Fatalf("parse %s: %v", s, err)
	}
	return n
}

func diffs(t *testing.T, a, b string, ignore string, skip ...string) []difference {
	t.Helper()
	var re *regexp.Regexp
	if ignore != "" {
		re = regexp.MustCompile(ignore)
	}
	sk := map[string]bool{}
	for _, s := range skip {
		sk[s] = true
	}
	return diffAnswers(mustParse(t, a), mustParse(t, b), 1e-9, re, sk)
}

func TestEqualAnswers(t *testing.T) {
	a := `{"a": 1, "b": [1, 2.5, "x", true, null], "c": {"d": "ü"}}`
	if d := diffs(t, a, a, ""); len(d) != 0 {
		t.Fatalf("want no differences, got %+v", d)
	}
	// 1 and 1.0 are the same number; a difference below the tolerance does not count.
	if d := diffs(t, `{"a": 1, "b": 0.30000000000000004}`, `{"a": 1.0, "b": 0.3}`, ""); len(d) != 0 {
		t.Fatalf("want no differences, got %+v", d)
	}
}

func TestDifferences(t *testing.T) {
	d := diffs(t, `{"a": 1, "b": [1, 2], "c": "x", "d": 1}`, `{"a": 1.5, "b": [1], "c": "y", "e": 1}`, "")
	want := map[string]string{"a": "value", "b": "length", "c": "value", "d": "missing", "e": "extra"}
	if len(d) != len(want) {
		t.Fatalf("want %d differences, got %+v", len(want), d)
	}
	for _, x := range d {
		if want[x.Path] != x.Kind {
			t.Errorf("%s: want %s, got %s", x.Path, want[x.Path], x.Kind)
		}
	}
	if d := diffs(t, `{"a": "1"}`, `{"a": 1}`, ""); len(d) != 1 || d[0].Kind != "type" {
		t.Fatalf("a string against a number is a type difference, got %+v", d)
	}
}

func TestKeyOrder(t *testing.T) {
	d := diffs(t, `{"x": {"a": 1, "b": 2}}`, `{"x": {"b": 2, "a": 1}}`, "")
	if len(d) != 1 || d[0].Kind != "order" || d[0].Path != "x" {
		t.Fatalf("want one order difference at x, got %+v", d)
	}
}

func TestIgnoredAndVolatilePaths(t *testing.T) {
	a := `{"generated": 1, "list": [{"age": 5, "n": 1}], "keep": 1}`
	b := `{"generated": 2, "list": [{"age": 6, "n": 1}], "keep": 2}`
	d := diffs(t, a, b, `^generated$`, "list[0].age")
	if len(d) != 1 || d[0].Path != "keep" {
		t.Fatalf("want only keep, got %+v", d)
	}
}

func TestPathsOfNestedValues(t *testing.T) {
	d := diffs(t, `{"a": [{"b": {"c": 1}}]}`, `{"a": [{"b": {"c": 2}}]}`, "")
	if len(d) != 1 || d[0].Path != "a[0].b.c" {
		t.Fatalf("want a[0].b.c, got %+v", d)
	}
}
