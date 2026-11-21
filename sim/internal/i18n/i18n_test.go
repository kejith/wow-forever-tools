package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
)

// literal is the text of a string literal, or of string literals joined with + (a long text over several
// lines).
func literal(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		text, err := strconv.Unquote(x.Value)
		return text, err == nil
	case *ast.ParenExpr:
		return literal(x.X)
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		a, ok1 := literal(x.X)
		b, ok2 := literal(x.Y)
		return a + b, ok1 && ok2
	}
	return "", false
}

// goTexts walks the syntax tree of every Go file of the module (tests and this package left out) for
// i18n.T("…") and i18n.Mark("…") and returns each text with where it is.
func goTexts(t *testing.T) map[string]string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if filepath.Dir(path) == filepath.Join(root, "internal", "i18n") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "i18n" || (sel.Sel.Name != "T" && sel.Sel.Name != "Mark") {
				return true
			}
			text, ok := literal(call.Args[0])
			if !ok {
				if sel.Sel.Name == "Mark" {
					t.Errorf("%s: i18n.Mark needs a string literal", fset.Position(call.Pos()))
				}
				return true
			}
			if _, seen := out[text]; !seen {
				rel, _ := filepath.Rel(root, path)
				out[text] = rel + ":" + strconv.Itoa(fset.Position(call.Args[0].Pos()).Line)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestGoTextsHaveGermanEntries(t *testing.T) {
	de := Catalog("de")
	var missing []string
	for text, where := range goTexts(t) {
		if _, ok := de[text]; !ok {
			missing = append(missing, where+": "+strconv.Quote(text))
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("no German text for (add it to locale_de.json):\n%s", strings.Join(missing, "\n"))
	}
}

func fieldSet(text string) map[pyfmt.Field]bool {
	out := map[pyfmt.Field]bool{}
	for _, f := range pyfmt.Fields(text) {
		out[f] = true
	}
	return out
}

func TestPlaceholdersMatch(t *testing.T) {
	sections, err := Sections()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for section, texts := range sections {
		for en, de := range texts {
			if other, dup := seen[en]; dup {
				t.Errorf("%q is in %q and in %q", en, other, section)
			}
			seen[en] = section
			if strings.TrimSpace(de) == "" {
				t.Errorf("%q has an empty German text", en)
			}
			a, b := fieldSet(en), fieldSet(de)
			if len(a) != len(b) {
				t.Errorf("placeholders differ: %q / %q", en, de)
				continue
			}
			for f := range a {
				if !b[f] {
					t.Errorf("placeholders differ: %q / %q", en, de)
					break
				}
			}
		}
	}
	if len(seen) < 100 {
		t.Fatalf("only %d texts in the catalog", len(seen))
	}
}

func TestLanguageSelection(t *testing.T) {
	wow := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wow, "WTF"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(wow, "WTF", "Config.wtf")
	t.Setenv("WOW_TOOLS_LANG", "")
	defer SetLanguage("en", "")
	if ClientLocale(wow) != "" || SetLanguage("auto", wow) != "en" {
		t.Fatal("no Config.wtf: English")
	}
	write := func(s string) {
		if err := os.WriteFile(config, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("SET portal \"EU\"\nSET textLocale \"deDE\"\nSET audioLocale \"enUS\"\n")
	if ClientLocale(wow) != "deDE" {
		t.Fatalf("client locale %q", ClientLocale(wow))
	}
	if SetLanguage("auto", wow) != "de" || T("Trainer") != "Lehrer" {
		t.Fatal("deDE client: German")
	}
	if SetLanguage("en", wow) != "en" || T("Trainer") != "Trainer" {
		t.Fatal("--lang wins over the client")
	}
	t.Setenv("WOW_TOOLS_LANG", "en")
	if SetLanguage("", wow) != "en" {
		t.Fatal("the environment wins over the client")
	}
	t.Setenv("WOW_TOOLS_LANG", "de")
	if SetLanguage("auto", wow) != "de" || ShortDate("2026-11-14") != "14.11." {
		t.Fatal("German date")
	}
	write("SET textLocale \"frFR\"\n")
	t.Setenv("WOW_TOOLS_LANG", "")
	if SetLanguage("auto", wow) != "en" || ShortDate("2026-11-14") != "11/14" {
		t.Fatal("any other client: English")
	}
	if got := T("only {n} items in the AH", "n", 3); got != "only 3 items in the AH" {
		t.Fatalf("got %q", got)
	}
	if got := T("a text nobody translated"); got != "a text nobody translated" {
		t.Fatalf("got %q", got)
	}
	SetLanguage("de", "")
	if got := T("{base:g}% without gear, the cap against +{target} levels is {cap:g}%: already reached",
		"base", 5.0, "target", 2, "cap", 6.0); got != "5 % ohne Ausrüstung vorhanden, Cap gegen +2 Stufen ist 6 %: schon erreicht" {
		t.Fatalf("German with placeholders: %q", got)
	}
}
