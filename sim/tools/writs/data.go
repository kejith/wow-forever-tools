package writs

import (
	"strconv"
	"strings"

	"github.com/kejith/wow-forever-tools/sim/internal/i18n"
)

// Rep is the reputation a writ of a tier gives (writs.REP).
var Rep = map[string]int64{"J": 75, "E": 125, "A": 200}

// tierName are the headings of the writ tiers (writs.TIER_NAME), translated where they are used.
var tierName = map[string]string{
	"J": i18n.Mark("Journeyman (75 reputation)"),
	"E": i18n.Mark("Expert (125 reputation)"),
	"A": i18n.Mark("Artisan (200 reputation)"),
}

// Sealed is the sealed writ of a tier (writs.SEALED).
var Sealed = map[string]int64{"J": 262888, "E": 262889, "A": 262890}

// Factions are the writ factions: Azeroth Commerce Authority (Alliance), Durotar Supply and Logistics (Horde).
var Factions = map[int64]bool{2586: true, 2587: true}

var profession = map[string]string{
	"alchemy":        i18n.Mark("Alchemy"),
	"blacksmithing":  i18n.Mark("Blacksmithing"),
	"engineering":    i18n.Mark("Engineering"),
	"leatherworking": i18n.Mark("Leatherworking"),
	"tailoring":      i18n.Mark("Tailoring"),
}

