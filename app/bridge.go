package main

// Every calculation runs in the bridge (sim/bridge, the same code as `wowtools bridge`). The app starts itself
// for each command: `"WoW Forever Tools.exe" bridge <command> --wow … --lang …` answers with one JSON object
// on stdout. A process per command keeps the commands apart (the bridge selects its language for the whole
// process, and the engine keeps tables in memory), lets the pages ask several things at once, and a crash of
// a calculation never takes the window with it.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/kejith/wow-forever-tools/sim/bridge"
)

const bridgeTimeout = 2 * time.Minute

// bridgeMain answers `bridge <command> [args]` and `bridge --list` like wowtools does.
func bridgeMain(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: bridge --list | bridge <command> [args]")
		return 2
	}
	var answer any
	if args[0] == "--list" {
		answer = bridge.Commands()
	} else {
		r, err := bridge.Run(args[0], args[1:])
		if err != nil { // wrong arguments: a usage message and exit code 2, as wowtools does
			fmt.Fprintln(os.Stderr, "bridge "+args[0]+": "+err.Error())
			return 2
		}
		answer = r
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(answer); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	os.Stdout.Write(out.Bytes())
	return 0
}

// truthy is JavaScript's idea of true for a value the page sent.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0 && x == x
	case string:
		return x != ""
	}
	return true
}

// jsString is String(v) in JavaScript for the numbers and strings the page sends.
func jsString(v any) string {
	switch x := v.(type) {
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case nil:
		return "null"
	}
	return fmt.Sprint(v)
}

func oneOf(v any, values ...string) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	for _, x := range values {
		if s == x {
			return true
		}
	}
	return false
}

// bridgeArgs turns the options of a page into the bridge's command line; anything unknown is left out.
func bridgeArgs(command string, opts map[string]any, wowDir, lang string, capture bool) []string {
	args := []string{"bridge", command, "--wow", wowDir, "--lang", lang}
	if opts == nil {
		opts = map[string]any{}
	}
	if c, ok := opts["char"].(string); ok && c != "" {
		args = append(args, "--char", c)
	}
	if truthy(opts["top"]) {
		args = append(args, "--top", jsString(opts["top"]))
	}
	if truthy(opts["maxPrice"]) {
		args = append(args, "--max-price", jsString(opts["maxPrice"]))
	}
	if v, ok := opts["preview"]; ok {
		args = append(args, "--preview", jsString(v))
	}
	if truthy(opts["ownedAtAhPrice"]) {
		args = append(args, "--owned-at-ah-price")
	}
	if oneOf(opts["profile"], "leveling", "dungeon") {
		args = append(args, "--profile", opts["profile"].(string))
	}
	if oneOf(opts["sort"], "gain", "price") {
		args = append(args, "--sort", opts["sort"].(string))
	}
	if oneOf(opts["engine"], "on", "off") { // auto is the bridge's default
		args = append(args, "--engine", opts["engine"].(string))
	}
	if n, ok := jsNumber(opts["fightSeconds"]); ok && n > 0 {
		args = append(args, "--fight-seconds", jsString(n))
	}
	if d, ok := opts["targetDelta"].(float64); ok && (d == 0 || d == 1 || d == 2 || d == 3) {
		args = append(args, "--target-delta", jsString(d))
	}
	// Without it the engine decides how many kills the fight has.
	if k, ok := opts["killsPerMinute"].(float64); ok && k >= 0 {
		args = append(args, "--kills-per-minute", jsString(k))
	}
	if truthy(opts["consumables"]) {
		args = append(args, "--consumables")
	}
	// The set of consumables by value, and the gold an hour of fighting may cost with it.
	if opts["set"] == "value" {
		args = append(args, "--set", "value")
		if b, ok := jsNumber(opts["budget"]); ok && b > 0 {
			args = append(args, "--budget", jsString(b))
		}
	}
	if oneOf(opts["metric"], "dps", "hps", "ttoom") {
		args = append(args, "--metric", opts["metric"].(string))
	}
	if opts["method"] == "montecarlo" {
		args = append(args, "--method", "montecarlo")
	}
	if p, ok := opts["profession"].(string); ok && p != "" {
		args = append(args, "--profession", p)
	}
	if ids, ok := opts["ids"].([]any); ok {
		var list []string
		for _, id := range ids {
			if n, ok := jsNumber(id); ok && n > 0 {
				list = append(list, jsString(n))
			}
		}
		args = append(args, "--ids", strings.Join(list, ","))
	}
	// Screenshots only look: nothing is written into the game folder in capture mode.
	if command == "ingame" && opts["write"] == true && !capture {
		args = append(args, "--write")
	}
	return args
}

func errorAnswer(msg string) map[string]any { return map[string]any{"error": msg} }

// runBridge answers one command of a page: the bridge's JSON, or {"error": "…"}.
func (a *App) runBridge(command string, opts map[string]any) any {
	exe, err := os.Executable()
	if err != nil {
		return errorAnswer(t("The bridge could not be started ({reason})", "reason", err.Error()))
	}
	ctx, cancel := context.WithTimeout(context.Background(), bridgeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, bridgeArgs(command, opts, a.wowDir(), language(), a.capture)...)
	cmd.Dir = a.root
	cmd.Env = append(os.Environ(), "WOW_TOOLS_CACHE="+a.cacheDir())
	cmd.SysProcAttr = hiddenProcess()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	var notStarted *exec.Error
	if errors.As(runErr, &notStarted) {
		return errorAnswer(t("The bridge could not be started ({reason})", "reason", notStarted.Err.Error()))
	}
	text := strings.TrimSpace(stdout.String())
	if text == "" {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" && runErr != nil {
			msg = runErr.Error()
		}
		if msg == "" {
			msg = t("no output")
		}
		lines := strings.Split(msg, "\n")
		if len(lines) > 3 {
			lines = lines[len(lines)-3:]
		}
		return errorAnswer(strings.Join(lines, "\n"))
	}
	last := text[strings.LastIndex(text, "\n")+1:]
	if !json.Valid([]byte(last)) {
		return errorAnswer(t("Invalid answer from the bridge: {text}", "text", truncate(text, 200)))
	}
	return json.RawMessage(last)
}
