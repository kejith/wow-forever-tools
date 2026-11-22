package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCombatlogGolden(t *testing.T) {
	wow := fixture(t)
	for _, c := range []struct {
		file string
		args []string
	}{
		{"combatlog-en.txt", []string{"--lang", "en"}},
		{"combatlog-de.txt", []string{"--lang", "de", "--gap", "3"}},
		{"combatlog-fight-en.txt", []string{"--lang", "en", "--fight", "1"}},
		{"combatlog-nomodel-de.txt", []string{"--lang", "de", "--no-model", "--fight", "2"}},
	} {
		out, code := runCmd(t, "combatlog", append([]string{"--wow", wow}, c.args...)...)
		if code != 0 {
			t.Fatalf("%s: exit %d", c.file, code)
		}
		golden(t, c.file, out)
	}
}

func TestCombatlogErrors(t *testing.T) {
	wow := fixture(t)
	// The header is printed before the fight is looked for, as the script did.
	out, code := runCmd(t, "combatlog", "--wow", wow, "--lang", "en", "--fight", "99")
	if code != 1 || !strings.HasPrefix(out, "WoWCombatLog-") {
		t.Errorf("--fight 99: exit %d, %q", code, out)
	}
	if out, code := runCmd(t, "combatlog", "--wow", wow, "--char", "Nobody"); code != 1 || out != "" {
		t.Errorf("unknown character: exit %d, %q", code, out)
	}
}

// The JSON is json.dumps(…, indent=1): the same value as the compact one, one item per line.
func TestCombatlogJSON(t *testing.T) {
	wow := fixture(t)
	out, code := runCmd(t, "combatlog", "--wow", wow, "--lang", "en", "--json", "--no-model")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.HasPrefix(out, "{\n \"file\": \"WoWCombatLog-112026_100000.txt\",\n \"modified\": ") {
		t.Errorf("not indented by one space: %.80q", out)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	if v["name"] != "Brakk" || v["model"] != false {
		t.Errorf("name %v, model %v", v["name"], v["model"])
	}
}

func TestCombatlogIndent(t *testing.T) {
	in := `{"a": [], "b": {}, "c": [1, {"d": "x, [y]: \"z\""}], "e": null}`
	want := "{\n \"a\": [],\n \"b\": {},\n \"c\": [\n  1,\n  {\n   \"d\": \"x, [y]: \\\"z\\\"\"\n  }\n ],\n \"e\": null\n}"
	if got := string(combatlogIndent([]byte(in), 1)); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if !bytes.Equal(combatlogIndent([]byte("[]"), 1), []byte("[]")) {
		t.Error("empty list")
	}
}
