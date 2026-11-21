package combatlog

import (
	"math"
	"reflect"
	"testing"
)

func TestCSVLikePython(t *testing.T) {
	for _, c := range csvCases {
		var r csvReader
		var got [][]string
		for _, l := range c.lines {
			if row, ok := r.feed(l); ok {
				got = append(got, row)
			}
		}
		if row, ok := r.end(); ok {
			got = append(got, row)
		}
		if len(got) == 0 && len(c.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("csv %q = %q, Python says %q", c.lines, got, c.want)
		}
	}
}

func TestNumbersLikePython(t *testing.T) {
	for _, c := range intCases {
		if got, ok := pyInt(c.text); ok != c.ok || got != c.want {
			t.Errorf("int(%q) = %v %v, Python says %v %v", c.text, got, ok, c.want, c.ok)
		}
	}
	for _, c := range floatCases {
		got, ok := pyFloat(c.text)
		same := got == c.want || (math.IsNaN(got) && math.IsNaN(c.want))
		if ok != c.ok || (ok && !same) {
			t.Errorf("float(%q) = %v %v, Python says %v %v", c.text, got, ok, c.want, c.ok)
		}
	}
	for _, c := range flagCases {
		if got := flags(c.text); got != c.want {
			t.Errorf("int(%q, 16) = %v, Python says %v", c.text, got, c.want)
		}
	}
	for _, c := range dateCases {
		if got, ok := ordinalDay(c.y, c.m, c.d); ok != c.ok || got != c.want {
			t.Errorf("date(%d, %d, %d).toordinal() = %v %v, Python says %v %v", c.y, c.m, c.d, got, ok, c.want, c.ok)
		}
	}
}

func TestSplitLines(t *testing.T) {
	got := splitLines("a\r\nb\nc\rd")
	want := []string{"a\r\n", "b\n", "c\r", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitLines = %q, want %q", got, want)
	}
}
