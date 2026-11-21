package pyfmt

import (
	"math"
	"testing"
)

// The expected strings in cases_test.go are what Python 3 prints for the same value and spec.
var negZero = math.Copysign(0, -1)

func TestSpecLikePython(t *testing.T) {
	if len(specCases) < 500 {
		t.Fatalf("only %d cases", len(specCases))
	}
	for _, c := range specCases {
		got, err := Spec(c.v, c.spec)
		if err != nil {
			t.Errorf("format(%#v, %q): %v", c.v, c.spec, err)
			continue
		}
		if got != c.want {
			t.Errorf("format(%#v, %q) = %q, Python says %q", c.v, c.spec, got, c.want)
		}
	}
}

func TestRoundLikePython(t *testing.T) {
	for _, c := range roundCases {
		if got := Round(c.x, c.n); got != c.want {
			t.Errorf("round(%v, %d) = %v, Python says %v", c.x, c.n, got, c.want)
		}
	}
	if RoundInt(2.5) != 2 || RoundInt(3.5) != 4 || RoundInt(-2.5) != -2 {
		t.Error("round() without digits rounds a half to even")
	}
}

func TestFormatLikePython(t *testing.T) {
	for _, c := range formatCases {
		if got := Format(c.text, c.vals); got != c.want {
			t.Errorf("%q.format(%v) = %q, Python says %q", c.text, c.vals, got, c.want)
		}
	}
	if got := Format("{missing} stays", map[string]any{"other": 1}); got != "{missing} stays" {
		t.Errorf("a field without a value stays: %q", got)
	}
}

func TestSumLikePython(t *testing.T) {
	for _, c := range sumCases {
		got := SumAny(c.xs...)
		if got != c.want {
			t.Errorf("sum(%v) = %v, Python says %v", c.xs, got, c.want)
		}
		floats := make([]float64, 0, len(c.xs))
		allFloat := true
		for _, x := range c.xs {
			f, ok := x.(float64)
			allFloat = allFloat && ok
			floats = append(floats, f)
		}
		if allFloat && Sum(floats...) != c.want {
			t.Errorf("Sum(%v) = %v, Python says %v", floats, Sum(floats...), c.want)
		}
	}
}

func TestFields(t *testing.T) {
	f := Fields("cap {cap:g}, {{not}} {target} {n:+.1f}")
	want := []Field{{"cap", "g"}, {"target", ""}, {"n", "+.1f"}}
	if len(f) != len(want) {
		t.Fatalf("got %v", f)
	}
	for i := range f {
		if f[i] != want[i] {
			t.Errorf("field %d: got %v, want %v", i, f[i], want[i])
		}
	}
}

func TestFsumAndMedianLikePython(t *testing.T) {
	for _, c := range fsumCases {
		if got := Fsum(c.xs); got != c.fsum {
			t.Errorf("fsum(%v) = %v, Python says %v", c.xs, got, c.fsum)
		}
		if got := Median(c.xs); got != c.median {
			t.Errorf("median(%v) = %v, Python says %v", c.xs, got, c.median)
		}
	}
}
