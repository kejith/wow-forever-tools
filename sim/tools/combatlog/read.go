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

	"github.com/kejith/wow-tools-forever/sim/internal/pycsv"
	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
	"github.com/kejith/wow-tools-forever/sim/internal/sv"
)

// line is one record of the log: when it happened (seconds) and its fields.
type line struct {
	t   float64
	row []string
}

// num is combatlog.num: float(x), 0 for anything else.
func num(x string) float64 {
	f, _ := pyfmt.ParseFloat(x)
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
				m, ok1 := pyfmt.ParseInt(parts[0])
				d, ok2 := pyfmt.ParseInt(parts[1])
				y, ok3 := pyfmt.ParseInt(parts[2])
				if ord, ok := ordinalDay(y, m, d); ok1 && ok2 && ok3 && ok {
					days[day] = float64(ord) * 86400
				}
			}
		}
		parts := strings.Split(clock, ":")
		if len(parts) != 3 {
			return
		}
		h, ok1 := pyfmt.ParseInt(parts[0])
		mi, ok2 := pyfmt.ParseInt(parts[1])
		sec := []rune(parts[2])
		if len(sec) > 6 {
			sec = sec[:6]
		}
		s, ok3 := pyfmt.ParseFloat(string(sec))
		if !ok1 || !ok2 || !ok3 {
			return
		}
		out = append(out, line{t: days[day] + float64(h)*3600 + float64(mi)*60 + s, row: row})
	}
	var r pycsv.Reader
	for _, l := range pycsv.SplitLines(sv.DecodeReplace(data)) {
		head, rest, ok := strings.Cut(l, "  ")
		if !ok {
			continue
		}
		stamp = head
		if row, ok := r.Feed(rest); ok {
			emit(row)
		}
	}
	if row, ok := r.End(); ok {
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
