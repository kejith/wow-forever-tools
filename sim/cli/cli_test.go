package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The text of a command on the test folder (scripts/fixtures/forever) is compared with what the Python script
// printed for the same arguments, saved in testdata/ before the scripts were removed. A command whose text
// depends on the current time is only run, not compared.

// fixture is the test WoW folder; it also points the table cache at the repo's scripts/cache (CI restores
// it there), not at a folder below the test's working directory.
func fixture(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("WOW_TOOLS_CACHE") == "" {
		t.Setenv("WOW_TOOLS_CACHE", filepath.Join(root, "scripts", "cache"))
	}
	t.Setenv("WOW_FOREVER_DIR", "")
	t.Setenv("WOW_TOOLS_LANG", "")
	return filepath.Join(root, "scripts", "fixtures", "forever")
}

// runCmd runs a registered command and returns its text and exit code.
func runCmd(t *testing.T, name string, args ...string) (string, int) {
	t.Helper()
	c, ok := Lookup(name)
	if !ok {
		t.Fatalf("no command %s", name)
	}
	var out bytes.Buffer
	code := Main(c, args, &out)
	return out.String(), code
}

// golden compares a command's text with testdata/<file> (Python's text for the same arguments).
func golden(t *testing.T, file, got string) {
	t.Helper()
	want, err := os.ReadFile(filepath.Join("testdata", file))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		gl, wl := bytes.Split([]byte(got), []byte("\n")), bytes.Split(want, []byte("\n"))
		for i := 0; i < len(gl) || i < len(wl); i++ {
			var g, w []byte
			if i < len(gl) {
				g = gl[i]
			}
			if i < len(wl) {
				w = wl[i]
			}
			if !bytes.Equal(g, w) {
				t.Errorf("%s: line %d\n got: %s\nwant: %s", file, i+1, g, w)
				return
			}
		}
	}
}

func TestEngine(t *testing.T) {
	if _, ok := Lookup("engine"); !ok {
		t.Fatal("engine is not registered")
	}
}
