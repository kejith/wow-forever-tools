package enginebundle

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/sv"
	"github.com/kejith/wow-forever-tools/sim/run"
)

// number is simengine.NUMBER (Python's \d: every Unicode decimal digit).
var number = regexp.MustCompile(`\p{Nd}+(?:[.,]\p{Nd}+)?`)

// GCDSec is the engine's global cooldown: no cast occupies less time.
const GCDSec = 1.5

// Off is one spell rank the tooltip check found off.
type Off struct {
	SpellID int
	Name    string
	Rank    string
	Kind    string    // "amount", "cost" or "cast"
	Model   []float64 // the model's numbers (rounded as Python rounds them)
	Text    string    // the game's text (amount)
	Game    any       // the game's number (cost, cast)
}

// Check is the result of TooltipCheck.
type Check struct {
	Checked, Matched int
	Costs            bool // the export had costs to compare
	Off              []Off
}

// digitValue turns the Unicode digits of a match into ASCII so that ParseFloat reads them as Python's
// float() does.
func digitValue(s string) float64 {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ',':
			b.WriteByte('.')
		case r == '.':
			b.WriteByte('.')
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteString(strconv.Itoa(digitOf(r)))
		}
	}
	f, _ := strconv.ParseFloat(b.String(), 64)
	return f
}

// digitOf is the value of a Unicode decimal digit (its offset from the zero of its block).
func digitOf(r rune) int {
	for z := r; z > r-10; z-- {
		if !isNd(z - 1) {
			return int(r - z)
		}
	}
	return 0
}

var nd = regexp.MustCompile(`^\p{Nd}$`)

func isNd(r rune) bool { return nd.MatchString(string(r)) }

// TooltipCheck is simengine.tooltip_check: lays the model's numbers next to the game's own spell texts.
// book is the spellbook export (spellbook.spells); coverage limits the check to spells the model casts or
// tried. Nil without anything to check.
func TooltipCheck(spells []run.SpellNumbers, book *sv.Table, coverage *run.Coverage) *Check {
	if len(spells) == 0 || book.Len() == 0 {
		return nil
	}
	var bases map[int]bool
	if coverage != nil {
		baseOf := map[int]int{}
		for _, n := range spells {
			baseOf[n.SpellID] = n.BaseID
		}
		bases = map[int]bool{}
		for _, e := range coverage.Spells {
			if e.Status == "used" || e.Status == "idle" {
				bases[baseOf[e.SpellID]] = true
			}
		}
	}
	c := &Check{}
	for _, n := range spells {
		entry := book.Tab(int64(n.SpellID))
		if entry == nil {
			entry = book.Tab(strconv.Itoa(n.SpellID))
		}
		text := entry.Str("text")
		if text == "" || (bases != nil && !bases[n.BaseID]) {
			continue
		}
		var seen []float64
		for _, x := range number.FindAllString(text, -1) {
			seen = append(seen, digitValue(x))
		}
		var amounts [][]float64
		for _, v := range []float64{n.DirectMin, n.DirectMax, n.Absorb} {
			if v != 0 {
				amounts = append(amounts, []float64{v})
			}
		}
		if n.Periodic != 0 {
			if n.Ticks != 0 {
				amounts = append(amounts, []float64{n.Periodic, n.Periodic / n.Ticks})
			} else {
				amounts = append(amounts, []float64{n.Periodic})
			}
		}
		// Another power than mana (health, a pet's energy) is not the model's mana.
		var cost any
		if !sv.Truthy(entry.Get("costType")) {
			cost = entry.Get("cost")
		}
		castMs, _ := entry.Num("castTime")
		cast := castMs / 1000
		if len(amounts) == 0 && cost == nil && cast == 0 {
			continue
		}
		c.Checked++
		before := len(c.Off)
		who := Off{SpellID: n.SpellID, Name: entry.Str("name"), Rank: entry.Str("rank")}
		// The client rounds the ends of the base range and then the sum: up to a point and a bit.
		ok := true
		for _, forms := range amounts {
			hit := false
			for _, v := range forms {
				for _, x := range seen {
					if math.Abs(x-v) <= 1+0.002*v {
						hit = true
					}
				}
			}
			ok = ok && hit
		}
		if !ok {
			o := who
			o.Kind, o.Text = "amount", text
			for _, forms := range amounts {
				o.Model = append(o.Model, float64(pyfmt.RoundInt(forms[0])))
			}
			c.Off = append(c.Off, o)
		}
		if cost != nil {
			c.Costs = true
			cf, _ := sv.Num(cost)
			if math.Abs(cf-n.Mana) > 1 {
				o := who
				o.Kind, o.Model, o.Game = "cost", []float64{float64(pyfmt.RoundInt(n.Mana))}, cost
				c.Off = append(c.Off, o)
			}
		}
		if cast != 0 && math.Abs(math.Max(cast, GCDSec)-n.CastSec) > 0.02 {
			o := who
			o.Kind, o.Model, o.Game = "cast", []float64{pyfmt.Round(n.CastSec, 2)}, pyfmt.Round(cast, 2)
			c.Off = append(c.Off, o)
		}
		if len(c.Off) == before {
			c.Matched++
		}
	}
	if c.Checked == 0 {
		return nil
	}
	return c
}
