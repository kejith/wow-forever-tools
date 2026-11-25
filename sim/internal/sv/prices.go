package sv

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"time"

	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyfmt"
)

var realmEntry = regexp.MustCompile(`\["([^"]+)"\] = "`)

// ErrNoPrices is the error when Auctionator.lua holds no price database.
var ErrNoPrices = errors.New("no price data")

// Prices is Auctionator's price database of the realm with the most entries, as writs.load_prices reads it:
// item key -> {"a": {day: offers}, "h": {day: highest}, "l": {day: lowest}, "m": minimum price, …}. Every key
// is a string, as Python's str(key) writes it; CBOR arrays are list tables.
func Prices(path string) (*Table, error) {
	_, best, err := PricesWithRealm(path)
	return best, err
}

// PricesWithRealm returns the Auctionator realm key (e.g. "PvP Horde") and price database of the realm with
// the most entries.
func PricesWithRealm(path string) (string, *Table, error) {
	all, bestKey, err := AllRealmPrices(path)
	if err != nil {
		return "", nil, err
	}
	return bestKey, all[bestKey], nil
}

// AllRealmPrices decodes every realm entry in AUCTIONATOR_PRICE_DATABASE and returns all realm tables plus
// the key of the largest realm.
func AllRealmPrices(path string) (map[string]*Table, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	start := indexOf(data, []byte("AUCTIONATOR_PRICE_DATABASE = {"), 0)
	if start < 0 {
		return nil, "", errors.New("subsection not found") // Python's bytes.index
	}
	end := indexOf(data, []byte("\n}"), start)
	if end < 0 {
		return nil, "", errors.New("subsection not found")
	}
	all := map[string]*Table{}
	var bestKey string
	var best *Table
	seen := map[string]bool{}
	for _, m := range realmEntry.FindAllSubmatchIndex(data[start:end], -1) {
		raw, ok := luaString(data, start+m[1]-1)
		if !ok {
			continue
		}
		obj, _, err := cbor(raw, 0)
		if err != nil {
			continue
		}
		t, ok := obj.(*Table)
		if !ok || t.list { // isinstance(obj, dict)
			continue
		}
		realm := DecodeReplace(data[start+m[2] : start+m[3]])
		all[realm] = t
		if seen[realm] {
			// The same realm twice: Python keeps the first place and the last value; only the size counts.
			if best != nil && t.Len() > best.Len() {
				best, bestKey = t, realm
			}
			continue
		}
		seen[realm] = true
		if best == nil || t.Len() > best.Len() {
			best, bestKey = t, realm
		}
	}
	if best == nil {
		return nil, "", fmt.Errorf("%w: %s", ErrNoPrices, i18n.T("No price data found in Auctionator.lua."))
	}
	return all, bestKey, nil
}

func indexOf(data, sub []byte, from int) int {
	for i := from; i+len(sub) <= len(data); i++ {
		if data[i] == sub[0] && string(data[i:i+len(sub)]) == string(sub) {
			return i
		}
	}
	return -1
}

// luaString reads the Lua string literal whose opening quote is at i, with the escapes writs.lua_string
// knows (\ddd with up to three digits, \n \r \t \" \\ \' \a \b \f \v and a backslash before a newline).
func luaString(b []byte, i int) ([]byte, bool) {
	esc := map[byte]byte{'n': 10, 'r': 13, 't': 9, '"': 34, '\\': 92, '\'': 39, 'a': 7, 'b': 8, 'f': 12, 'v': 11,
		'\n': 10}
	var out []byte
	i++
	for i < len(b) {
		c := b[i]
		switch {
		case c == '"':
			return out, true
		case c == '\\':
			if i+1 >= len(b) {
				return nil, false
			}
			n := b[i+1]
			if n >= '0' && n <= '9' {
				j := i + 1
				for j < len(b) && j-i <= 3 && b[j] >= '0' && b[j] <= '9' {
					j++
				}
				v := 0
				for _, d := range b[i+1 : j] {
					v = v*10 + int(d-'0')
				}
				if v > 255 {
					return nil, false // bytearray.append raises
				}
				out = append(out, byte(v))
				i = j
				continue
			}
			e, ok := esc[n]
			if !ok {
				return nil, false
			}
			out = append(out, e)
			i += 2
		default:
			out = append(out, c)
			i++
		}
	}
	return nil, false
}

