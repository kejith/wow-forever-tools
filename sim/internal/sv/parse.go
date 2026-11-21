package sv

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// File is the globals a SavedVariables file sets, in order.
type File struct {
	names  []string
	values map[string]any
}

// Get is one global; nil if the file does not set it (lupa returns None then).
func (f *File) Get(name string) any {
	if f == nil {
		return nil
	}
	return f.values[name]
}

// Tab is one global that is a table, else nil.
func (f *File) Tab(name string) *Table { t, _ := f.Get(name).(*Table); return t }

// Names are the globals in the order the file sets them.
func (f *File) Names() []string { return f.names }

// ReadFile parses a SavedVariables file.
func ReadFile(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Load is one global of a SavedVariables file, like upgrades.load_sv(path, name).
func Load(path, name string) (any, error) {
	f, err := ReadFile(path)
	if err != nil {
		return nil, err
	}
	return f.Get(name), nil
}

// LoadTable is Load for a global that is a table; an empty table if the file does not set it.
func LoadTable(path, name string) (*Table, error) {
	f, err := ReadFile(path)
	if err != nil {
		return nil, err
	}
	if t := f.Tab(name); t != nil {
		return t, nil
	}
	return NewTable(), nil
}

// DecodeReplace is Python's bytes.decode("utf-8", "replace"): every invalid sequence becomes U+FFFD, a
// truncated sequence (its maximal valid start) as one replacement character.
func DecodeReplace(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var out strings.Builder
	out.Grow(len(b) + 8)
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r != utf8.RuneError || size > 1 {
			out.WriteRune(r)
			i += size
			continue
		}
		out.WriteRune(utf8.RuneError)
		i += maximalSubpart(b[i:])
	}
	return out.String()
}

// maximalSubpart is how many bytes of an invalid sequence one replacement character stands for: the lead
// byte and the continuation bytes that could still have made a valid character (Unicode, table 3-7).
func maximalSubpart(b []byte) int {
	c := b[0]
	var need int
	lo, hi := byte(0x80), byte(0xBF)
	switch {
	case c >= 0xC2 && c <= 0xDF:
		need = 1
	case c == 0xE0:
		need, lo = 2, 0xA0
	case c >= 0xE1 && c <= 0xEC, c == 0xEE, c == 0xEF:
		need = 2
	case c == 0xED:
		need, hi = 2, 0x9F
	case c == 0xF0:
		need, lo = 3, 0x90
	case c >= 0xF1 && c <= 0xF3:
		need = 3
	case c == 0xF4:
		need, hi = 3, 0x8F
	default:
		return 1
	}
	n := 1
	for j := 0; j < need && n < len(b); j++ {
		x := b[n]
		if j == 0 && (x < lo || x > hi) || j > 0 && (x < 0x80 || x > 0xBF) {
			break
		}
		n++
	}
	return n
}

// Parse reads the statements `Name = value` of a SavedVariables file. The source is decoded as upgrades.py
// reads it (UTF-8, invalid bytes replaced) before it is parsed.
func Parse(data []byte) (*File, error) {
	p := &parser{src: []byte(DecodeReplace(data))}
	f := &File{values: map[string]any{}}
	for {
		p.space()
		if p.pos >= len(p.src) {
			return f, nil
		}
		name := p.name()
		if name == "" {
			return nil, p.errorf("a global name expected")
		}
		p.space()
		if !p.eat('=') {
			return nil, p.errorf("'=' expected after %s", name)
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		if _, seen := f.values[name]; !seen {
			f.names = append(f.names, name)
		}
		f.values[name] = v
		p.space()
		p.eat(';')
	}
}

type parser struct {
	src []byte
	pos int
}

func (p *parser) errorf(format string, a ...any) error {
	line := 1 + strings.Count(string(p.src[:min(p.pos, len(p.src))]), "\n")
	return fmt.Errorf("line %d: %s", line, fmt.Sprintf(format, a...))
}

func (p *parser) peek() byte {
	if p.pos < len(p.src) {
		return p.src[p.pos]
	}
	return 0
}

func (p *parser) eat(c byte) bool {
	if p.peek() == c {
		p.pos++
		return true
	}
	return false
}

// space skips whitespace and comments.
func (p *parser) space() {
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			p.pos++
		case c == '-' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '-':
			p.pos += 2
			if level, ok := p.longBracket(); ok {
				p.skipLong(level)
				continue
			}
			for p.pos < len(p.src) && p.src[p.pos] != '\n' {
				p.pos++
			}
		default:
			return
		}
	}
}

