package combatlog

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kejith/wow-forever-tools/sim/internal/game"
	"github.com/kejith/wow-forever-tools/sim/internal/i18n"
	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
	"github.com/kejith/wow-forever-tools/sim/internal/sv"
	"github.com/kejith/wow-forever-tools/sim/run"
	"github.com/kejith/wow-forever-tools/sim/tools/enginebundle"
	"github.com/kejith/wow-forever-tools/sim/tools/rotations"
	"github.com/kejith/wow-forever-tools/sim/tools/upgrades"
)

const (
	// Gap is how long nothing happens between the character and an enemy before the fight is over.
	Gap = 8.0
	// lead: a cast that starts at most this long before the first hit opens the fight.
	lead = 6.0
	// advanced is the number of fields advanced logging puts in front of the amounts.
	advanced = 19
	// stale: a debuff the log never saw end is forgotten after this many seconds.
	stale       = 300.0
	maxTimeline = 80
	// slack: seconds a channel's last tick may be late.
	slack = 0.5
	// pulse: hits of an area channel at most this far apart are one tick.
	pulse = 0.3
)

// Unit flags of the log (COMBATLOG_OBJECT_*).
const (
	flagMine     = 0x1
	flagFriendly = 0x10
	flagPlayer   = 0x400
	flagPet      = 0x1000
	flagGuardian = 0x2000
)

var schools = map[int64]string{2: "holy", 4: "fire", 8: "nature", 16: "frost", 32: "shadow", 64: "arcane"}