// cbor decodes one CBOR item as writs.cbor does: maps become tables with str() keys, arrays list tables,
// byte and text strings text (invalid UTF-8 replaced), tags are skipped.
func cbor(b []byte, i int) (any, int, error) {
	if i >= len(b) {
		return nil, i, errors.New("truncated")
	}
	ib := b[i]
	mt, ai := ib>>5, ib&31
	i++
	if mt == 7 {
		switch ai {
		case 20:
			return false, i, nil
		case 21:
			return true, i, nil
		case 22:
			return nil, i, nil
		case 25:
			if i+2 > len(b) {
				return nil, i, errors.New("truncated")
			}
			return halfFloat(binary.BigEndian.Uint16(b[i:])), i + 2, nil
		case 26:
			if i+4 > len(b) {
				return nil, i, errors.New("truncated")
			}
			return float64(math.Float32frombits(binary.BigEndian.Uint32(b[i:]))), i + 4, nil
		case 27:
			if i+8 > len(b) {
				return nil, i, errors.New("truncated")
			}
			return math.Float64frombits(binary.BigEndian.Uint64(b[i:])), i + 8, nil
		}
		return nil, i, fmt.Errorf("simple value %d", ai)
	}
	var v uint64
	if ai >= 24 {
		size := map[byte]int{24: 1, 25: 2, 26: 4, 27: 8}[ai]
		if size == 0 {
			return nil, i, fmt.Errorf("additional information %d", ai)
		}
		if i+size > len(b) {
			return nil, i, errors.New("truncated")
		}
		for _, x := range b[i : i+size] {
			v = v<<8 | uint64(x)
		}
		i += size
	} else {
		v = uint64(ai)
	}
	switch mt {
	case 0:
		return int64(v), i, nil
	case 1:
		return -1 - int64(v), i, nil
	case 2, 3:
		if uint64(len(b)-i) < v {
			// Python slices what is there and goes on; the next item then fails.
			return DecodeReplace(b[i:]), len(b), nil
		}
		return DecodeReplace(b[i : i+int(v)]), i + int(v), nil
	case 4:
		t := NewList()
		for k := uint64(0); k < v; k++ {
			x, j, err := cbor(b, i)
			if err != nil {
				return nil, j, err
			}
			i = j
			t.Set(int64(k+1), keepNone(x))
		}
		return t, i, nil
	case 5:
		t := NewTable()
		for k := uint64(0); k < v; k++ {
			key, j, err := cbor(b, i)
			if err != nil {
				return nil, j, err
			}
			x, j, err := cbor(b, j)
			if err != nil {
				return nil, j, err
			}
			i = j
			t.Set(pyfmt.Str(key), keepNone(x))
		}
		return t, i, nil
	case 6:
		return cbor(b, i)
	}
	return nil, i, fmt.Errorf("CBOR major type %d", mt)
}

// keepNone turns a CBOR null into Null, which a table keeps (Python keeps None in a list or dict).
func keepNone(v any) any {
	if v == nil {
		return Null{}
	}
	return v
}

// Null is a None inside a CBOR array or map that Python keeps (a Lua table cannot hold nil).
type Null struct{}

func halfFloat(h uint16) float64 {
	sign := 1.0
	if h&0x8000 != 0 {
		sign = -1
	}
	exp := int(h>>10) & 0x1f
	frac := float64(h & 0x3ff)
	switch exp {
	case 0:
		return sign * math.Ldexp(frac, -24)
	case 31:
		if frac == 0 {
			return math.Inf(int(sign))
		}
		return math.NaN()
	}
	return sign * math.Ldexp(frac+1024, exp-25)
}

// ScanDate is the newest scan day of the database (writs.scan_date): days since 2020-01-01 in the "a"
// tables; false when there is none.
func ScanDate(prices *Table) (time.Time, bool) {
	best, found := int64(0), false
	for _, v := range prices.Values() {
		entry, ok := v.(*Table)
		if !ok {
			continue
		}
		a, _ := entry.Get("a").(*Table) // `v.get('a') or {}`: a list is walked by its values
		for _, k := range a.Iter() {
			day, err := pyInt(k)
			if err != nil {
				continue
			}
			if !found || day > best {
				best, found = day, true
			}
		}
	}
	if !found {
		return time.Time{}, false
	}
	return time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(best)), true
}

// pyInt is Python's int() of a key text.
func pyInt(k any) (int64, error) {
	switch x := k.(type) {
	case int64:
		return x, nil
	case string:
		var n int64
		_, err := fmt.Sscan(x, &n)
		return n, err
	}
	return 0, errors.New("not a number")
}
