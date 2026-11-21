// Package pyfmt formats numbers and texts the way Python does, so the Go side of the bridge writes the same
// strings as the scripts it replaces: str() of a value, the format mini-language of str.format ("{n:.1f}",
// "{n:,}", "{share:.0%}", "{x:g}"), and round().
//
// It covers what the texts and scripts use: fill and alignment, sign, zero padding, width, the "," and "_"
// groupings, precision, and the types d, f, F, e, E, g, G, % and none. Not covered: "#", "z", "n", b/o/x/c.
package pyfmt

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Str is Python's str() of a value: ints in decimal, floats like repr(), True/False, None.
func Str(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case string:
		return x
	case bool:
		if x {
			return "True"
		}
		return "False"
	case float64:
		return Float(x)
	case float32:
		return Float(float64(x))
	}
	if i, ok := asInt(v); ok {
		return strconv.FormatInt(i, 10)
	}
	return fmt.Sprint(v)
}

func asInt(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int8:
		return int64(x), true
	case int16:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case uint:
		return int64(x), true
	case uint8:
		return int64(x), true
	case uint16:
		return int64(x), true
	case uint32:
		return int64(x), true
	case uint64:
		return int64(x), true
	}
	return 0, false
}

// Float is Python's repr() (and str()) of a float: the shortest digits that read back as the same number,
// with ".0" when it is whole, and an exponent below 1e-4 and from 1e16 on.
func Float(f float64) string {
	switch {
	case math.IsNaN(f):
		return "nan"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	}
	s := strconv.FormatFloat(f, 'e', -1, 64) // e.g. -1.2345e+06
	sign := ""
	if s[0] == '-' {
		sign, s = "-", s[1:]
	}
	e := strings.IndexByte(s, 'e')
	exp, _ := strconv.Atoi(s[e+1:])
	digits := strings.Replace(s[:e], ".", "", 1)
	decpt := exp + 1 // the value is 0.<digits> × 10^decpt
	if decpt <= -4 || decpt > 16 {
		m := digits[:1]
		if len(digits) > 1 {
			m += "." + digits[1:]
		}
		return sign + m + "e" + fmt.Sprintf("%+03d", exp)
	}
	switch {
	case decpt <= 0:
		return sign + "0." + strings.Repeat("0", -decpt) + digits
	case decpt >= len(digits):
		return sign + digits + strings.Repeat("0", decpt-len(digits)) + ".0"
	default:
		return sign + digits[:decpt] + "." + digits[decpt:]
	}
}

// Round is Python's round(x, n) for a float: correctly rounded to n decimals, an exact half to even.
func Round(x float64, n int) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	if n < 0 {
		p := math.Pow(10, float64(-n))
		return math.RoundToEven(x/p) * p
	}
	r, err := strconv.ParseFloat(strconv.FormatFloat(x, 'f', n, 64), 64)
	if err != nil {
		return x
	}
	return r
}

// Sum is Python's sum() of floats (start 0): since Python 3.12 a compensated (Neumaier) sum, which differs
// from adding one after the other in the last bits, and so in a value rounded at its half. Ints among floats
// are added as CPython 3.12 and 3.13 add them, see SumAny.
func Sum(xs ...float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	hi, lo := xs[0], 0.0
	for _, x := range xs[1:] {
		hi, lo = csAdd(hi, lo, x)
	}
	return csDouble(hi, lo)
}

func csAdd(hi, lo, x float64) (float64, float64) {
	t := hi + x
	if math.Abs(hi) >= math.Abs(x) {
		lo += (hi - t) + x
	} else {
		lo += (x - t) + hi
	}
	return t, lo
}

func csDouble(hi, lo float64) float64 {
	if lo != 0 && !math.IsInf(lo, 0) && !math.IsNaN(lo) {
		return hi + lo
	}
	return hi
}

