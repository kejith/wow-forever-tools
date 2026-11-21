// parity runs every bridge command through both implementations, the Python bridge
// (scripts/app_bridge.py) and the Go one (wowtools bridge), against the same WoW folder and reports where
// their answers differ. It is the check of the migration to Go (notes/go-migration.md): a command counts as
// ported when its answers agree here.
//
//	cd sim && go run ./cmd/parity                      the test folder scripts/fixtures/forever
//	cd sim && go run ./cmd/parity --wow "<_forever_>"  real data (the answers stay in scripts/cache/parity)
//
// Each case runs the Python bridge twice; what differs between those two runs (the time of the answer,
// ages) is left out of the comparison with Go. A command the Go side does not have yet is reported as
// "not ported" and costs nothing. The exit code is 1 when a ported case differs or an answer is not JSON.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

type options struct {
	root, wow, label, uv, goBin, out string
	langs, only                      []string
	tol                              float64
	ignore                           *regexp.Regexp
	once, verbose                    bool
	// strictOrder makes a different key order a difference. Off by default: the Python bridge reads the
	// SavedVariables through lupa, and Lua 5.4 seeds its string hashes per process, so the key order of
	// everything it passes on from a snapshot changes from run to run. Values and list order always count.
	strictOrder bool
	maxDiffs    int
}

// testCase is one bridge call: a command with its arguments, in one language.
type testCase struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Lang    string   `json:"lang"`
}

type result struct {
	Case       testCase     `json:"case"`
	Status     string       `json:"status"` // equal, differs, not ported, python failed, go failed
	PythonMs   float64      `json:"pythonMs"`
	GoMs       float64      `json:"goMs,omitempty"`
	Volatile   int          `json:"volatile"`   // paths that differ between two Python runs
	OrderNotes int          `json:"orderNotes"` // objects whose keys come in another order (see strictOrder)
	Diffs      []difference `json:"diffs,omitempty"`
	ErrorText  string       `json:"error,omitempty"` // the error of an answer, also when both agree on it
	Detail     string       `json:"detail,omitempty"`
}

