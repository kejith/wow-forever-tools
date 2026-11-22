package cli

import (
	"strings"
	"testing"
)

// The text has the file's path and modification time, so it is only run.
func TestGoldPerHour(t *testing.T) {
	wow := fixture(t)
	out, code := runCmd(t, "goldperhour", "--wow", wow)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, line := range []string{"{\n  \"found\": true,\n  \"version\": \"0.3.1\",\n", "\n        \"items\": [],\n",
		"\n            \"name\": \"Peacebloom\",\n", "\n  \"herbGathering\": 2366,\n"} {
		if !strings.Contains(out, line) {
			t.Errorf("no %q in\n%s", line, out)
		}
	}
	if !strings.HasSuffix(out, "\n}\n") {
		t.Errorf("end: %q", out[max(0, len(out)-20):])
	}
	out, code = runCmd(t, "goldperhour", "--wow", t.TempDir())
	if code != 0 || !strings.HasPrefix(out, "{\n  \"found\": false,\n  \"settings\": {\n") {
		t.Fatalf("without the addon: exit %d\n%s", code, out)
	}
	if _, code := runCmd(t, "goldperhour", "--wow", wow, "--lang", "en"); code != 2 {
		t.Fatalf("--lang is not an option of the script: exit %d", code)
	}
}

func TestIndentJSON(t *testing.T) {
	in := `{"a": [], "b": {}, "c": [1, {"d": "x, \"y\": [z]"}], "e": {"f": null}}`
	want := "{\n  \"a\": [],\n  \"b\": {},\n  \"c\": [\n    1,\n    {\n      \"d\": \"x, \\\"y\\\": [z]\"\n    }\n  ],\n" +
		"  \"e\": {\n    \"f\": null\n  }\n}"
	if got := string(indentJSON([]byte(in), 2)); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