// writsTable is writs.WRITS: writ item ID | tier | item it asks for | amount | profession (wow.gg, Nov 2026).
const writsTable = `264012|J|3383|1|alchemy
264019|J|5996|1|alchemy
264013|J|3385|1|alchemy
264017|J|3388|1|alchemy
264014|J|6048|1|alchemy
264016|J|3390|1|alchemy
264011|J|3391|1|alchemy
264015|J|5634|1|alchemy
264018|J|8951|1|alchemy
264062|E|3827|1|alchemy
264057|E|3823|1|alchemy
264064|E|3824|1|alchemy
264061|E|5633|1|alchemy
264056|E|6050|1|alchemy
264066|E|8949|1|alchemy
264059|E|6052|1|alchemy
264063|E|6049|1|alchemy
264060|E|10592|1|alchemy
264065|E|9061|1|alchemy
264067|E|9144|1|alchemy
264058|E|4623|1|alchemy
264117|A|12190|1|alchemy
264120|A|9088|1|alchemy
264124|A|246948|1|alchemy
264116|A|9224|1|alchemy
264119|A|9233|1|alchemy
264122|A|3387|1|alchemy
264118|A|13457|1|alchemy
264123|A|18253|1|alchemy
264121|A|13513|1|alchemy
264023|J|15869|1|blacksmithing
264027|J|3848|1|blacksmithing
264020|J|5540|1|blacksmithing
264025|J|2865|1|blacksmithing
264021|J|11128|1|blacksmithing
264024|J|3491|1|blacksmithing
264022|J|5541|1|blacksmithing
264028|J|3484|1|blacksmithing
264026|J|3485|1|blacksmithing
264075|E|7964|1|blacksmithing
264073|E|3853|1|blacksmithing
264077|E|11144|1|blacksmithing
264070|E|6041|1|blacksmithing
264074|E|3837|1|blacksmithing
264069|E|9060|1|blacksmithing
264078|E|9366|1|blacksmithing
264068|E|7922|1|blacksmithing
264079|E|7941|1|blacksmithing
264071|E|7928|1|blacksmithing
264072|E|7942|1|blacksmithing
264076|E|7943|1|blacksmithing
264127|A|7929|1|blacksmithing
264128|A|7969|1|blacksmithing
264132|A|15872|1|blacksmithing
264126|A|12416|1|blacksmithing
264133|A|7947|1|blacksmithing
264131|A|12775|1|blacksmithing
264125|A|12417|1|blacksmithing
264129|A|12422|1|blacksmithing
264130|A|20549|1|blacksmithing
264033|J|6712|1|engineering
264037|J|8069|200|engineering
264036|J|4380|1|engineering
264034|J|4373|1|engineering
264035|J|7506|1|engineering
264029|J|5507|1|engineering
264032|J|4381|1|engineering
264031|J|4383|1|engineering
264030|J|4384|1|engineering
264089|E|10498|1|engineering
264081|E|4392|1|engineering
264086|E|4852|1|engineering
264085|E|10560|1|engineering
264090|E|11590|1|engineering
264082|E|4393|1|engineering
264083|E|4395|1|engineering
264088|E|10514|1|engineering
264080|E|4396|1|engineering
264087|E|10500|1|engineering
264084|E|10501|1|engineering
264091|E|16005|1|engineering
264141|A|10548|1|engineering
264135|A|10504|1|engineering
264140|A|18631|1|engineering
264137|A|18645|1|engineering
264136|A|15996|1|engineering
264138|A|18587|1|engineering
264134|A|16023|1|engineering
264139|A|18232|1|engineering
264142|A|21277|1|engineering
264041|J|2317|1|leatherworking
264039|J|5958|1|leatherworking
264046|J|7284|1|leatherworking
264038|J|18662|1|leatherworking
264040|J|7349|1|leatherworking
264042|J|7358|1|leatherworking
264044|J|4252|1|leatherworking
264043|J|7371|1|leatherworking
264045|J|4254|1|leatherworking
264093|E|5965|1|leatherworking
264094|E|7378|1|leatherworking
264092|E|8174|1|leatherworking
264098|E|7391|1|leatherworking
264099|E|8192|1|leatherworking
264102|E|8189|1|leatherworking
264101|E|8200|1|leatherworking
264096|E|8210|1|leatherworking
264100|E|8201|1|leatherworking
264095|E|8214|1|leatherworking
264097|E|8217|1|leatherworking
264103|E|8218|1|leatherworking
264147|A|15084|1|leatherworking
264150|A|15092|1|leatherworking
264151|A|15071|1|leatherworking
264145|A|15053|1|leatherworking
264143|A|15087|1|leatherworking
264144|A|18258|1|leatherworking
264146|A|15094|1|leatherworking
264148|A|15075|1|leatherworking
264149|A|15080|1|leatherworking
264052|J|4313|1|tailoring
264053|J|10048|1|tailoring
264048|J|6263|1|tailoring
264054|J|6264|1|tailoring
264051|J|4317|1|tailoring
264047|J|5766|1|tailoring
264049|J|7048|1|tailoring
264055|J|4321|1|tailoring
264050|J|4245|1|tailoring
264115|E|7061|1|tailoring
264106|E|7063|1|tailoring
264109|E|4329|1|tailoring
264110|E|9998|1|tailoring
264113|E|10008|1|tailoring
264112|E|10052|1|tailoring
264107|E|10004|1|tailoring
264111|E|10009|1|tailoring
264104|E|10050|1|tailoring
264105|E|10023|1|tailoring
264108|E|22246|1|tailoring
264114|E|10042|1|tailoring
264155|A|10036|1|tailoring
264158|A|10025|1|tailoring
264153|A|14046|1|tailoring
264160|A|21340|1|tailoring
264159|A|14141|1|tailoring
264157|A|22251|1|tailoring
264154|A|13864|1|tailoring
264156|A|14108|1|tailoring
264152|A|14130|1|tailoring`

// WritDef is one line of the writ table.
type WritDef struct {
	ID         int64  // the writ item
	Tier       string // J, E or A
	Item       int64  // the item it asks for
	Qty        int64
	Profession string // alchemy, blacksmithing, …
}

// WritList are the writs in the order of the table.
var WritList = func() []WritDef {
	var out []WritDef
	for _, line := range strings.Split(writsTable, "\n") {
		f := strings.Split(line, "|")
		out = append(out, WritDef{mustInt(f[0]), f[1], mustInt(f[2]), mustInt(f[3]), f[4]})
	}
	return out
}()

// PouchEstimate is the coin pouch per tier in copper until the addon measured one (writs.POUCH_ESTIMATE).
var PouchEstimate = map[string]int64{"J": 3300, "E": 7000, "A": 7500}

var pouchEstimateSource = i18n.Mark("estimate from conquestcapped.com (Nov 2026): Journeyman 22–45 s, Expert " +
	"50–90 s, Artisan about 75 s")

// FavorCurrency is Merchant's Favor.
const FavorCurrency = 3402

// CrateTiers are the crate tiers in order; L is Apprentice.
const CrateTiers = "LJEA"

var crateTierName = map[string]string{
	"L": i18n.Mark("Apprentice"), "J": i18n.Mark("Journeyman"), "E": i18n.Mark("Expert"), "A": i18n.Mark("Artisan"),
}