func main() {
	o := options{}
	var langs, only, ignore string
	flag.StringVar(&o.wow, "wow", "", "WoW folder (_forever_); default: a copy of scripts/fixtures/forever")
	flag.StringVar(&o.uv, "uv", "uv", "uv, which runs the Python bridge")
	flag.StringVar(&o.goBin, "go-bin", "", "the wowtools binary; default: built from this module")
	flag.StringVar(&o.out, "out", "", "folder for the answers; default: scripts/cache/parity/<fixture|wow>")
	flag.StringVar(&langs, "lang", "en,de", "languages, comma-separated")
	flag.StringVar(&only, "only", "", "only these commands, comma-separated")
	portedOnly := flag.Bool("ported", false, "only the commands the Go side has (CI)")
	flag.StringVar(&ignore, "ignore", `^generated$`, "regular expression of answer paths to leave out")
	flag.Float64Var(&o.tol, "tol", 1e-9, "relative tolerance for numbers")
	flag.BoolVar(&o.once, "once", false, "run the Python bridge only once (no check for volatile fields)")
	flag.BoolVar(&o.verbose, "v", false, "print every difference, not only the first ones")
	flag.BoolVar(&o.strictOrder, "strict-order", false, "count a different key order as a difference")
	flag.IntVar(&o.maxDiffs, "max-diffs", 8, "differences printed per case")
	flag.Parse()
	o.langs = split(langs)
	o.only = split(only)
	var err error
	if o.ignore, err = regexp.Compile(ignore); err != nil {
		fail("bad --ignore: %v", err)
	}
	if o.root, err = repoRoot(); err != nil {
		fail("%v", err)
	}

	if o.wow == "" {
		o.label = "fixture"
		tmp, err := os.MkdirTemp("", "parity-wow-")
		if err != nil {
			fail("%v", err)
		}
		defer os.RemoveAll(tmp)
		o.wow = filepath.Join(tmp, "_forever_")
		if err := copyFixture(filepath.Join(o.root, "scripts", "fixtures", "forever"), o.wow); err != nil {
			fail("copying the fixture: %v", err)
		}
	} else {
		o.label = "wow"
	}
	if o.out == "" {
		o.out = filepath.Join(o.root, "scripts", "cache", "parity", o.label)
	}
	if err := os.MkdirAll(o.out, 0o755); err != nil {
		fail("%v", err)
	}

	if o.goBin == "" {
		tmp, err := os.MkdirTemp("", "parity-bin-")
		if err != nil {
			fail("%v", err)
		}
		defer os.RemoveAll(tmp)
		o.goBin = filepath.Join(tmp, "wowtools"+exe())
		build := exec.Command("go", "build", "-o", o.goBin, "./cmd/wowtools")
		build.Dir = filepath.Join(o.root, "sim")
		build.Stderr = os.Stderr
		if err := build.Run(); err != nil {
			fail("building wowtools: %v", err)
		}
	}
	ported, err := portedCommands(o.goBin)
	if err != nil {
		fail("%v", err)
	}
	if *portedOnly {
		o.only = nil
		for c := range ported {
			o.only = append(o.only, c)
		}
		if len(o.only) == 0 {
			fmt.Println("parity: the Go side has no bridge command yet")
			return
		}
	}

	cases, err := buildCases(o)
	if err != nil {
		fail("%v", err)
	}
	fmt.Printf("parity: %d cases against %s (%s), Go has %d of the bridge commands\n\n",
		len(cases), o.wow, o.label, len(ported))
	var results []result
	failed := false
	for _, c := range cases {
		r := runCase(o, c, ported[c.Command])
		results = append(results, r)
		printResult(o, r)
		if r.Status != "equal" && r.Status != "not ported" {
			failed = true
		}
	}
	printSummary(results)
	report, _ := json.MarshalIndent(results, "", "  ")
	_ = os.WriteFile(filepath.Join(o.out, "report.json"), report, 0o644)
	fmt.Printf("\nanswers and report.json in %s\n", o.out)
	if failed {
		os.Exit(1)
	}
}

func split(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "parity: "+format+"\n", a...)
	os.Exit(2)
}

func exe() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// repoRoot is the folder with scripts/app_bridge.py, found from the working directory upwards.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "scripts", "app_bridge.py")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("scripts/app_bridge.py not found above the working directory")
		}
		dir = parent
	}
}

// copyFixture copies the test folder and gives its files fixed ages: the readers sort characters by the
// time their snapshot was written, and git does not keep file times. Character folders in alphabetical order
// get older and older (the first one is the most recently played), everything else is older than all of them.
func copyFixture(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("%s is missing (uv run scripts/fixtures/make_forever.py writes it)", src)
	}
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	var chars []string
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
		if d.Name() == "WoWMCP.lua" && filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path)))) != "Account" {
			chars = append(chars, target)
		}
		return os.Chtimes(target, base, base)
	})
	if err != nil {
		return err
	}
	sort.Strings(chars)
	for i, path := range chars {
		t := base.Add(time.Duration(len(chars)-i) * 10 * time.Minute)
		if err := os.Chtimes(path, t, t); err != nil {
			return err
		}
	}
	return nil
}

func portedCommands(bin string) (map[string]bool, error) {
	out, err := exec.Command(bin, "bridge", "--list").Output()
	if err != nil {
		return nil, fmt.Errorf("%s bridge --list: %v", bin, err)
	}
	var list []string
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("%s bridge --list: %v", bin, err)
	}
	m := map[string]bool{}
	for _, c := range list {
		m[c] = true
	}
	return m, nil
}

