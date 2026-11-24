// Package bridge answers the desktop app: one command in, one JSON object out, errors as {"error": "…"} with
// exit code 0 (`wowtools bridge <command>`). It took over from scripts/app_bridge.py, field for field; the
// comments still name the Python functions a command was ported from (removed in phase 5 of
// notes/go-migration.md, see the git history before it), and golden_test.go holds the Python bridge's
// answers on the test folder.
package bridge

import (
	"fmt"
	"sort"
	"time"

	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
	"github.com/kejith/wow-tools-forever/sim/internal/pyjson"
)

// handler answers one command; an error becomes {"error": its text}, as app_bridge.py answers the errors it
// expects (FileNotFoundError, LookupError, ValueError, RuntimeError).
type handler func(a *Args) (*pyjson.Obj, error)

// handlers maps a bridge command to its Go implementation.
var handlers = map[string]handler{
	"status":    status,
	"character": character,
	"rotations": rotationsCmd,
	"healing":   healingCmd,
	"damage":    damageCmd,
	"combatlog": combatlogCmd,
}

// register adds a command; the files of the ported scripts call it from init, so that ports done side by
// side do not edit one list.
func register(name string, h handler) { handlers[name] = h }

// Commands lists the bridge commands, sorted.
func Commands() []string {
	out := make([]string, 0, len(handlers))
	for name := range handlers {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Run answers one bridge command. argv are the arguments after the command, as app_bridge.py took them. An
// error means the command or its arguments are wrong: that is a usage message and exit code 2, not JSON.
func Run(command string, argv []string) (*pyjson.Obj, error) {
	h, ok := handlers[command]
	if !ok { // argparse refuses a command it does not know
		return nil, fmt.Errorf("unknown command %q", command)
	}
	a, err := ParseArgs(argv)
	if err != nil {
		return nil, err
	}
	language := i18n.SetLanguage(a.Lang, a.Wow)
	r, err := safely(h, a)
	if err != nil {
		return pyjson.New("error", err.Error()), nil
	}
	r.Set("generated", float64(time.Now().UnixMicro())/1e6)
	r.Set("language", language)
	return r, nil
}

// safely turns a panic of a handler into an error answer: Python would stop with a traceback, which the app
// cannot read either; an error text is the better answer.
func safely(h handler, a *Args) (r *pyjson.Obj, err error) {
	defer func() {
		if p := recover(); p != nil {
			r, err = nil, fmt.Errorf("internal error: %v", p)
		}
	}()
	return h(a)
}
