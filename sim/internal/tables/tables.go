// Package tables is the Go side of scripts/icons.table: the DB2 tables of the game as CSV from wago.tools,
// downloaded once per build (and per locale for the tables with text) into the cache folder the scripts and
// the engine share. Reading a table is sim/db2's job; this package only makes sure the file is there.
package tables

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Localized are the tables with text: cached per client locale (icons.LOCALIZED).
var Localized = map[string]bool{"ItemSparse": true, "SpellName": true}

// DefaultLocale is the locale of a localized table when none is given (icons.DEFAULT_LOCALE).
const DefaultLocale = "enUS"

// userAgent is what icons._get sends.
const userAgent = "Mozilla/5.0 (wow-forever-tools)"

// CacheDir is icons.CACHE: WOW_TOOLS_CACHE, else scripts/cache of the repo the binary was built in (the
// binary lives in sim/), else scripts/cache below the working folder.
func CacheDir() string {
	if dir := os.Getenv("WOW_TOOLS_CACHE"); dir != "" {
		return dir
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Join(filepath.Dir(exe), "..", "scripts", "cache")
		if st, err := os.Stat(filepath.Join(dir, "..")); err == nil && st.IsDir() {
			return filepath.Clean(dir)
		}
	}
	return filepath.Join("scripts", "cache")
}

// Path is where a table of a build is cached: <cache>/<name>-<build>[-<locale>].csv.
func Path(cache, name, build, locale string) string {
	if Localized[name] {
		if locale == "" {
			locale = DefaultLocale
		}
		return filepath.Join(cache, fmt.Sprintf("%s-%s-%s.csv", name, build, locale))
	}
	return filepath.Join(cache, fmt.Sprintf("%s-%s.csv", name, build))
}

// IsTable is icons.is_table: a CSV export with an ID column. wago.tools answers an unknown build or table
// with a web page; ID is not always the first column (SkillLineAbility starts with its text columns).
func IsTable(data []byte) bool {
	line := data
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		line = data[:i]
	}
	header := bytes.Split(bytes.TrimSpace(line), []byte(","))
	for _, h := range header {
		if string(h) == "ID" {
			return len(header) > 1
		}
	}
	return false
}

// client fetches the tables; a variable so that tests can point it elsewhere.
var client = &http.Client{Timeout: 60 * time.Second}

// baseURL is wago.tools; tests replace it.
var baseURL = "https://wago.tools"

// Ensure downloads a table of a build into the cache unless it is there. Like icons.table it does not fail:
// without a network the file stays missing, and whoever reads it says so.
func Ensure(cache, name, build, locale string) {
	if build == "" {
		return
	}
	path := Path(cache, name, build, locale)
	if _, err := os.Stat(path); err == nil {
		return
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return
	}
	url := fmt.Sprintf("%s/db2/%s/csv?build=%s", baseURL, name, build)
	if Localized[name] {
		if locale == "" {
			locale = DefaultLocale
		}
		url += "&locale=" + locale
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK || !IsTable(data) {
		return
	}
	// Several processes may fetch the same table at once: write it in one step.
	part := fmt.Sprintf("%s.%d.part", path, os.Getpid())
	if err := os.WriteFile(part, data, 0o644); err != nil {
		return
	}
	if err := os.Rename(part, path); err != nil {
		_ = os.Remove(part)
	}
}