// buildCases is the matrix: the account-wide commands, then per character of the folder every command with
// the options the app uses, each in every language.
func buildCases(o options) ([]testCase, error) {
	data, _, err := runPython(o, "status", nil, "en")
	if err != nil {
		return nil, fmt.Errorf("status: %v", err)
	}
	status, err := parseJSON(data)
	if err != nil {
		return nil, fmt.Errorf("status is not JSON: %v", err)
	}
	var chars []string
	if list := status.get("characters"); list != nil {
		for _, ch := range list.vals {
			if name := ch.get("name"); name != nil && name.kind == 's' {
				chars = append(chars, name.str)
			}
		}
	}
	type spec struct {
		cmd  string
		args []string
	}
	account := []spec{{"status", nil}, {"goldperhour", nil}, {"pricehistory", nil}, {"ingame", nil},
		{"icons", []string{"--ids", "9492,10033"}}}
	perChar := []spec{
		{"character", nil},
		{"writs", nil}, {"writs", []string{"--owned-at-ah-price"}},
		{"upgrades", nil}, {"upgrades", []string{"--engine", "off"}}, {"upgrades", []string{"--consumables"}},
		{"upgrades", []string{"--max-price", "1", "--sort", "price"}},
		{"recipes", nil},
		{"consumables", nil}, {"consumables", []string{"--engine", "off"}},
		{"rotations", nil}, {"rotations", []string{"--consumables"}},
		{"healing", nil}, {"damage", nil}, {"combatlog", nil},
	}
	var specs []spec
	specs = append(specs, account...)
	for _, ch := range chars {
		for _, s := range perChar {
			specs = append(specs, spec{s.cmd, append([]string{"--char", ch}, s.args...)})
		}
	}
	var cases []testCase
	for _, lang := range o.langs {
		for _, s := range specs {
			if len(o.only) > 0 && !contains(o.only, s.cmd) {
				continue
			}
			name := strings.Join(append([]string{s.cmd}, s.args...), " ") + " (" + lang + ")"
			cases = append(cases, testCase{Name: name, Command: s.cmd, Args: s.args, Lang: lang})
		}
	}
	return cases, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func runPython(o options, cmd string, args []string, lang string) ([]byte, time.Duration, error) {
	full := append([]string{"run", "--quiet", filepath.Join(o.root, "scripts", "app_bridge.py"), cmd}, args...)
	full = append(full, "--wow", o.wow, "--lang", lang)
	return timed(o, exec.Command(o.uv, full...))
}

func runGo(o options, cmd string, args []string, lang string) ([]byte, time.Duration, error) {
	full := append([]string{"bridge", cmd}, args...)
	full = append(full, "--wow", o.wow, "--lang", lang)
	return timed(o, exec.Command(o.goBin, full...))
}

// timed runs one side. Both read the game tables from the same cache, as the app sets it for them: the
// repo's scripts/cache unless WOW_TOOLS_CACHE names another.
func timed(o options, c *exec.Cmd) ([]byte, time.Duration, error) {
	if os.Getenv("WOW_TOOLS_CACHE") == "" {
		c.Env = append(os.Environ(), "WOW_TOOLS_CACHE="+filepath.Join(o.root, "scripts", "cache"))
	}
	var stderr bytes.Buffer
	c.Stderr = &stderr
	start := time.Now()
	out, err := c.Output()
	took := time.Since(start)
	if err != nil {
		return out, took, fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, took, nil
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func fileName(c testCase) string {
	s := regexp.MustCompile(`[^A-Za-z0-9._-]+`).ReplaceAllString(c.Name, "_")
	return strings.Trim(s, "_")
}

func runCase(o options, c testCase, ported bool) result {
	r := result{Case: c}
	first, took, err := runPython(o, c.Command, c.Args, c.Lang)
	r.PythonMs = ms(took)
	_ = os.WriteFile(filepath.Join(o.out, fileName(c)+".python.json"), first, 0o644)
	if err != nil {
		r.Status, r.Detail = "python failed", err.Error()
		return r
	}
	py, err := parseJSON(first)
	if err != nil {
		r.Status, r.Detail = "python failed", "not JSON: "+err.Error()
		return r
	}
	if e := py.get("error"); e != nil && e.kind == 's' {
		r.ErrorText = e.str
	}
	skip := map[string]bool{}
	if !o.once {
		second, _, err := runPython(o, c.Command, c.Args, c.Lang)
		if again, perr := parseJSON(second); err == nil && perr == nil {
			for _, d := range diffAnswers(py, again, o.tol, o.ignore, nil) {
				if d.Kind != "order" { // an order path is an object: skipping it would hide all its values
					skip[d.Path] = true
				}
			}
		}
		r.Volatile = len(skip)
	}
	if !ported {
		r.Status = "not ported"
		return r
	}
	out, took, err := runGo(o, c.Command, c.Args, c.Lang)
	r.GoMs = ms(took)
	_ = os.WriteFile(filepath.Join(o.out, fileName(c)+".go.json"), out, 0o644)
	if err != nil {
		r.Status, r.Detail = "go failed", err.Error()
		return r
	}
	g, err := parseJSON(out)
	if err != nil {
		r.Status, r.Detail = "go failed", "not JSON: "+err.Error()
		return r
	}
	if np := g.get("notPorted"); np != nil && np.kind == 'b' && np.b {
		// The command is ported, this variant of it not yet: the app asks Python for it.
		r.Status = "not ported"
		return r
	}
	r.Diffs = diffAnswers(py, g, o.tol, o.ignore, skip)
	r.Status = "equal"
	for _, d := range r.Diffs {
		if d.Kind == "order" && !o.strictOrder {
			r.OrderNotes++ // see strictOrder
			continue
		}
		r.Status = "differs"
	}
	return r
}

func printResult(o options, r result) {
	goMs := "-"
	if r.GoMs > 0 {
		goMs = fmt.Sprintf("%.0f ms", r.GoMs)
	}
	line := fmt.Sprintf("%-12s %8.0f ms %9s  %s", r.Status, r.PythonMs, goMs, r.Case.Name)
	if r.ErrorText != "" {
		line += "  [error: " + cut(r.ErrorText, 70) + "]"
	}
	if r.OrderNotes > 0 {
		line += fmt.Sprintf("  (%d objects with another key order)", r.OrderNotes)
	}
	fmt.Println(line)
	if r.Detail != "" {
		fmt.Println("    " + cut(r.Detail, 300))
	}
	shown := 0
	for i, d := range r.Diffs {
		if d.Kind == "order" && !o.strictOrder && !o.verbose {
			continue
		}
		if shown == o.maxDiffs && !o.verbose {
			fmt.Printf("    … %d more\n", len(r.Diffs)-i)
			break
		}
		fmt.Printf("    %-7s %s: python %s, go %s\n", d.Kind, d.Path, d.Python, d.Go)
		shown++
	}
}

// cut shortens s to n characters (not bytes: the answers are full of umlauts).
func cut(s string, n int) string {
	r := []rune(strings.ReplaceAll(s, "\n", " "))
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return string(r)
}

// printSummary counts the results and adds up the time per command, the baseline each phase is measured by.
func printSummary(results []result) {
	count := map[string]int{}
	type times struct {
		n        int
		py, gone float64
		goN      int
	}
	per := map[string]*times{}
	var order []string
	for _, r := range results {
		count[r.Status]++
		t := per[r.Case.Command]
		if t == nil {
			t = &times{}
			per[r.Case.Command] = t
			order = append(order, r.Case.Command)
		}
		t.n++
		t.py += r.PythonMs
		if r.GoMs > 0 {
			t.gone += r.GoMs
			t.goN++
		}
	}
	fmt.Println("\ncommand        cases   python avg       go avg")
	for _, cmd := range order {
		t := per[cmd]
		goAvg := "-"
		if t.goN > 0 {
			goAvg = fmt.Sprintf("%.0f ms", t.gone/float64(t.goN))
		}
		fmt.Printf("%-14s %5d %9.0f ms %12s\n", cmd, t.n, t.py/float64(t.n), goAvg)
	}
	var parts []string
	for _, s := range []string{"equal", "differs", "not ported", "python failed", "go failed"} {
		if count[s] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", count[s], s))
		}
	}
	fmt.Println("\n" + strings.Join(parts, ", "))
}