// SumAny is Python's sum() of ints and floats as CPython 3.12 and 3.13 compute it: ints exactly until the
// first float, then floats compensated and ints added plainly (3.14 compensates the ints too).
func SumAny(xs ...any) any {
	var iResult int64
	i := 0
	for ; i < len(xs); i++ {
		n, ok := asInt(xs[i])
		if !ok {
			break
		}
		iResult += n
	}
	if i == len(xs) {
		return iResult
	}
	f, _ := xs[i].(float64)
	hi, lo := float64(iResult)+f, 0.0
	for _, x := range xs[i+1:] {
		if n, ok := asInt(x); ok {
			hi += float64(n)
			continue
		}
		v, _ := x.(float64)
		hi, lo = csAdd(hi, lo, v)
	}
	return csDouble(hi, lo)
}

// RoundInt is Python's round(x) without digits: the nearest integer, an exact half to even.
func RoundInt(x float64) int64 { return int64(math.RoundToEven(x)) }

type spec struct {
	fill      rune
	align     byte // '<', '>', '=', '^' or 0
	sign      byte // '+', '-', ' ' or 0
	width     int
	grouping  byte // ',', '_' or 0
	precision int  // -1: none
	typ       byte // 0: none
}

func parseSpec(s string) (spec, error) {
	sp := spec{fill: ' ', precision: -1}
	r := []rune(s)
	i := 0
	isAlign := func(c rune) bool { return c == '<' || c == '>' || c == '=' || c == '^' }
	if len(r) >= 2 && isAlign(r[1]) {
		sp.fill, sp.align, i = r[0], byte(r[1]), 2
	} else if len(r) >= 1 && isAlign(r[0]) {
		sp.align, i = byte(r[0]), 1
	}
	if i < len(r) && (r[i] == '+' || r[i] == '-' || r[i] == ' ') {
		sp.sign = byte(r[i])
		i++
	}
	if i < len(r) && r[i] == '0' {
		if sp.align == 0 {
			sp.fill, sp.align = '0', '='
		}
		i++
	}
	start := i
	for i < len(r) && r[i] >= '0' && r[i] <= '9' {
		i++
	}
	if i > start {
		sp.width, _ = strconv.Atoi(string(r[start:i]))
	}
	if i < len(r) && (r[i] == ',' || r[i] == '_') {
		sp.grouping = byte(r[i])
		i++
	}
	if i < len(r) && r[i] == '.' {
		i++
		start = i
		for i < len(r) && r[i] >= '0' && r[i] <= '9' {
			i++
		}
		if i == start {
			return sp, fmt.Errorf("format spec %q: precision without digits", s)
		}
		sp.precision, _ = strconv.Atoi(string(r[start:i]))
	}
	if i < len(r) {
		sp.typ = byte(r[i])
		i++
	}
	if i != len(r) {
		return sp, fmt.Errorf("format spec %q is not supported", s)
	}
	return sp, nil
}

// Spec formats one value with a format spec of Python's mini-language, as format(v, spec) does.
func Spec(v any, s string) (string, error) {
	if s == "" {
		return Str(v), nil
	}
	sp, err := parseSpec(s)
	if err != nil {
		return "", err
	}
	if b, ok := v.(bool); ok { // a bool formats as the int it is, unless the spec is empty
		if b {
			v = 1
		} else {
			v = 0
		}
	}
	var body string
	numeric := true
	switch x := v.(type) {
	case string:
		if sp.typ != 0 && sp.typ != 's' {
			return "", fmt.Errorf("format spec %q for a string", s)
		}
		body, numeric = x, false
		if sp.precision >= 0 && len([]rune(body)) > sp.precision {
			body = string([]rune(body)[:sp.precision])
		}
	case nil:
		body, numeric = "None", false
	default:
		if i, ok := asInt(v); ok {
			body, err = formatInt(i, sp)
		} else if f, ok := v.(float64); ok {
			body, err = formatFloat(f, sp)
		} else if f, ok := v.(float32); ok {
			body, err = formatFloat(float64(f), sp)
		} else {
			body, numeric = fmt.Sprint(v), false
		}
		if err != nil {
			return "", err
		}
	}
	return pad(body, sp, numeric), nil
}