func set(xs ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

var (
	damageEvents   = set("SPELL_DAMAGE", "SPELL_PERIODIC_DAMAGE", "RANGE_DAMAGE", "SWING_DAMAGE", "DAMAGE_SHIELD")
	missedEvents   = set("SPELL_MISSED", "SPELL_PERIODIC_MISSED", "RANGE_MISSED", "SWING_MISSED", "DAMAGE_SHIELD_MISSED")
	healEvents     = set("SPELL_HEAL", "SPELL_PERIODIC_HEAL")
	energizeEvents = set("SPELL_ENERGIZE", "SPELL_PERIODIC_ENERGIZE")
	auraOn         = set("SPELL_AURA_APPLIED", "SPELL_AURA_REFRESH")
	// notAMiss: the cast was not missed or resisted.
	notAMiss = set("ABSORB", "IMMUNE", "EVADE", "REFLECT", "DEFLECT")
)

const nobody = "0000000000000000"

// omap is a Python dict: its keys in the order they were first set; a key popped and set again goes last.
type omap[K comparable, V any] struct {
	keys []K
	m    map[K]V
}

func newOmap[K comparable, V any]() *omap[K, V] { return &omap[K, V]{m: map[K]V{}} }

func (o *omap[K, V]) get(k K) (V, bool) { v, ok := o.m[k]; return v, ok }

func (o *omap[K, V]) setdefault(k K, v V) V {
	if old, ok := o.m[k]; ok {
		return old
	}
	o.keys = append(o.keys, k)
	o.m[k] = v
	return v
}

func (o *omap[K, V]) pop(k K) (V, bool) {
	v, ok := o.m[k]
	if !ok {
		return v, false
	}
	delete(o.m, k)
	for i, x := range o.keys {
		if x == k {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
	return v, true
}

func (o *omap[K, V]) values() []V {
	out := make([]V, len(o.keys))
	for i, k := range o.keys {
		out[i] = o.m[k]
	}
	return out
}

func (o *omap[K, V]) len() int { return len(o.keys) }

// event is one line of the character or its pets, as combatlog.collect keeps it.
type event struct {
	t                                         float64
	spell                                     int64
	name                                      string
	school                                    int64
	dst, dstName                              string
	hostile                                   bool
	pet                                       string
	kind                                      string
	amount, base, resisted, blocked, absorbed float64
	over                                      float64
	crit, periodic                            bool
	how                                       string
	hp, hpMax, power, mana, manaMax           float64
	own                                       bool
}

type logChar struct {
	guid, name string
	events     []*event
}

type encounter struct {
	name       string
	start, end float64
	ended      bool
}

type logData struct {
	version    int64
	advanced   bool
	build      string
	first      float64
	last       float64
	lines      int
	chars      *omap[string, *logChar]
	deaths     map[string][]float64
	levels     map[string]int64
	encounters []*encounter
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// collect is combatlog.collect: one pass over the log.
func collect(path string) (*logData, error) {
	lines, err := readLog(path)
	if err != nil {
		return nil, err
	}
	log := &logData{chars: newOmap[string, *logChar](), deaths: map[string][]float64{}, levels: map[string]int64{}}
	var current *logChar
	for _, l := range lines {
		row, t := l.row, l.t
		if len(row) == 0 {
			continue
		}
		ev := row[0]
		log.lines++
		if log.first == 0 {
			log.first = t
		}
		log.last = t
		if ev == "COMBAT_LOG_VERSION" {
			head := map[string]string{}
			for i := 0; i+1 < len(row); i += 2 {
				head[row[i]] = row[i+1]
			}
			log.version = int64(num(head["COMBAT_LOG_VERSION"]))
			log.advanced = head["ADVANCED_LOG_ENABLED"] == "1"
			log.build = head["BUILD_VERSION"]
			continue
		}
		if ev == "ENCOUNTER_START" && len(row) > 2 {
			log.encounters = append(log.encounters, &encounter{name: row[2], start: t})
			continue
		}
		if ev == "ENCOUNTER_END" && len(log.encounters) > 0 && !log.encounters[len(log.encounters)-1].ended {
			last := log.encounters[len(log.encounters)-1]
			last.end, last.ended = t, true
			continue
		}
		if len(row) < 9 {
			continue
		}
		src, srcName, srcFlags := row[1], row[2], flags(row[3])
		dst, dstName, dstFlags := row[5], row[6], flags(row[7])
		if ev == "UNIT_DIED" {
			log.deaths[dst] = append(log.deaths[dst], t)
			continue
		}
		adv := 0
		if log.advanced {
			adv = advanced
		}
		swing := strings.HasPrefix(ev, "SWING")
		if strings.HasPrefix(ev, "ENVIRONMENTAL") || (!swing && len(row) < 12) {
			continue // falling and drowning are no fight; PARTY_KILL and the like have no spell
		}
		at := 12 // where the event's own fields start: a swing has no spell
		if swing {
			at = 9
		}
		var block []string
		if adv > 0 && len(row) >= at+adv {
			block = row[at : at+adv]
		}
		// The block's last field is a creature's level; for a player it is the item level.
		if block != nil && (strings.HasPrefix(block[0], "Creature-") || strings.HasPrefix(block[0], "Vehicle-")) &&
			isDigits(block[18]) {
			if n, err := strconv.ParseInt(block[18], 10, 64); err == nil {
				log.levels[block[0]] = n
			}
		}

		mineSrc, mineDst := srcFlags&flagMine != 0, dstFlags&flagMine != 0
		if !mineSrc && !mineDst {
			continue
		}
		for _, u := range []struct {
			guid, name string
			fl         int64
		}{{src, srcName, srcFlags}, {dst, dstName, dstFlags}} {
			if u.fl&flagMine != 0 && u.fl&flagPlayer != 0 {
				name, _, _ := strings.Cut(u.name, "-")
				current = log.chars.setdefault(u.guid, &logChar{guid: u.guid, name: name})
			}
		}
		if current == nil {
			continue
		}
		me := current.guid
		pet := mineSrc && src != me && srcFlags&(flagPet|flagGuardian) != 0
		if mineSrc && src != me && !pet {
			continue
		}
		e := &event{t: t, dst: dst, dstName: dstName,
			hostile: dst != nobody && dst != me && dstFlags&flagFriendly == 0}
		if swing {
			e.spell, e.name, e.school = 0, i18n.T("Melee"), 1
		} else {
			e.spell, e.name, e.school = int64(num(row[9])), row[10], flags(row[11])
		}
		if pet {
			e.pet = srcName
		}
		var rest []string
		if at+adv <= len(row) {
			rest = row[at+adv:]
		}
		switch {
		case damageEvents[ev] && len(rest) >= 8:
			e.amount, e.base, e.resisted = num(rest[0]), num(rest[1]), num(rest[4])
			e.blocked, e.absorbed, e.crit = num(rest[5]), num(rest[6]), rest[7] == "1"
			e.periodic = ev == "SPELL_PERIODIC_DAMAGE"
			switch {
			case mineSrc && dst == me:
				e.kind = "self" // a spell that burns its caster
			case mineSrc:
				e.kind = "damage"
			case srcFlags&flagFriendly == 0:
				e.kind = "taken"
			default:
				continue
			}
		case missedEvents[ev] && mineSrc:
			e.kind = "miss"
			if len(row) > at {
				e.how = row[at]
			}
			e.periodic = ev == "SPELL_PERIODIC_MISSED"
		case missedEvents[ev] && srcFlags&flagFriendly == 0:
			e.kind, e.amount = "taken", 0
		case healEvents[ev] && src == me && len(rest) >= 5:
			e.kind, e.amount, e.base, e.over = "heal", num(rest[0]), num(rest[1]), num(rest[2])
			e.crit, e.periodic = rest[4] == "1", ev == "SPELL_PERIODIC_HEAL"
		case energizeEvents[ev] && src == me && dst == me && len(rest) > 0:
			e.kind, e.amount = "energize", num(rest[0])
		case ev == "SPELL_CAST_SUCCESS" && src == me:
			e.kind = "cast"
			if block != nil && block[0] == me {
				e.hp, e.hpMax, e.power = num(block[2]), num(block[3]), num(block[5])
				if block[10] == "0" {
					e.mana, e.manaMax = num(block[11]), num(block[12])
				}
			}
		case ev == "SPELL_CAST_START" && src == me:
			e.kind = "start"
		case dst == me && len(row) > 12 && row[12] == "BUFF" && (auraOn[ev] || ev == "SPELL_AURA_REMOVED"):
			// A buff on the character, whoever cast it: it explains spell power and regeneration.
			e.kind = "buff-"
			if auraOn[ev] {
				e.kind = "buff+"
			}
			e.own = src == me
		case auraOn[ev] && mineSrc && !pet && dst != me && len(row) > 12 && row[12] == "DEBUFF":
			e.kind = "aura+"
		case ev == "SPELL_AURA_REMOVED" && mineSrc && !pet && dst != me && len(row) > 12 && row[12] == "DEBUFF":
			e.kind = "aura-"
		default:
			continue
		}
		current.events = append(current.events, e)
	}
	return log, nil
}

type span struct{ a, b float64 }

// fightsOf is combatlog.fights_of: the fights as (start, end).
func fightsOf(events []*event, gap float64) []span {
	var out []span
	var opener *event
	for _, e := range events {
		k := e.kind
		if k == "start" {
			opener = e
			continue
		}
		fights := ((k == "damage" || k == "miss") && e.hostile) || k == "taken" || (k == "cast" && e.hostile)
		if !fights {
			continue
		}
		if len(out) > 0 && e.t-out[len(out)-1].b <= gap {
			out[len(out)-1].b = e.t
			continue
		}
		start := e.t
		if opener != nil && opener.spell == e.spell && 0 <= start-opener.t && start-opener.t <= lead &&
			(len(out) == 0 || opener.t > out[len(out)-1].b) {
			start = opener.t
		}
		out = append(out, span{start, e.t})
	}
	return out
}

type rowKey struct {
	spell int64
	pet   string
}

// row is the sums of one spell (combatlog.new_row).
type row struct {
	spellID                                       int64
	name                                          string
	school                                        int64
	pet                                           string
	casts, hits, crits, ticks, tickCrits          int64
	misses                                        *omap[string, int64]
	damage, resisted, absorbed, healing, overheal float64
	heals                                         int64
	mana                                          float64
	energizes                                     int64
	selfDamage                                    float64
	hit, tick                                     []float64
	plain, crit                                   [2]float64
	gaps                                          []float64
	last                                          map[string]float64
	cover                                         []span
}

func newRow(e *event) *row {
	return &row{spellID: e.spell, name: e.name, school: e.school, pet: e.pet, misses: newOmap[string, int64](),
		last: map[string]float64{}}
}

// add is combatlog.add: books one event on its spell's row.
func add(rows *omap[rowKey, *row], e *event) *row {
	r := rows.setdefault(rowKey{e.spell, e.pet}, nil)
	if r == nil {
		r = newRow(e)
		rows.m[rowKey{e.spell, e.pet}] = r
	}
	switch e.kind {
	case "cast":
		r.casts++
	case "damage":
		full := e.amount + e.resisted + e.absorbed + e.blocked
		r.damage += e.amount
		r.resisted += e.resisted
		r.absorbed += e.absorbed
		if e.periodic {
			r.ticks++
		} else {
			r.hits++
		}
		if e.crit {
			if e.periodic {
				r.tickCrits++
			} else {
				r.crits++
			}
		}
		if e.base > 0 {
			if e.periodic {
				r.tick = append(r.tick, e.base)
			} else {
				r.hit = append(r.hit, e.base)
			}
			pair := &r.plain
			if e.crit {
				pair = &r.crit
			}
			pair[0] += full
			pair[1] += e.base
		}
		if e.periodic {
			if last, ok := r.last[e.dst]; ok && 0 < e.t-last && e.t-last <= 12 {
				r.gaps = append(r.gaps, e.t-last)
			}
			r.last[e.dst] = e.t
		}
	case "miss":
		n, _ := r.misses.get(e.how)
		r.misses.setdefault(e.how, 0)
		r.misses.m[e.how] = n + 1
	case "heal":
		r.healing += e.amount - e.over
		r.overheal += e.over
		r.heals++
	case "energize":
		r.mana += e.amount
		r.energizes++
	case "self":
		r.selfDamage += e.amount
	}
	return r
}

// merge is combatlog.merge: the game books some effects on a second spell of the same name (Life Tap's
// mana, the pulses of Hellfire and Rain of Fire). The row that was cast takes the rows of that name that
// never were.
func merge(rows *omap[rowKey, *row]) {
	type nameKey struct{ name, pet string }
	names := newOmap[nameKey, []rowKey]()
	for _, k := range rows.keys {
		r := rows.m[k]
		nk := nameKey{r.name, r.pet}
		keys, _ := names.get(nk)
		names.setdefault(nk, nil)
		names.m[nk] = append(keys, k)
	}
	for _, keys := range names.values() {
		var cast, rest []rowKey
		for _, k := range keys {
			if rows.m[k].casts != 0 {
				cast = append(cast, k)
			} else {
				rest = append(rest, k)
			}
		}
		if len(cast) != 1 || len(rest) == 0 {
			continue
		}
		into := rows.m[cast[0]]
		for _, k := range rest {
			r, _ := rows.pop(k)
			into.casts += r.casts
			into.hits += r.hits
			into.crits += r.crits
			into.ticks += r.ticks
			into.tickCrits += r.tickCrits
			for _, how := range r.misses.keys {
				n, _ := into.misses.get(how)
				into.misses.setdefault(how, 0)
				into.misses.m[how] = n + r.misses.m[how]
			}
			into.damage += r.damage
			into.resisted += r.resisted
			into.absorbed += r.absorbed
			into.healing += r.healing
			into.overheal += r.overheal
			into.heals += r.heals
			into.mana += r.mana
			into.energizes += r.energizes
			into.selfDamage += r.selfDamage
			into.hit = append(into.hit, r.hit...)
			into.tick = append(into.tick, r.tick...)
			into.plain = [2]float64{into.plain[0] + r.plain[0], into.plain[1] + r.plain[1]}
			into.crit = [2]float64{into.crit[0] + r.crit[0], into.crit[1] + r.crit[1]}
			into.gaps = append(into.gaps, r.gaps...)
			into.cover = append(into.cover, r.cover...)
		}
	}
}

// covered is the seconds covered by at least one of the spans.
func covered(spans []span) float64 {
	s := append([]span(nil), spans...)
	sort.Slice(s, func(i, j int) bool {
		if s[i].a != s[j].a {
			return s[i].a < s[j].a
		}
		return s[i].b < s[j].b
	})
	total, end := 0.0, math.Inf(-1)
	for _, x := range s {
		if x.b > end {
			total += x.b - math.Max(x.a, end)
			end = x.b
		}
	}
	return total
}

func optRound(ok bool, x float64, n int) any {
	if !ok {
		return nil
	}
	return pyfmt.Round(x, n)
}

func fmean(xs []float64) float64 { return pyfmt.Fsum(xs) / float64(len(xs)) }

// finished is a spell's row as it is given out, with the values the callers read again.
type finished struct {
	obj                               *pyjson.Obj
	spellID                           int64
	pet, name                         string
	school                            int64
	casts                             int64
	damage, healing, mana, selfDamage int64
}

// finish is combatlog.finish: a spell's row as it is given out: sums, averages, shares.
func finish(r *row, seconds float64, model *run.SpellNumbers) finished {
	gaps := append([]float64(nil), r.gaps...)
	sort.Float64s(gaps)
	missed := int64(0)
	misses := pyjson.New()
	for _, how := range r.misses.keys {
		n := r.misses.m[how]
		misses.Set(how, n)
		if !notAMiss[how] {
			missed += n
		}
	}
	attempts := r.casts
	if attempts == 0 {
		attempts = r.hits + missed
	}
	full := r.damage + r.resisted + r.absorbed
	f := finished{spellID: r.spellID, pet: r.pet, name: r.name, school: r.school, casts: r.casts,
		damage: pyfmt.RoundInt(r.damage), healing: pyfmt.RoundInt(r.healing), mana: pyfmt.RoundInt(r.mana),
		selfDamage: pyfmt.RoundInt(r.selfDamage)}
	perMin, dps := any(0.0), any(0.0)
	if seconds != 0 {
		perMin = pyfmt.Round(float64(r.casts)/seconds*60, 1)
		dps = pyfmt.Round(r.damage/seconds, 1)
	}
	resistedPct := any(0.0)
	if full != 0 {
		resistedPct = pyfmt.Round(r.resisted/full*100, 1)
	}
	var avgHit, avgTick any
	if len(r.hit) > 0 {
		avgHit = pyfmt.Round(fmean(r.hit), 1)
	}
	if len(r.tick) > 0 {
		avgTick = pyfmt.Round(fmean(r.tick), 1)
	}
	var tickSec, fast, slow any
	if len(gaps) > 0 {
		tickSec = pyfmt.Round(pyfmt.Median(gaps), 2)
		fast = pyfmt.Round(gaps[len(gaps)/10], 2)
		slow = pyfmt.Round(gaps[len(gaps)*9/10], 2)
	}
	var uptime any
	if len(r.cover) > 0 && seconds != 0 {
		uptime = pyfmt.Round(covered(r.cover)/seconds*100, 1)
	}
	var critMult any
	if r.crit[1] != 0 && r.plain[1] != 0 && r.plain[0] != 0 {
		critMult = pyfmt.Round(r.crit[0]/r.crit[1]/(r.plain[0]/r.plain[1]), 2)
	}
	missPct := any(nil)
	if attempts != 0 {
		missPct = pyfmt.Round(math.Min(float64(missed)/float64(attempts), 1)*100, 1)
	}
	out := pyjson.New("spellId", r.spellID, "name", r.name, "school", r.school, "pet", r.pet, "casts", r.casts,
		"hits", r.hits, "crits", r.crits, "ticks", r.ticks, "tickCrits", r.tickCrits, "misses", misses,
		"damage", f.damage, "resisted", pyfmt.RoundInt(r.resisted), "absorbed", pyfmt.RoundInt(r.absorbed),
		"healing", f.healing, "overheal", pyfmt.RoundInt(r.overheal), "heals", r.heals, "mana", f.mana,
		"energizes", r.energizes, "selfDamage", f.selfDamage,
		"castsPerMin", perMin, "dps", dps,
		"critPct", optRound(r.hits != 0, float64(r.crits)/float64(r.hits)*100, 1),
		"tickCritPct", optRound(r.ticks != 0, float64(r.tickCrits)/float64(r.ticks)*100, 1),
		"missed", missed, "missPct", missPct, "resistedPct", resistedPct,
		"avgHit", avgHit, "avgTick", avgTick,
		// What the enemies added to or took off the amount before any resist, and what a crit multiplies.
		"takenPct", optRound(r.plain[1] != 0, (r.plain[0]/r.plain[1]-1)*100, 1),
		"critMult", critMult, "tickSec", tickSec, "tickSecFast", fast, "tickSecSlow", slow,
		"uptimePct", uptime)
	if model != nil {
		m := pyjson.New()
		has := map[string]float64{}
		setM := func(k string, v float64) { m.Set(k, v); has[k] = v }
		if model.DirectMin != 0 || model.DirectMax != 0 {
			setM("hit", pyfmt.Round((model.DirectMin+model.DirectMax)/2, 1))
		}
		if model.Periodic != 0 && model.Ticks != 0 {
			setM("tick", pyfmt.Round(model.Periodic/model.Ticks, 1))
		}
		_, hasHit := has["hit"]
		tick, hasTick := has["tick"]
		if sv.Truthy(avgHit) && !sv.Truthy(avgTick) && !hasHit && hasTick {
			// An area channel: the log has every pulse as a hit of its own.
			m = pyjson.New()
			delete(has, "tick")
			setM("hit", tick)
		}
		for _, p := range []struct {
			mine   any
			theirs string
		}{{avgHit, "hit"}, {avgTick, "tick"}} {
			if v, ok := has[p.theirs]; ok && sv.Truthy(p.mine) && v != 0 {
				m.Set(p.theirs+"DiffPct", pyfmt.Round((p.mine.(float64)/v-1)*100, 1))
			}
		}
		if len(m.Keys()) > 0 {
			out.Set("model", m)
		}
	}
	f.obj = out
	return f
}

// ended is how one spell's channels ended.
type ended struct {
	casts, ticks, sum, full, died, cut, short, missed, clipped int64
	lost                                                       map[string]int64
}

func (c *ended) obj() *pyjson.Obj {
	var avg any
	if c.casts != 0 {
		avg = pyfmt.Round(float64(c.sum)/float64(c.casts), 2)
	}
	return pyjson.New("casts", c.casts, "ticks", c.ticks, "full", c.full, "died", c.died, "cut", c.cut,
		"short", c.short, "missed", c.missed, "clipped", c.clipped,
		"lost", pyjson.New("died", c.lost["died"], "cut", c.lost["cut"], "short", c.lost["short"]),
		"avgTicks", avg)
}

// channels is combatlog.channels: how the channels ended, per spell the model calls channeled.
func channels(events []*event, spans []span, deaths map[string][]float64, numbers map[int64]run.SpellNumbers) map[int64]*ended {
	known := map[int64]run.SpellNumbers{}
	for id, n := range numbers {
		if n.Channeled && n.Ticks != 0 && n.CastSec != 0 {
			known[id] = n
		}
	}
	var own []*event
	if len(known) > 0 {
		for _, e := range events {
			if e.pet == "" {
				own = append(own, e)
			}
		}
	}
	out := map[int64]*ended{}
	s := 0
	for i, e := range own {
		n, ok := known[e.spell]
		if e.kind != "cast" || !ok {
			continue
		}
		for s < len(spans) && spans[s].b < e.t {
			s++
		}
		if s == len(spans) || spans[s].a > e.t {
			continue // not in a fight
		}
		ticks, dur := pyfmt.RoundInt(n.Ticks), n.CastSec
		single := e.hostile
		var following *float64
		var seen []float64
		missed := false
		for _, x := range own[i+1:] {
			if x.t > e.t+dur+slack {
				break
			}
			k := x.kind
			if k == "cast" || k == "start" {
				if following == nil && x.t > e.t {
					t := x.t
					following = &t
				}
				if k == "cast" && x.name == e.name {
					break // the next channel of the same spell: what follows are its ticks
				}
			} else if x.name == e.name && (k == "damage" || k == "self" || k == "miss") && (!single || x.dst == e.dst) {
				if k == "miss" && !x.periodic {
					missed = missed || single
				} else if x.t > e.t+0.05 {
					seen = append(seen, x.t)
				}
			}
		}
		sort.Float64s(seen)
		got, last := int64(0), math.Inf(-1)
		for _, t := range seen {
			if t-last > pulse {
				got++
			}
			last = t
		}
		if got > ticks {
			got = ticks
		}
		c := out[e.spell]
		if c == nil {
			c = &ended{ticks: ticks, lost: map[string]int64{"died": 0, "cut": 0, "short": 0}}
			out[e.spell] = c
		}
		if missed && got == 0 {
			c.missed++
			continue
		}
		c.casts++
		c.sum += got
		if got >= ticks {
			c.full++
			continue
		}
		died := false
		if single {
			for _, d := range deaths[e.dst] {
				if e.t <= d && d <= e.t+dur+slack && (following == nil || d <= *following+0.1) {
					died = true
					break
				}
			}
		}
		how := "short"
		if died {
			how = "died"
		} else if following != nil && *following < e.t+dur-0.05 {
			how = "cut"
		}
		switch how {
		case "died":
			c.died++
		case "cut":
			c.cut++
		default:
			c.short++
		}
		c.lost[how] += ticks - got
		if how == "cut" && got == ticks-1 {
			c.clipped++
		}
	}
	return out
}

// clockOf is combatlog.clock_of: the time of day of a log time.
func clockOf(t float64) string {
	s := int64(pyMod(t, 86400))
	return fmt.Sprintf("%02d:%02d:%02d", s/3600, s/60%60, s%60)
}

// FindLogs are the combat logs of the game folder, newest first.
func FindLogs(wow string) []string {
	paths := game.Glob(wow, "Logs", "WoWCombatLog*.txt")
	mtime := map[string]float64{}
	for _, p := range paths {
		mtime[p], _ = game.Mtime(p)
	}
	sort.SliceStable(paths, func(i, j int) bool { return mtime[paths[i]] > mtime[paths[j]] })
	return paths
}

// snapshotOf is the character's WoWMCP snapshot, or nothing: the log itself does not say the level.
func snapshotOf(wow, name string) (*sv.Table, error) {
	files := game.FindChars(wow, name)
	if len(files) == 0 {
		return sv.NewTable(), nil
	}
	return upgrades.LoadChar(files[0])
}

// modelNumbers is combatlog.model_numbers: what the combat model says one cast of each known spell rank
// does, at the spell power given (the log's, which moves with the buffs) instead of the snapshot's.
func modelNumbers(wow, name string, spellPower int64) (map[int64]run.SpellNumbers, error) {
	atPower := func(c *sv.Table) {
		schools := c.Tab("character").Tab("stats").Tab("spellDamage")
		if spellPower == 0 || schools.Len() == 0 {
			return
		}
		high := math.Inf(-1)
		for _, v := range schools.Values() {
			if f, ok := sv.Num(v); ok && f > high {
				high = f
			}
		}
		delta := float64(spellPower) - high
		for _, k := range schools.Keys() {
			f, _ := sv.Num(schools.Get(k))
			schools.Set(k, f+delta)
		}
	}
	setup, err := rotations.Character(wow, name, 60, -1, -1, atPower)
	if err != nil {
		return nil, err
	}
	c := setup.C
	eng, err := setup.Start(enginebundle.DefaultMetric(c.Tab("character").Str("classFile"), enginebundle.SpecOf(c)), 0, nil)
	if err != nil {
		return nil, err
	}
	if _, err := eng.Batch(nil, false); err != nil {
		return nil, err
	}
	out := map[int64]run.SpellNumbers{}
	for _, n := range eng.Spells {
		out[int64(n.SpellID)] = n
	}
	return out, nil
}

type buffRow struct {
	on    *event
	cover []span
}

type auraKey struct {
	spell int64
	dst   string
}

// Compute is combatlog.compute: the evaluation of a combat log as plain data. file is the log to read
// (default: the newest of the game); model asks the combat engine for its numbers.
func Compute(wow, char, file string, gap float64, model bool) (*pyjson.Obj, error) {
	path := file
	if path == "" {
		if logs := FindLogs(wow); len(logs) > 0 {
			path = logs[0]
		}
	}
	if st, err := os.Stat(path); path == "" || err != nil || !st.Mode().IsRegular() {
		return nil, errors.New(i18n.T("No combat log found. Type /combatlog in the game before the fight; " +
			"the game writes it to its Logs folder."))
	}
	log, err := collect(path)
	if err != nil {
		return nil, err
	}
	chars := log.chars.values()
	sort.SliceStable(chars, func(i, j int) bool { return len(chars[i].events) > len(chars[j].events) })
	if char != "" {
		var keep []*logChar
		for _, c := range chars {
			if strings.HasPrefix(strings.ToLower(c.name), strings.ToLower(char)) {
				keep = append(keep, c)
			}
		}
		chars = keep
	}
	if len(chars) == 0 {
		return nil, errors.New(i18n.T("The combat log has no fights of this character."))
	}
	me := chars[0]
	events := me.events
	spans := fightsOf(events, gap)

	snapshot, err := snapshotOf(wow, me.name)
	if err != nil {
		return nil, err
	}
	label := rotations.NewLabeler(snapshot)

	rows := newOmap[rowKey, *row]()
	var fights []*pyjson.Obj
	var fightSeconds, fightDamage, fightHealing []float64
	var fightKills []int64
	auras := newOmap[auraKey, *event]()
	buffs := newOmap[int64, *event]()      // the buffs up right now
	buffRows := newOmap[int64, *buffRow]() // per buff when it was up during the fights
	level, _ := snapshot.Tab("character").Num("level")
	var power []float64
	type hitCount struct{ casts, misses int64 }
	byDelta := map[int64]*hitCount{} // casts on enemies and how many missed, by the enemy's level above
	coverRow := func(on *event, a, b float64) {
		x := *on
		x.kind = ""
		r := add(rows, &x)
		r.cover = append(r.cover, span{a, b})
	}
	coverBuff := func(on *event, a, b float64) {
		br := buffRows.setdefault(on.spell, &buffRow{on: on})
		br.cover = append(br.cover, span{a, b})
	}
	i := 0
	for _, sp := range spans {
		start, end := sp.a, sp.b
		var mine []*event
		for i < len(events) && events[i].t <= end {
			e := events[i]
			i++
			// Debuffs are followed outside the fights too: one may be older than the fight it works in.
			switch e.kind {
			case "aura+":
				auras.setdefault(auraKey{e.spell, e.dst}, e)
			case "aura-":
				if on, ok := auras.pop(auraKey{e.spell, e.dst}); ok && e.t > start {
					coverRow(on, math.Max(on.t, start), e.t)
				}
			case "buff+":
				buffs.setdefault(e.spell, e)
			case "buff-":
				if on, ok := buffs.pop(e.spell); ok && e.t > start {
					coverBuff(on, math.Max(on.t, start), e.t)
				}
			}
			if e.t >= start {
				mine = append(mine, e)
			}
		}
		for _, on := range buffs.values() {
			coverBuff(on, math.Max(on.t, start), end)
		}
		for _, key := range append([]auraKey(nil), auras.keys...) { // still up when the fight ends
			on := auras.m[key]
			if on.t < start-stale {
				auras.pop(key)
			} else {
				coverRow(on, math.Max(on.t, start), end)
			}
		}
		fRows := newOmap[rowKey, *row]()
		enemies := newOmap[string, string]()
		var mana, hp []float64
		var casts []any
		taken := 0.0
		for _, e := range mine {
			k := e.kind
			switch k {
			case "aura+", "aura-", "buff+", "buff-", "start":
				continue
			case "taken":
				taken += e.amount
				continue
			}
			add(rows, e)
			add(fRows, e)
			if (k == "damage" || k == "miss") && e.hostile {
				enemies.setdefault(e.dst, e.dstName)
			}
			if lv, ok := log.levels[e.dst]; e.hostile && e.pet == "" && level != 0 && ok {
				d := lv - int64(level)
				hc := byDelta[d]
				if hc == nil {
					hc = &hitCount{}
					byDelta[d] = hc
				}
				if k == "cast" {
					hc.casts++
				} else if k == "miss" && !e.periodic && !notAMiss[e.how] {
					hc.misses++
				}
			}
			if k == "cast" {
				if e.manaMax != 0 {
					mana = append(mana, e.mana/e.manaMax*100)
				}
				if e.hpMax != 0 {
					hp = append(hp, e.hp/e.hpMax*100)
					power = append(power, e.power)
				}
				target := ""
				if e.hostile {
					target = e.dstName
				}
				var manaPct, healthPct any
				if e.manaMax != 0 {
					manaPct = pyfmt.RoundInt(mana[len(mana)-1])
				}
				if e.hpMax != 0 {
					healthPct = pyfmt.RoundInt(hp[len(hp)-1])
				}
				casts = append(casts, pyjson.New("t", pyfmt.Round(e.t-start, 1), "spellId", e.spell,
					"label", label(int(e.spell), e.name, 0), "target", target, "manaPct", manaPct,
					"healthPct", healthPct))
			}
		}
		seconds := math.Max(end-start, 1.0)
		var dmg, heal []float64
		for _, r := range fRows.values() {
			dmg = append(dmg, r.damage)
			heal = append(heal, r.healing)
		}
		damage := pyfmt.Sum(dmg...)
		anyHealing := false
		for _, h := range heal {
			anyHealing = anyHealing || h != 0
		}
		if damage == 0 && !anyHealing {
			continue
		}
		var deltas []int64
		for _, g := range enemies.keys {
			if lv, ok := log.levels[g]; ok && level != 0 {
				deltas = append(deltas, lv-int64(level))
			}
		}
		merge(fRows)
		var boss *encounter
		for _, x := range log.encounters {
			xEnd := x.start
			if x.ended && x.end != 0 {
				xEnd = x.end
			}
			if x.start <= end && xEnd >= start {
				boss = x
				break
			}
		}
		encounterName := ""
		if boss != nil {
			encounterName = boss.name
		}
		names := map[string]bool{}
		var targets []string
		for _, n := range enemies.values() {
			if !names[n] {
				names[n] = true
				targets = append(targets, n)
			}
		}
		sort.Strings(targets)
		kills := int64(0)
		for _, g := range enemies.keys {
			for _, t := range log.deaths[g] {
				if start <= t && t <= end+2 {
					kills++
					break
				}
			}
		}
		var levelDelta any
		if len(deltas) > 0 {
			high := deltas[0]
			for _, d := range deltas[1:] {
				if d > high {
					high = d
				}
			}
			levelDelta = high
		}
		type fightSpell struct {
			obj  *pyjson.Obj
			sort int64
		}
		var fspells []fightSpell
		healing := pyfmt.Sum(func() []float64 {
			var h []float64
			for _, r := range fRows.values() {
				h = append(h, r.healing)
			}
			return h
		}()...)
		for _, r := range fRows.values() {
			if r.casts == 0 && r.damage == 0 && r.healing == 0 && r.mana == 0 {
				continue
			}
			d, h := pyfmt.RoundInt(r.damage), pyfmt.RoundInt(r.healing)
			fspells = append(fspells, fightSpell{pyjson.New("spellId", r.spellID,
				"label", label(int(r.spellID), r.name, 0), "pet", r.pet, "casts", r.casts, "damage", d,
				"healing", h, "mana", pyfmt.RoundInt(r.mana)), -(d + h)})
		}
		sort.SliceStable(fspells, func(a, b int) bool { return fspells[a].sort < fspells[b].sort })
		spellList := make([]any, len(fspells))
		for j, s := range fspells {
			spellList[j] = s.obj
		}
		pct := func(xs []float64, pick func([]float64) float64) any {
			if len(xs) == 0 {
				return nil
			}
			return pyfmt.RoundInt(pick(xs))
		}
		first := func(xs []float64) float64 { return xs[0] }
		lastOf := func(xs []float64) float64 { return xs[len(xs)-1] }
		low := func(xs []float64) float64 {
			m := xs[0]
			for _, x := range xs[1:] {
				m = math.Min(m, x)
			}
			return m
		}
		shown := casts
		if len(shown) > maxTimeline {
			shown = shown[:maxTimeline]
		}
		more := len(casts) - maxTimeline
		if more < 0 {
			more = 0
		}
		secRounded := pyfmt.Round(seconds, 1)
		damageRounded := pyfmt.RoundInt(damage)
		healingRounded := pyfmt.RoundInt(healing)
		fightSeconds = append(fightSeconds, secRounded)
		fightDamage = append(fightDamage, float64(damageRounded))
		fightHealing = append(fightHealing, float64(healingRounded))
		fightKills = append(fightKills, kills)
		if targets == nil {
			targets = []string{}
		}
		fights = append(fights, pyjson.New("encounter", encounterName, "n", len(fights)+1, "start", clockOf(start),
			"seconds", secRounded, "enemies", enemies.len(), "targets", targets, "levelDelta", levelDelta,
			"kills", kills, "damage", damageRounded, "dps", pyfmt.Round(damage/seconds, 1),
			"healing", healingRounded, "taken", pyfmt.RoundInt(taken),
			"manaStartPct", pct(mana, first), "manaEndPct", pct(mana, lastOf), "manaLowPct", pct(mana, low),
			"healthLowPct", pct(hp, low), "spells", spellList, "casts", shown, "moreCasts", more))
	}

	active := pyfmt.Sum(fightSeconds...)
	numbers := map[int64]run.SpellNumbers{}
	modelError := ""
	modelPower := int64(0)
	if len(power) > 0 {
		modelPower = pyfmt.RoundInt(pyfmt.Median(power))
	}
	if model {
		if n, err := modelNumbers(wow, me.name, modelPower); err != nil {
			modelError = err.Error()
		} else {
			numbers = n
		}
	}
	merge(rows)
	ends := channels(events, spans, log.deaths, numbers)
	total := pyfmt.Sum(fightDamage...)
	stats := snapshot.Tab("character").Tab("stats")
	spellHit, _ := stats.Num("spellHit")
	var deltas []int64
	for d := range byDelta {
		deltas = append(deltas, d)
	}
	sort.Slice(deltas, func(a, b int) bool { return deltas[a] < deltas[b] })
	hit := []any{}
	for _, d := range deltas {
		v := byDelta[d]
		if v.casts == 0 {
			continue
		}
		var modelMiss any
		if snapshot.Len() > 0 && d <= 3 {
			modelMiss = math.Max(upgrades.MinMiss, upgrades.SpellMiss[int(min(max(d, 0), 3))]-spellHit)
		}
		hit = append(hit, pyjson.New("levelDelta", d, "casts", v.casts, "misses", v.misses,
			"missPct", pyfmt.Round(float64(v.misses)/float64(v.casts)*100, 1), "modelMissPct", modelMiss))
	}

	type spellOut struct {
		obj  *pyjson.Obj
		sort int64
	}
	var spells []spellOut
	crits := stats.Tab("spellCrit")
	for _, r := range rows.values() {
		var m *run.SpellNumbers
		if r.pet == "" {
			if n, ok := numbers[r.spellID]; ok {
				m = &n
			}
		}
		f := finish(r, active, m)
		if f.casts == 0 && f.damage == 0 && f.healing == 0 && f.mana == 0 && f.selfDamage == 0 {
			continue
		}
		out := f.obj
		out.Set("label", label(int(f.spellID), f.name, 0))
		if total != 0 {
			out.Set("sharePct", pyfmt.Round(float64(f.damage)/total*100, 1))
		} else {
			out.Set("sharePct", 0.0)
		}
		if crit := crits.Get(schools[f.school]); crit != nil && f.pet == "" {
			if c, ok := sv.Num(crit); ok {
				out.Set("sheetCritPct", pyfmt.Round(c, 1))
			}
		}
		if c, ok := ends[f.spellID]; ok && f.pet == "" {
			out.Set("channel", c.obj())
		}
		spells = append(spells, spellOut{out, -(f.damage + f.healing)})
	}
	sort.SliceStable(spells, func(a, b int) bool { return spells[a].sort < spells[b].sort })
	spellList := make([]any, len(spells))
	for j, s := range spells {
		spellList[j] = s.obj
	}

	type buffOut struct {
		obj    *pyjson.Obj
		uptime float64
		name   string
	}
	var buffList []buffOut
	for _, b := range buffRows.values() {
		c := covered(b.cover)
		if active == 0 || c <= 0 {
			continue
		}
		up := pyfmt.Round(c/active*100, 1)
		buffList = append(buffList, buffOut{pyjson.New("spellId", b.on.spell, "name", b.on.name, "own", b.on.own,
			"uptimePct", up), up, b.on.name})
	}
	sort.SliceStable(buffList, func(a, b int) bool {
		if buffList[a].uptime != buffList[b].uptime {
			return buffList[a].uptime > buffList[b].uptime
		}
		return buffList[a].name < buffList[b].name
	})
	buffObjs := make([]any, len(buffList))
	for j, b := range buffList {
		buffObjs[j] = b.obj
	}

	modified, _ := game.Mtime(path)
	others := []any{}
	for _, c := range chars[1:] {
		others = append(others, c.name)
	}
	var spellPower any
	if len(power) > 0 {
		lo, hi := power[0], power[0]
		for _, p := range power[1:] {
			lo, hi = math.Min(lo, p), math.Max(hi, p)
		}
		spellPower = pyjson.New("low", pyfmt.RoundInt(lo), "high", pyfmt.RoundInt(hi))
	}
	var sheetSpellDamage any
	if sd := stats.Tab("spellDamage"); sd.Len() > 0 {
		sheetSpellDamage = sd
	}
	var modelSpellPower any
	if len(numbers) > 0 && modelPower != 0 {
		modelSpellPower = modelPower
	}
	var levelOut any
	if level != 0 {
		levelOut = snapshot.Tab("character").Get("level")
	}
	totalDPS := any(0.0)
	if active != 0 {
		totalDPS = pyfmt.Round(total/active, 1)
	}
	totalHealing := int64(0)
	for _, h := range fightHealing {
		totalHealing += int64(h)
	}
	totalKills := int64(0)
	for _, k := range fightKills {
		totalKills += k
	}
	fightList := make([]any, len(fights))
	for j, f := range fights {
		fightList[j] = f
	}
	return pyjson.New(
		"file", filepath.Base(path), "modified", modified,
		"started", clockOf(log.first), "seconds", pyfmt.RoundInt(log.last-log.first),
		"lines", log.lines, "version", log.version, "advanced", log.advanced, "build", log.build,
		"name", me.name, "level", levelOut, "others", others,
		"spellPower", spellPower, "sheetSpellDamage", sheetSpellDamage, "modelSpellPower", modelSpellPower,
		"total", pyjson.New("fights", len(fights), "seconds", pyfmt.Round(active, 1),
			"damage", pyfmt.RoundInt(total), "dps", totalDPS, "healing", totalHealing, "kills", totalKills),
		"spells", spellList, "hit", hit, "fights", fightList,
		// Buffs the log saw start (one that was up before the log began is not in it).
		"buffs", buffObjs, "model", len(numbers) > 0, "modelError", modelError), nil
}
