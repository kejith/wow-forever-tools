// Package i18n is the Go side of scripts/i18n.py: the English source text is the key, T("only {n} in the
// AH", "n", 3) looks it up in the catalog of the selected language and fills in the placeholders the way
// Python's str.format does. The German catalog is locale_de.json in this folder; the Python scripts read the
// same file (scripts/locale_de.py), so both sides translate alike while the scripts move to Go.
//
// The language follows the game client unless it is set: --lang en|de, the environment variable
// WOW_TOOLS_LANG, or textLocale in WTF/Config.wtf (deDE -> German, anything else English).
package i18n

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kejith/wow-forever-tools/sim/internal/pyfmt"
)

//go:embed locale_de.json
var germanJSON []byte

// Languages are the languages the texts exist in; English is the source.
var Languages = []string{"en", "de"}

var (
	language = "en"
	catalog  map[string]string
	textLoc  = regexp.MustCompile(`(?m)^SET\s+textLocale\s+"(\w+)"`)
)

// Sections is the German catalog as it is in the file: section (the script or package that uses the texts)
// -> English -> German.
func Sections() (map[string]map[string]string, error) {
	var s map[string]map[string]string
	err := json.Unmarshal(germanJSON, &s)
	return s, err
}

// Catalog is the merged catalog of a language (empty for English).
func Catalog(lang string) map[string]string {
	out := map[string]string{}
	if lang != "de" {
		return out
	}
	s, err := Sections()
	if err != nil {
		return out
	}
	for _, texts := range s {
		for en, de := range texts {
			out[en] = de
		}
	}
	return out
}

// ClientLocale is the text locale of the game client, e.g. "deDE", from WTF/Config.wtf; "" if it cannot be
// read.
func ClientLocale(wow string) string {
	if wow == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(wow, "WTF", "Config.wtf"))
	if err != nil {
		return ""
	}
	if m := textLoc.FindSubmatch(data); m != nil {
		return string(m[1])
	}
	return ""
}

// LanguageOf is the language for a client locale: "de" for deDE, "en" for everything else.
func LanguageOf(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "de") {
		return "de"
	}
	return "en"
}

func known(lang string) bool {
	for _, l := range Languages {
		if l == lang {
			return true
		}
	}
	return false
}

// SetLanguage selects the language: lang if it is one of Languages, else WOW_TOOLS_LANG, else the client's.
func SetLanguage(lang, wow string) string {
	choice := strings.ToLower(lang)
	if !known(choice) {
		choice = strings.ToLower(os.Getenv("WOW_TOOLS_LANG"))
	}
	if !known(choice) {
		choice = LanguageOf(ClientLocale(wow))
	}
	language = choice
	catalog = Catalog(choice)
	return choice
}

// Language is the selected language.
func Language() string { return language }

// Mark marks a text for translation without translating it yet: a text that is looked up through a variable
// (scripts/test_i18n.py and the test of this package find it at the Mark call).
func Mark(text string) string { return text }

// T is text in the selected language with its {name} placeholders filled in from name/value pairs:
// T("{n} items", "n", 3). Without values the text stays as it is, braces included, as in Python.
func T(text string, pairs ...any) string {
	if de, ok := catalog[text]; ok {
		text = de
	}
	if len(pairs) == 0 {
		return text
	}
	values := make(map[string]any, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		if name, ok := pairs[i].(string); ok {
			values[name] = pairs[i+1]
		}
	}
	return pyfmt.Format(text, values)
}

// ShortDate is the day and month of an ISO date the way the language writes it: 11/14 or 14.11.
func ShortDate(iso string) string {
	if len(iso) < 10 {
		return iso
	}
	month, day := iso[5:7], iso[8:10]
	if language == "de" {
		return day + "." + month + "."
	}
	return month + "/" + day
}