// CrateLevel is the character level a crate tier needs.
var CrateLevel = map[string]int64{"L": 10, "J": 10, "E": 20, "A": 35}

// CrateFavor is the favor per sealed crate (common, uncommon); Artisan is extrapolated (CrateFavorGuess).
var CrateFavor = map[string][2]int64{"L": {5, 10}, "J": {10, 20}, "E": {15, 30}, "A": {20, 40}}

// CrateFavorGuess are the tiers whose favor is a guess.
var CrateFavorGuess = map[string]bool{"A": true}

// SealedCrate is the sealed crate of a tier: common, uncommon.
var SealedCrate = func() map[string][2]int64 {
	out := map[string][2]int64{}
	for i, t := range CrateTiers {
		out[string(t)] = [2]int64{248765 + int64(i), 248770 + int64(i)}
	}
	return out
}()

var crateKind = map[string]string{
	"herbs": i18n.Mark("Herbs"), "ore": i18n.Mark("Ore"), "textiles": i18n.Mark("Textiles"),
	"curiosities": i18n.Mark("Curiosities"), "fabrics": i18n.Mark("Fabrics"), "ingots": i18n.Mark("Ingots"),
	"parts": i18n.Mark("Parts"), "earthly_herbs": i18n.Mark("Earthly herbs"),
	"flowering_herbs": i18n.Mark("Flowering herbs"),
}

// cratesTable is writs.CRATES: crate item | tier | kind | bundles as item ID:amount, separated by ;.
const cratesTable = `248682|L|herbs|2447:20;765:20;2449:10;785:10;2450:10
248683|L|ore|2770:20;2771:20
248684|L|textiles|2589:40;2318:20
248685|L|curiosities|10940:20;10939:4;10978:6;11084:2
248702|L|fabrics|4231:2;2996:20
248703|L|ingots|2840:20;3576:20;2841:10
248704|L|parts|4359:16;4361:16;4363:4
248686|J|herbs|3820:10;2453:10;3355:10;3369:10;3356:10
248687|J|ore|2775:10;2772:20
248688|J|textiles|2592:20;4306:40;2319:20
248689|J|curiosities|11083:20;11082:4;11138:6;11139:2
248705|J|fabrics|2997:6;4305:10;4233:2
248706|J|ingots|2842:10;3575:20
248707|J|parts|4371:8;4375:7;4382:4;10558:45
248690|E|earthly_herbs|3357:20;3818:10;3821:10;3358:10
248691|E|flowering_herbs|3819:10;4625:10;8831:20;8836:10
248692|E|ore|2776:10;3858:20
248693|E|textiles|4338:40;4234:20
248694|E|curiosities|11137:20;11135:4;11177:6;11178:2
248708|E|fabrics|4339:8;4236:2
248709|E|ingots|3860:20;3859:15;3577:10
248710|E|parts|4387:8;10559:5;10560:12;10561:5
248695|A|earthly_herbs|13464:10;8838:20;8839:10;8845:10;8846:10
248696|A|flowering_herbs|13463:20;13465:20;13466:20;13467:20
248697|A|ore|7911:10;10620:20;11370:20
248698|A|textiles|14047:40;4304:20;8170:20
248699|A|curiosities|11176:20;11175:4;14343:6;14344:2
248711|A|fabrics|14048:8;8172:2
248712|A|ingots|11371:2;6037:10;12359:20
248713|A|parts|15994:5;18631:5;16000:2`

// CrateDef is one line of the crate table (writs.crate_list).
type CrateDef struct {
	ID      int64
	Tier    string
	Kind    string
	Bundles [][2]int64 // item, amount
}

// CrateList are the crates in the order of the table.
var CrateList = func() []CrateDef {
	var out []CrateDef
	for _, line := range strings.Split(cratesTable, "\n") {
		f := strings.Split(line, "|")
		c := CrateDef{ID: mustInt(f[0]), Tier: f[1], Kind: f[2]}
		for _, o := range strings.Split(f[3], ";") {
			p := strings.Split(o, ":")
			c.Bundles = append(c.Bundles, [2]int64{mustInt(p[0]), mustInt(p[1])})
		}
		out = append(out, c)
	}
	return out
}()

func mustInt(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		panic(err)
	}
	return n
}
