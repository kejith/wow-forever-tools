// Package combatlog is the Go side of scripts/combatlog.py: what the character really did, next to what the
// combat model says. It reads a combat log of the game, cuts it into fights and adds up per spell what the
// casts did; per fight length, enemies, damage per second, mana.
//
// The log names every player who was near; only the lines of the character who wrote it (the game marks
// them) and of that character's pets are read.
package combatlog

import (
	"math"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/kejith/wow-forever-tools/sim/internal/sv"
)

// line is one record of the log: when it happened (seconds) and its fields.
type line struct {
	t   float64
	row []string
}

// splitLines splits text as Python's universal newlines with newline="" do: a line ends after "\r\n", "\n"
// or "\r", and keeps its ending.
func splitLines(text string) []string {
	var out []string
	start := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\n':
			out = append(out, text[start:i+1])
			start = i + 1
		case '\r':
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			out = append(out, text[start:i+1])
			start = i + 1
		}
	}
	if start < len(text) {
		out = append(out, text[start:])
	}
	return out
}

// csvReader is Python's csv.reader with the default dialect (CPython's _csv.c), fed one line at a time. Each
// line ends its record, unless a quoted field goes on into the next line.
type csvReader struct {
	fields []string
	field  strings.Builder
	state  int
	any    bool // the field has a character (CPython's field_len)
}

const (
	startRecord = iota
	startField
	inField
	inQuoted
	quoteInQuoted
	eatCRNL
)

func (r *csvReader) save() {
	r.fields = append(r.fields, r.field.String())
	r.field.Reset()
	r.any = false
}

func (r *csvReader) add(c rune) {
	r.field.WriteRune(c)
	r.any = true
}

func (r *csvReader) char(c rune) {
	newline := c == '\n' || c == '\r'
	switch r.state {
	case startRecord:
		if newline {
			r.state = eatCRNL
			return
		}
		r.state = startField
		fallthrough
	case startField:
		switch {
		case newline:
			r.save()
			r.state = eatCRNL
		case c == '"':
			r.state = inQuoted
		case c == ',':
			r.save()
		default:
			r.add(c)
			r.state = inField
		}
	case inField:
		switch {
		case newline:
			r.save()
			r.state = eatCRNL
		case c == ',':
			r.save()
			r.state = startField
		default:
			r.add(c)
		}
	case inQuoted:
		if c == '"' {
			r.state = quoteInQuoted
		} else {
			r.add(c)
		}
	case quoteInQuoted:
		switch {
		case c == '"':
			r.add('"')
			r.state = inQuoted
		case c == ',':
			r.save()
			r.state = startField
		case newline:
			r.save()
			r.state = eatCRNL
		default:
			r.add(c)
			r.state = inField
		}
	case eatCRNL:
		if !newline {
			r.state = startRecord // CPython stops with an error here; a split line never has it
		}
	}
}

// feed reads one line; it returns the record when the line ends one.
func (r *csvReader) feed(text string) ([]string, bool) {
	for _, c := range text {
		r.char(c)
	}
	switch r.state {
	case startField, inField, quoteInQuoted:
		r.save()
	case inQuoted:
		return nil, false
	}
	r.state = startRecord
	return r.take(), true
}

// end is what the reader returns when the input ends inside a quoted field: the record so far.
func (r *csvReader) end() ([]string, bool) {
	if !r.any && r.state != inQuoted {
		return nil, false
	}
	r.save()
	r.state = startRecord
	return r.take(), true
}

func (r *csvReader) take() []string {
	out := r.fields
	if out == nil {
		out = []string{}
	}
	r.fields = nil
	return out
}

// pyInt is Python's int() of a text: white space around it, an optional sign, digits with single
// underscores between them.
func pyInt(s string) (int64, bool) {
	s = strings.TrimFunc(s, unicode.IsSpace)
	body := strings.TrimLeft(s, "+-")
	if len(s)-len(body) > 1 || body == "" || body[0] == '_' || body[len(body)-1] == '_' ||
		strings.Contains(body, "__") {
		return 0, false
	}
	n, err := strconv.ParseInt(s[:len(s)-len(body)]+strings.ReplaceAll(body, "_", ""), 10, 64)
	return n, err == nil
}

// pyFloat is Python's float() of a text.
func pyFloat(s string) (float64, bool) {
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

// num is combatlog.num: float(x), 0 for anything else.
func num(x string) float64 {
	f, _ := pyFloat(x)
	return f
}

// flags is combatlog.flags: int(x, 16), 0 for anything else.
func flags(x string) int64 {
	s := strings.TrimFunc(x, unicode.IsSpace)
	neg := false
	if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") {
		neg = s[0] == '-'
		s = s[1:]
	}
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		s = strings.TrimPrefix(s[2:], "_")
	}
	if s == "" || s[0] == '_' || s[len(s)-1] == '_' || strings.Contains(s, "__") {
		return 0
	}
	n, err := strconv.ParseInt(strings.ReplaceAll(s, "_", ""), 16, 64)
	if err != nil {
		return 0
	}
	if neg {
		return -n
	}
	return n
}

// ordinalDay is datetime.date(y, m, d).toordinal(); false for a date that does not exist.
func ordinalDay(y, m, d int64) (int64, bool) {
	if y < 1 || y > 9999 || m < 1 || m > 12 || d < 1 {
		return 0, false
	}
	t := time.Date(int(y), time.Month(m), int(d), 0, 0, 0, 0, time.UTC)
	if t.Day() != int(d) || int64(t.Month()) != m {
		return 0, false
	}
	epoch := time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
	return (t.Unix()-epoch.Unix())/86400 + 1, true // a Duration would overflow after 292 years
}

// readLog is combatlog.read: per line its time in seconds and its fields. A line is a time stamp, two spaces
// and comma-separated fields (names in quotes). The stamp's digits after the milliseconds are the time zone.
func readLog(path string) ([]line, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	days := map[string]float64{}
	stamp := ""
	var out []line
	emit := func(row []string) {
		day, clock, _ := strings.Cut(stamp, " ")
		if _, ok := days[day]; !ok {
			days[day] = 0
			parts := strings.Split(day, "/")
			if len(parts) == 3 {
				m, ok1 := pyInt(parts[0])
				d, ok2 := pyInt(parts[1])
				y, ok3 := pyInt(parts[2])
				if ord, ok := ordinalDay(y, m, d); ok1 && ok2 && ok3 && ok {
					days[day] = float64(ord) * 86400
				}
			}
		}
		parts := strings.Split(clock, ":")
		if len(parts) != 3 {
			return
		}
		h, ok1 := pyInt(parts[0])
		mi, ok2 := pyInt(parts[1])
		sec := []rune(parts[2])
		if len(sec) > 6 {
			sec = sec[:6]
		}
		s, ok3 := pyFloat(string(sec))
		if !ok1 || !ok2 || !ok3 {
			return
		}
		out = append(out, line{t: days[day] + float64(h)*3600 + float64(mi)*60 + s, row: row})
	}
	var r csvReader
	for _, l := range splitLines(sv.DecodeReplace(data)) {
		head, rest, ok := strings.Cut(l, "  ")
		if !ok {
			continue
		}
		stamp = head
		if row, ok := r.feed(rest); ok {
			emit(row)
		}
	}
	if row, ok := r.end(); ok {
		emit(row)
	}
	return out, nil
}

// pyMod is Python's x % m for a positive m.
func pyMod(x, m float64) float64 {
	r := math.Mod(x, m)
	if r < 0 {
		r += m
	}
	return r
}
