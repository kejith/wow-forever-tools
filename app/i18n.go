package main

// Language of the texts the app itself produces: the error messages of the addon install, the releases, the
// update and the bridge call. The English text is the key, German lives in locale_de.go; a text without a
// translation stays English. The page has its own catalog for its texts (renderer/i18n.js), the bridge has
// its own too (sim/internal/i18n); all three get the same language.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
)

var languages = []string{"en", "de"}

var catalogs = map[string]map[string]string{"de": catalogDE}

var currentLanguage atomic.Value

func knownLanguage(lang string) bool {
	for _, l := range languages {
		if l == lang {
			return true
		}
	}
	return false
}

// setLanguage selects the language of t; an unknown one is English.
func setLanguage(lang string) string {
	if !knownLanguage(lang) {
		lang = "en"
	}
	currentLanguage.Store(lang)
	return lang
}

func language() string {
	if lang, ok := currentLanguage.Load().(string); ok {
		return lang
	}
	return "en"
}

var placeholder = regexp.MustCompile(`\{(\w+)\}`)

// t is `text` in the selected language, with {name} placeholders filled in from name/value pairs:
// t("Unknown action: {action}", "action", a).
func t(text string, pairs ...any) string {
	found := text
	if german, ok := catalogs[language()][text]; ok {
		found = german
	}
	if len(pairs) == 0 {
		return found
	}
	values := map[string]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		values[fmt.Sprint(pairs[i])] = fmt.Sprint(pairs[i+1])
	}
	return placeholder.ReplaceAllStringFunc(found, func(all string) string {
		if v, ok := values[all[1:len(all)-1]]; ok {
			return v
		}
		return all
	})
}

var textLocale = regexp.MustCompile(`(?m)^SET\s+textLocale\s+"(\w+)"`)

// clientLocale is the text locale of the game client, e.g. "deDE", from WTF/Config.wtf; "" if it cannot be read.
func clientLocale(wowDir string) string {
	data, err := os.ReadFile(filepath.Join(wowDir, "WTF", "Config.wtf"))
	if err != nil {
		return ""
	}
	if m := textLocale.FindSubmatch(data); m != nil {
		return string(m[1])
	}
	return ""
}

func languageOf(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "de") {
		return "de"
	}
	return "en"
}
