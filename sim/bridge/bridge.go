// Package bridge is the Go side of scripts/app_bridge.py: one command in, one JSON object out, errors as
// {"error": "…"} with exit code 0. During the migration (notes/go-migration.md) it grows one command at a
// time; a command it does not have yet is still answered by the Python bridge, and the parity check
// (cmd/parity) compares the two wherever both answer.
package bridge

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/kejith/wow-forever-tools/sim/internal/i18n"
	"github.com/kejith/wow-forever-tools/sim/internal/pyjson"
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

// notPorted is the answer to a command, or a variant of one, that only the Python bridge has so far:
// main.js then asks app_bridge.py instead, and the parity check counts the case as not ported.
type notPorted string

func (n notPorted) Error() string { return "not ported to Go yet: " + string(n) }

func notPortedAnswer(what string) *pyjson.Obj {
	return pyjson.New("error", notPorted(what).Error(), "notPorted", true)
}

// Commands lists the bridge commands the Go side answers, sorted.
func Commands() []string {
	out := make([]string, 0, len(handlers))
	for name := range handlers {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Run answers one bridge command. argv are the arguments after the command, as app_bridge.py takes them.
// The second result is false when the arguments are wrong: argparse answers that with a usage message and
// exit code 2, not with JSON.
func Run(command string, argv []string) (*pyjson.Obj, error) {
	h, ok := handlers[command]
	if !ok {
		return notPortedAnswer(command), nil
	}
	a, err := ParseArgs(argv)
	if err != nil {
		return nil, err
	}
	language := i18n.SetLanguage(a.Lang, a.Wow)
	r, err := safely(h, a)
	var np notPorted
	if errors.As(err, &np) {
		return notPortedAnswer(string(np)), nil
	}
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
