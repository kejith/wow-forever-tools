package main

// The German catalog against the source: every text handed to t() needs an entry in locale_de.go with the
// same placeholders, and the catalog must not hold entries nothing uses any more. The page's catalog has its
// own test (renderer.test.js).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// literal is a string literal, or literals joined with +; ok is false for anything else.
func literal(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(x.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		a, okA := literal(x.X)
		b, okB := literal(x.Y)
		return a + b, okA && okB
	}
	return "", false
}

func sortedMatches(text string, re *regexp.Regexp) string {
	m := re.FindAllString(text, -1)
	slices.Sort(m)
	return strings.Join(m, " ")
}

func TestCatalog(t *testing.T) {
	sources, _ := filepath.Glob("*.go")
	used := map[string]string{} // text -> where
	fset := token.NewFileSet()
	for _, file := range sources {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			if id, ok := call.Fun.(*ast.Ident); !ok || id.Name != "t" {
				return true
			}
			text, ok := literal(call.Args[0])
			if !ok {
				t.Errorf("%s: t() needs a string literal", fset.Position(call.Pos()))
				return true
			}
			if _, seen := used[text]; !seen {
				used[text] = fset.Position(call.Pos()).String()
			}
			return true
		})
	}
	if len(used) < 30 {
		t.Fatalf("only %d texts found, the scan is broken", len(used))
	}
	for text, where := range used {
		if _, ok := catalogDE[text]; !ok {
			t.Errorf("%s: no German text for %q", where, text)
		}
	}
	for text, german := range catalogDE {
		if _, ok := used[text]; !ok {
			t.Errorf("locale_de.go: entry nothing uses: %q", text)
		}
		if strings.TrimSpace(german) == "" {
			t.Errorf("locale_de.go: empty entry for %q", text)
		}
		if sortedMatches(german, placeholder) != sortedMatches(text, placeholder) {
			t.Errorf("locale_de.go: placeholders differ in %q", text)
		}
	}
}

// No German left in the code: umlauts only belong into the catalog (and into test data).
func TestNoGermanInCode(t *testing.T) {
	german := regexp.MustCompile(`[äöüÄÖÜß]`)
	files, _ := filepath.Glob("*.go")
	more, _ := filepath.Glob("build/*/*.go")
	for _, file := range append(files, more...) {
		if file == "locale_de.go" || strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if german.MatchString(line) {
				t.Errorf("%s:%d: German text outside the catalog: %s", file, i+1, line)
			}
		}
	}
}

func TestLanguage(t *testing.T) {
	defer setLanguage("en")
	setLanguage("en")
	if got := t_("Unknown action: {action}", "action", "x"); got != "Unknown action: x" {
		t.Fatal(got)
	}
	if setLanguage("de") != "de" || t_("Unknown action: {action}", "action", "x") != "Unbekannte Aktion: x" {
		t.Fatal("German")
	}
	if t_("a text nobody translated") != "a text nobody translated" {
		t.Fatal("fallback")
	}
	if setLanguage("fr") != "en" { // unknown languages fall back to English
		t.Fatal("unknown language")
	}
	for locale, want := range map[string]string{"deDE": "de", "de-AT": "de", "enUS": "en", "": "en"} {
		if languageOf(locale) != want {
			t.Errorf("languageOf(%q)", locale)
		}
	}

	// The client's language comes from WTF/Config.wtf.
	wow := t.TempDir()
	if clientLocale(wow) != "" {
		t.Fatal("locale without a client")
	}
	writeTree(t, wow, map[string]string{"WTF/Config.wtf": "SET portal \"EU\"\r\nSET textLocale \"deDE\"\r\nSET audioLocale \"enUS\"\r\n"})
	if clientLocale(wow) != "deDE" {
		t.Fatal(clientLocale(wow))
	}
}

// t_ is the package's t: inside a test, t is the *testing.T.
var t_ = t
