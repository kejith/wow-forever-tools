package cli

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kejith/wow-forever-tools/sim/internal/game"
	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
)

// The scripts printed their text from the dict compute() returned. The Go compute functions return the same
// data as a *pyjson.Obj whose values come in many Go types; pdData turns it into what json.loads would make
// of the script's --json output (nil, bool, string, int64, float64, []any, map[string]any), so that the text
// code can read it as the Python code did: a whole float stays a float, an int an int.

// pdData is v as json.loads(json.dumps(v)) would give it.
func pdData(v any) (any, error) {
	b, err := pyjson.Encode(v)
	if err != nil {
		return nil, err
	}
	p := &pdParser{s: string(b)}
	out, err := p.value()
	if err != nil {
		return nil, err
	}
	p.space()
	if p.i != len(p.s) {
		return nil, fmt.Errorf("json: trailing data at %d", p.i)
	}
	return out, nil
}

type pdParser struct {
	s string
	i int
}

func (p *pdParser) space() {
	for p.i < len(p.s) && strings.IndexByte(" \t\r\n", p.s[p.i]) >= 0 {
		p.i++
	}
}

func (p *pdParser) lit(word string) bool {
	if strings.HasPrefix(p.s[p.i:], word) {
		p.i += len(word)
		return true
	}
	return false
}

func (p *pdParser) value() (any, error) {
	p.space()
	if p.i >= len(p.s) {
		return nil, fmt.Errorf("json: unexpected end")
	}
	switch c := p.s[p.i]; {
	case c == '{':
		p.i++
		m := map[string]any{}
		p.space()
		if p.lit("}") {
			return m, nil
		}
		for {
			p.space()
			k, err := p.str()
			if err != nil {
				return nil, err
			}
			p.space()
			if !p.lit(":") {
				return nil, fmt.Errorf("json: ':' expected at %d", p.i)
			}
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			m[k] = v
			p.space()
			if p.lit("}") {
				return m, nil
			}
			if !p.lit(",") {
				return nil, fmt.Errorf("json: ',' expected at %d", p.i)
			}
		}
	case c == '[':
		p.i++
		list := []any{}
		p.space()
		if p.lit("]") {
			return list, nil
		}
		for {
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			list = append(list, v)
			p.space()
			if p.lit("]") {
				return list, nil
			}
			if !p.lit(",") {
				return nil, fmt.Errorf("json: ',' expected at %d", p.i)
			}
		}
	case c == '"':
		return p.str()
	case p.lit("null"):
		return nil, nil
	case p.lit("true"):
		return true, nil
	case p.lit("false"):
		return false, nil
	case p.lit("NaN"):
		return math.NaN(), nil
	case p.lit("Infinity"):
		return math.Inf(1), nil
	case p.lit("-Infinity"):
		return math.Inf(-1), nil
	}
	start := p.i
	for p.i < len(p.s) && strings.IndexByte("+-0123456789.eE", p.s[p.i]) >= 0 {
		p.i++
	}
	num := p.s[start:p.i]
	if num == "" {
		return nil, fmt.Errorf("json: unexpected %q at %d", p.s[p.i], p.i)
	}
	if !strings.ContainsAny(num, ".eE") {
		if n, err := strconv.ParseInt(num, 10, 64); err == nil {
			return n, nil
		}
	}
	return strconv.ParseFloat(num, 64)
}

func (p *pdParser) str() (string, error) {
	if !p.lit(`"`) {
		return "", fmt.Errorf("json: string expected at %d", p.i)
	}
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == '"':
			p.i++
			return b.String(), nil
		case c == '\\' && p.i+1 < len(p.s):
			e := p.s[p.i+1]
			p.i += 2
			switch e {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'u':
				if p.i+4 > len(p.s) {
					return "", fmt.Errorf("json: bad escape")
				}
				r, err := strconv.ParseUint(p.s[p.i:p.i+4], 16, 32)
				if err != nil {
					return "", err
				}
				p.i += 4
				if r >= 0xD800 && r < 0xDC00 && strings.HasPrefix(p.s[p.i:], `\u`) && p.i+6 <= len(p.s) {
					if lo, err := strconv.ParseUint(p.s[p.i+2:p.i+6], 16, 32); err == nil && lo >= 0xDC00 && lo < 0xE000 {
						p.i += 6
						r = 0x10000 + (r-0xD800)<<10 + (lo - 0xDC00)
					}
				}
				b.WriteRune(rune(r))
			default:
				b.WriteByte(e)
			}
		default:
			b.WriteByte(c)
			p.i++
		}
	}
	return "", fmt.Errorf("json: unterminated string")
}

// pdGet is d[k1][k2]…, read as Python's (d.get(k1) or {}).get(k2): nil where a key or a dict is missing.
func pdGet(d any, keys ...string) any {
	for _, k := range keys {
		m, ok := d.(map[string]any)
		if !ok {
			return nil
		}
		d = m[k]
	}
	return d
}

// pdList is a list value, nil for anything else (None).
func pdList(v any) []any {
	l, _ := v.([]any)
	return l
}

// pdTruthy is Python's bool() of a value.
func pdTruthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case int64:
		return x != 0
	case float64:
		return x != 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

// pdOr is Python's `v or alt`.
func pdOr(v, alt any) any {
	if pdTruthy(v) {
		return v
	}
	return alt
}

// pdStr is str() of a value.
func pdStr(v any) string { return pyfmt.Str(v) }

// pdSpec is format(v, spec); a spec Python would reject comes back as the plain value.
func pdSpec(v any, spec string) string {
	s, err := pyfmt.Spec(v, spec)
	if err != nil {
		return pyfmt.Str(v)
	}
	return s
}

// pdCut is s[:n] of a text: n code points.
func pdCut(v any, n int) string {
	s := pdStr(v)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// pdNum is a number value as a float (0 for anything else).
func pdNum(v any) float64 {
	switch x := v.(type) {
	case int64:
		return float64(x)
	case float64:
		return x
	case bool:
		if x {
			return 1
		}
	}
	return 0
}

// pdMoney is writs.fmt (and upgrades.money) of a copper amount.
func pdMoney(v any) string { return game.Money(pdNum(v)) }

// pdJoin is sep.join(list of texts).
func pdJoin(sep string, l []any) string {
	parts := make([]string, len(l))
	for i, x := range l {
		parts[i] = pdStr(x)
	}
	return strings.Join(parts, sep)
}