// longBracket reads "[[" or "[==[" at the position and returns its level.
func (p *parser) longBracket() (int, bool) {
	if p.peek() != '[' {
		return 0, false
	}
	j := p.pos + 1
	for j < len(p.src) && p.src[j] == '=' {
		j++
	}
	if j < len(p.src) && p.src[j] == '[' {
		level := j - p.pos - 1
		p.pos = j + 1
		return level, true
	}
	return 0, false
}

// skipLong moves past the closing bracket of the given level and returns what was inside.
func (p *parser) skipLong(level int) string {
	closer := "]" + strings.Repeat("=", level) + "]"
	end := strings.Index(string(p.src[p.pos:]), closer)
	if end < 0 {
		s := string(p.src[p.pos:])
		p.pos = len(p.src)
		return s
	}
	s := string(p.src[p.pos : p.pos+end])
	p.pos += end + len(closer)
	return s
}

func isNameStart(c byte) bool { return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isNameChar(c byte) bool  { return isNameStart(c) || c >= '0' && c <= '9' }

func (p *parser) name() string {
	if !isNameStart(p.peek()) {
		return ""
	}
	start := p.pos
	for p.pos < len(p.src) && isNameChar(p.src[p.pos]) {
		p.pos++
	}
	return string(p.src[start:p.pos])
}

func (p *parser) value() (any, error) {
	p.space()
	c := p.peek()
	switch {
	case c == '{':
		return p.table()
	case c == '"' || c == '\'':
		return p.str()
	case c == '[':
		if level, ok := p.longBracket(); ok {
			s := p.skipLong(level)
			if strings.HasPrefix(s, "\r\n") {
				s = s[2:]
			} else if strings.HasPrefix(s, "\n") || strings.HasPrefix(s, "\r") {
				s = s[1:]
			}
			return s, nil
		}
	case c == '-' || c == '.' || c >= '0' && c <= '9':
		return p.number()
	case isNameStart(c):
		start := p.pos
		switch n := p.name(); n {
		case "nil":
			return nil, nil
		case "true":
			return true, nil
		case "false":
			return false, nil
		default:
			p.pos = start
			return nil, p.errorf("unsupported expression %q", n)
		}
	}
	return nil, p.errorf("a value expected")
}

func (p *parser) table() (*Table, error) {
	p.pos++ // {
	t := NewTable()
	var next int64 = 1
	var keyed []any // the record fields in file order, for the order Lua iterates them in
	done := func() (*Table, error) {
		t.luaOrder(keyed, next-1)
		return t, nil
	}
	for {
		p.space()
		if p.eat('}') {
			return done()
		}
		var key any
		positional := false
		switch c := p.peek(); {
		case c == '[' && !p.isLongString():
			p.pos++
			k, err := p.value()
			if err != nil {
				return nil, err
			}
			p.space()
			if !p.eat(']') {
				return nil, p.errorf("']' expected")
			}
			p.space()
			if !p.eat('=') {
				return nil, p.errorf("'=' expected")
			}
			if k == nil {
				return nil, p.errorf("a table key is nil")
			}
			if f, ok := k.(float64); ok && math.IsNaN(f) {
				return nil, p.errorf("a table key is NaN")
			}
			key = k
		case isNameStart(c):
			save := p.pos
			n := p.name()
			p.space()
			if p.eat('=') && n != "nil" && n != "true" && n != "false" {
				key = n
			} else {
				p.pos = save
				positional = true
			}
		default:
			positional = true
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		if positional {
			t.Set(next, v)
			next++
		} else {
			t.Set(key, v)
			keyed = append(keyed, normKey(key))
		}
		p.space()
		if !p.eat(',') && !p.eat(';') {
			p.space()
			if !p.eat('}') {
				return nil, p.errorf("',' or '}' expected")
			}
			return done()
		}
	}
}

func (p *parser) isLongString() bool {
	j := p.pos + 1
	for j < len(p.src) && p.src[j] == '=' {
		j++
	}
	return j < len(p.src) && p.src[j] == '['
}

// str reads a quoted string with Lua 5.4's escapes; the bytes it stands for are decoded as UTF-8.
func (p *parser) str() (string, error) {
	quote := p.src[p.pos]
	p.pos++
	var out []byte
	for {
		if p.pos >= len(p.src) {
			return "", p.errorf("unfinished string")
		}
		c := p.src[p.pos]
		switch {
		case c == quote:
			p.pos++
			return DecodeReplace(out), nil
		case c == '\n':
			return "", p.errorf("unfinished string")
		case c != '\\':
			out = append(out, c)
			p.pos++
			continue
		}
		p.pos++ // backslash
		if p.pos >= len(p.src) {
			return "", p.errorf("unfinished string")
		}
		e := p.src[p.pos]
		p.pos++
		switch e {
		case 'n':
			out = append(out, '\n')
		case 't':
			out = append(out, '\t')
		case 'r':
			out = append(out, '\r')
		case 'a':
			out = append(out, 7)
		case 'b':
			out = append(out, 8)
		case 'f':
			out = append(out, 12)
		case 'v':
			out = append(out, 11)
		case '\\', '"', '\'':
			out = append(out, e)
		case '\n', '\r':
			out = append(out, '\n')
			if p.pos < len(p.src) && (p.src[p.pos] == '\n' || p.src[p.pos] == '\r') && p.src[p.pos] != e {
				p.pos++
			}
		case 'x':
			if p.pos+2 > len(p.src) {
				return "", p.errorf("bad \\x escape")
			}
			n, err := strconv.ParseUint(string(p.src[p.pos:p.pos+2]), 16, 8)
			if err != nil {
				return "", p.errorf("bad \\x escape")
			}
			out = append(out, byte(n))
			p.pos += 2
		case 'z':
			for p.pos < len(p.src) && strings.IndexByte(" \t\n\r\f\v", p.src[p.pos]) >= 0 {
				p.pos++
			}
		case 'u':
			end := strings.IndexByte(string(p.src[p.pos:]), '}')
			if p.peek() != '{' || end < 0 {
				return "", p.errorf("bad \\u escape")
			}
			n, err := strconv.ParseUint(string(p.src[p.pos+1:p.pos+end]), 16, 32)
			if err != nil {
				return "", p.errorf("bad \\u escape")
			}
			out = utf8.AppendRune(out, rune(n))
			p.pos += end + 1
		default:
			if e >= '0' && e <= '9' {
				n := int(e - '0')
				for k := 0; k < 2 && p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9'; k++ {
					n = n*10 + int(p.src[p.pos]-'0')
					p.pos++
				}
				if n > 255 {
					return "", p.errorf("decimal escape too large")
				}
				out = append(out, byte(n))
				continue
			}
			return "", p.errorf("invalid escape \\%c", e)
		}
	}
}

// number reads a numeral with an optional minus: a Lua integer (int64) unless it has a point or an
// exponent or does not fit, then a float.
func (p *parser) number() (any, error) {
	neg := false
	for p.peek() == '-' {
		if p.pos+1 < len(p.src) && p.src[p.pos+1] == '-' {
			return nil, p.errorf("a value expected")
		}
		neg = !neg
		p.pos++
		p.space()
	}
	start := p.pos
	hex := p.peek() == '0' && p.pos+1 < len(p.src) && (p.src[p.pos+1] == 'x' || p.src[p.pos+1] == 'X')
	if hex {
		p.pos += 2
	}
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		isExp := (!hex && (c == 'e' || c == 'E')) || (hex && (c == 'p' || c == 'P'))
		if isExp && p.pos+1 < len(p.src) && (p.src[p.pos+1] == '+' || p.src[p.pos+1] == '-') {
			p.pos += 2
			continue
		}
		if c >= '0' && c <= '9' || c == '.' || isExp || hex && (c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			p.pos++
			continue
		}
		break
	}
	text := string(p.src[start:p.pos])
	if text == "" {
		return nil, p.errorf("a number expected")
	}
	var v any
	if hex {
		body := text[2:]
		if strings.ContainsAny(body, ".pP") {
			f, err := strconv.ParseFloat(text, 64)
			if err != nil {
				return nil, p.errorf("bad number %q", text)
			}
			v = f
		} else {
			n, err := strconv.ParseUint(body, 16, 64)
			if err != nil {
				return nil, p.errorf("bad number %q", text)
			}
			v = int64(n) // Lua wraps hex integers around
		}
	} else if strings.ContainsAny(text, ".eE") {
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, p.errorf("bad number %q", text)
		}
		v = f
	} else if n, err := strconv.ParseInt(text, 10, 64); err == nil {
		v = n
	} else {
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, p.errorf("bad number %q", text)
		}
		v = f
	}
	if neg {
		switch x := v.(type) {
		case int64:
			v = -x
		case float64:
			v = -x
		}
	}
	return v, nil
}