func formatInt(i int64, sp spec) (string, error) {
	switch sp.typ {
	case 0, 'd':
		if sp.precision >= 0 {
			return "", fmt.Errorf("precision not allowed in an integer format")
		}
		neg := i < 0
		digits := strconv.FormatInt(i, 10)
		if neg {
			digits = digits[1:]
		}
		return withSign(group(digits, sp.grouping), neg, sp.sign), nil
	}
	return formatFloat(float64(i), sp)
}

func formatFloat(f float64, sp spec) (string, error) {
	neg := math.Signbit(f) && !math.IsNaN(f)
	a := math.Abs(f)
	var s string
	switch sp.typ {
	case 'f', 'F':
		s = strconv.FormatFloat(a, 'f', prec(sp, 6), 64)
	case 'e', 'E':
		s = strconv.FormatFloat(a, 'e', prec(sp, 6), 64)
	case 'g', 'G':
		p := prec(sp, 6)
		if p == 0 {
			p = 1
		}
		s = strconv.FormatFloat(a, 'g', p, 64)
	case '%':
		s = strconv.FormatFloat(a*100, 'f', prec(sp, 6), 64) + "%"
	case 0:
		if sp.precision < 0 {
			s = Float(a)
		} else {
			// No type with a precision: like g, but a whole number keeps no point and the exponent starts at
			// the precision (Python's "general format with at least one digit after the point" only applies
			// without a precision).
			p := sp.precision
			if p == 0 {
				p = 1
			}
			s = strconv.FormatFloat(a, 'g', p, 64)
		}
	default:
		return "", fmt.Errorf("unknown format type %q for a number", sp.typ)
	}
	if math.IsInf(f, 0) || (sp.typ == '%' && math.IsInf(a*100, 0)) {
		s = "inf"
		if sp.typ == '%' {
			s += "%"
		}
	} else if math.IsNaN(f) {
		s = "nan"
	}
	if sp.typ == 'E' || sp.typ == 'G' || sp.typ == 'F' {
		s = strings.ToUpper(s)
	}
	// Group the digits before the point or exponent.
	end := len(s)
	for i, c := range s {
		if c == '.' || c == 'e' || c == 'E' || c == '%' {
			end = i
			break
		}
	}
	return withSign(group(s[:end], sp.grouping)+s[end:], neg, sp.sign), nil
}

func prec(sp spec, def int) int {
	if sp.precision < 0 {
		return def
	}
	return sp.precision
}

func group(digits string, sep byte) string {
	if sep == 0 || len(digits) <= 3 {
		return digits
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return digits // inf, nan
		}
	}
	var b strings.Builder
	first := len(digits) % 3
	if first > 0 {
		b.WriteString(digits[:first])
	}
	for i := first; i < len(digits); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(sep)
		}
		b.WriteString(digits[i : i+3])
	}
	return b.String()
}

func withSign(s string, neg bool, sign byte) string {
	switch {
	case neg:
		return "-" + s
	case sign == '+':
		return "+" + s
	case sign == ' ':
		return " " + s
	}
	return s
}

func pad(body string, sp spec, numeric bool) string {
	n := len([]rune(body))
	if n >= sp.width {
		return body
	}
	fill := strings.Repeat(string(sp.fill), sp.width-n)
	align := sp.align
	if align == 0 {
		align = '<'
		if numeric {
			align = '>'
		}
	}
	switch align {
	case '>':
		return fill + body
	case '^':
		left := (sp.width - n) / 2
		return strings.Repeat(string(sp.fill), left) + body + strings.Repeat(string(sp.fill), sp.width-n-left)
	case '=':
		if numeric && body != "" && (body[0] == '-' || body[0] == '+' || body[0] == ' ') {
			return body[:1] + fill + body[1:]
		}
		return fill + body
	}
	return body + fill
}

// Field is one replacement field of a format string: its name and its spec ("cap" and "g" for "{cap:g}").
type Field struct{ Name, Spec string }

