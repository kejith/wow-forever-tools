package cli

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// The text of a command on the test folder (scripts/fixtures/forever) is compared with what the Python script
// printed for the same arguments, saved in testdata/ before the scripts were removed. A command whose text
// depends on the current time is only run, not compared.

// The copy of the test folder all tests of this package read, made by the first one that asks for it.
var (
	fixtureOnce sync.Once
	fixtureDir  string
	fixtureErr  error
)

// TestMain removes the copy of the test folder.
func TestMain(m *testing.M) {
	code := m.Run()
	if fixtureDir != "" {
		os.RemoveAll(filepath.Dir(fixtureDir))
	}
	os.Exit(code)
}

// fixture is the test WoW folder; it also points the table cache at the repo's scripts/cache (CI restores
// it there), not at a folder below the test's working directory.
//
// The tests read a copy whose files have fixed times: the readers put the character with the newest snapshot
// first, git keeps no file times, and a checkout can give two files the same one (on Windows in CI Brakk
// then came before Ilse, and the texts did not match). The copy's files are a second apart in the order of
// their paths, which is the order a checkout writes them in and the one the texts were recorded with.
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
	fixtureOnce.Do(func() {
		fixtureDir, fixtureErr = copyFixture(filepath.Join(root, "scripts", "fixtures", "forever"))
	})
	if fixtureErr != nil {
		t.Fatal(fixtureErr)
	}
	return fixtureDir
}

// copyFixture copies the test folder into a temporary one and gives its files rising times in the order of
// their paths.
func copyFixture(src string) (string, error) {
	tmp, err := os.MkdirTemp("", "wowtools-cli-")
	if err != nil {
		return "", err
	}
	dst := filepath.Join(tmp, "forever")
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		return dst, err
	}
	at := time.Unix(1795315076, 0) // 2026-11-21, the day of the snapshots
	return dst, filepath.WalkDir(dst, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		at = at.Add(time.Second)
		return os.Chtimes(path, at, at)
	})
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
// With UPDATE_GOLDEN=1 it writes the current output to testdata/<file>.
func golden(t *testing.T, file, got string) {
	t.Helper()
	path := filepath.Join("testdata", file)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
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
