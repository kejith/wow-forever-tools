package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The last line of the text has the time of the computation; the goldens have HH:MM there.
var computedAt = regexp.MustCompile(`(computed|berechnet) \d\d:\d\d\.`)

func TestIngame(t *testing.T) {
	wow := fixture(t)
	for _, lang := range []string{"en", "de"} {
		out, code := runCmd(t, "ingame", "--wow", wow, "--lang", lang)
		if code != 0 {
			t.Fatalf("%s: exit %d", lang, code)
		}
		golden(t, "ingame-"+lang+".txt", computedAt.ReplaceAllString(out, "$1 HH:MM."))
	}
}

func TestIngameJSON(t *testing.T) {
	wow := fixture(t)
	out, code := runCmd(t, "ingame", "--wow", wow, "--char", "Ilse", "--lang", "en", "--json")
	if code != 0 || !strings.HasPrefix(out, `{"payload": {"version": 1, "generated": `) ||
		!strings.Contains(out, `"locale": "deDE", "scanDate": "2026-11-21", "chars": {"Ilse": {"level": 28, `) ||
		!strings.HasSuffix(out, `, "result": null}`+"\n") {
		t.Fatalf("exit %d: %.300s", code, out)
	}
	if !regexp.MustCompile(`"tips": \{"\d+": \["`).MatchString(out) {
		t.Error("tips are not an object of item IDs")
	}
}

// --write only into a copy of the fixture: the first run writes AppData.lua, the second finds it current.
func TestIngameWrite(t *testing.T) {
	src := fixture(t)
	wow := filepath.Join(t.TempDir(), "forever")
	if err := os.CopyFS(wow, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(wow, "Interface", "AddOns", "WoWMCP", "AppData.lua")
	out, code := runCmd(t, "ingame", "--wow", wow, "--char", "Ilse", "--write", "--lang", "en")
	if code != 0 || !strings.HasSuffix(out, "\nWritten to "+file+". Type /reload in the game.\n") {
		t.Fatalf("exit %d: ...%s", code, out[max(0, len(out)-200):])
	}
	data, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(data), "\nWoWMCP_AppData = {") {
		t.Fatalf("%v: %.80s", err, data)
	}
	out, code = runCmd(t, "ingame", "--wow", wow, "--char", "Ilse", "--write", "--lang", "de")
	if code != 0 || !strings.HasSuffix(out, "\nNichts geändert, die Datei im Addon-Ordner ist aktuell.\n") {
		t.Fatalf("second run: exit %d: ...%s", code, out[max(0, len(out)-200):])
	}
	if err := os.RemoveAll(filepath.Join(wow, "Interface")); err != nil {
		t.Fatal(err)
	}
	out, code = runCmd(t, "ingame", "--wow", wow, "--char", "Ilse", "--write", "--lang", "en")
	if code != 0 || !strings.HasSuffix(out, "\nThe WoWMCP addon is not installed.\n") {
		t.Fatalf("no addon: exit %d: ...%s", code, out[max(0, len(out)-200):])
	}
}