// Fields lists the replacement fields of a format string in order, as string.Formatter().parse does.
func Fields(text string) []Field {
	var out []Field
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == '{' {
			if i+1 < len(text) && text[i+1] == '{' {
				i++
				continue
			}
			end := strings.IndexByte(text[i:], '}')
			if end < 0 {
				return out
			}
			inner := text[i+1 : i+end]
			name, sp, _ := strings.Cut(inner, ":")
			out = append(out, Field{Name: name, Spec: sp})
			i += end
		} else if c == '}' && i+1 < len(text) && text[i+1] == '}' {
			i++
		}
	}
	return out
}

// Format is Python's text.format(**values): {name} and {name:spec} filled in, {{ and }} as braces. A field
// without a value stays as it is (Python would raise KeyError).
func Format(text string, values map[string]any) string {
	var b strings.Builder
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c == '{' && i+1 < len(text) && text[i+1] == '{':
			b.WriteByte('{')
			i++
		case c == '}' && i+1 < len(text) && text[i+1] == '}':
			b.WriteByte('}')
			i++
		case c == '{':
			end := strings.IndexByte(text[i:], '}')
			if end < 0 {
				b.WriteString(text[i:])
				return b.String()
			}
			inner := text[i+1 : i+end]
			name, sp, _ := strings.Cut(inner, ":")
			v, ok := values[name]
			if !ok {
				b.WriteString(text[i : i+end+1])
			} else if s, err := Spec(v, sp); err != nil {
				b.WriteString(text[i : i+end+1])
			} else {
				b.WriteString(s)
			}
			i += end
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// Fsum is math.fsum: the correctly rounded sum (Shewchuk partials, as CPython computes it). statistics.fmean
// is Fsum(xs) / len(xs).
func Fsum(xs []float64) float64 {
	var partials []float64
	for _, x := range xs {
		i := 0
		for _, y := range partials {
			if math.Abs(x) < math.Abs(y) {
				x, y = y, x
			}
			hi := x + y
			lo := y - (hi - x)
			if lo != 0 {
				partials[i] = lo
				i++
			}
			x = hi
		}
		partials = append(partials[:i], x)
	}
	n := len(partials)
	if n == 0 {
		return 0
	}
	n--
	hi, lo := partials[n], 0.0
	for n > 0 {
		x := hi
		n--
		y := partials[n]
		hi = x + y
		lo = y - (hi - x)
		if lo != 0 {
			break
		}
	}
	// Round half to even across the remaining partials, as CPython does.
	if n > 0 && ((lo < 0 && partials[n-1] < 0) || (lo > 0 && partials[n-1] > 0)) {
		y := lo * 2
		x := hi + y
		if y == x-hi {
			hi = x
		}
	}
	return hi
}

// Median is statistics.median: the middle of the sorted values, the mean of the two middle ones for an even
// count. xs must not be empty; it is not changed.
func Median(xs []float64) float64 {
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// ParseInt is Python's int() of a text: white space around it, an optional sign, digits with single
// underscores between them.
func ParseInt(s string) (int64, bool) {
	s = strings.TrimFunc(s, unicode.IsSpace)
	body := strings.TrimLeft(s, "+-")
	if len(s)-len(body) > 1 || body == "" || body[0] == '_' || body[len(body)-1] == '_' ||
		strings.Contains(body, "__") {
		return 0, false
	}
	n, err := strconv.ParseInt(s[:len(s)-len(body)]+strings.ReplaceAll(body, "_", ""), 10, 64)
	return n, err == nil
}

// ParseFloat is Python's float() of a text.
func ParseFloat(s string) (float64, bool) {
	s = strings.TrimFunc(s, unicode.IsSpace)
	low := strings.ToLower(strings.TrimLeft(s, "+-"))
	if strings.HasPrefix(low, "0x") || strings.HasPrefix(low, "0b") || strings.HasPrefix(low, "0o") || low == "" {
		return 0, false
	}
	switch low {
	case "inf", "infinity", "nan":
	default:
		if strings.ContainsAny(low, "abcdfghijklmnopqrstuvwxyz") || strings.Contains(s, "__") ||
			strings.HasPrefix(low, "_") || strings.HasSuffix(low, "_") {
			return 0, false
		}
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(s, "_", ""), 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return f, true
		}
		return 0, false
	}
	return f, true
}
