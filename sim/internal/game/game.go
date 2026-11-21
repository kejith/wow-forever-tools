// Package game finds the files of the game folder the way the Python scripts do (writs.find_chars,
// writs.find_auctionator, the globs of app_bridge.status): the same paths, written the same way, in the same
// order, so the answers of both bridges name the same files.
package game

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// DefaultWow is writs.DEFAULT_WOW: WOW_FOREVER_DIR, else the game's folder under Windows or, in WSL, under
// /mnt/c.
func DefaultWow() string {
	if w := os.Getenv("WOW_FOREVER_DIR"); w != "" {
		return w
	}
	if runtime.GOOS == "windows" {
		return `C:\Program Files (x86)\World of Warcraft\_forever_`
	}
	return "/mnt/c/Program Files (x86)/World of Warcraft/_forever_"
}

// Join is os.path.join: the parts with one separator between them, nothing cleaned up (Python keeps "./" and
// a doubled separator; filepath.Join would not, and the answers carry these paths).
func Join(parts ...string) string {
	sep := string(os.PathSeparator)
	out := ""
	for _, p := range parts {
		switch {
		case filepath.IsAbs(p) && p != "":
			out = p
		case out == "" || strings.HasSuffix(out, sep) || strings.HasSuffix(out, "/"):
			out += p
		default:
			out += sep + p
		}
	}
	return out
}

// Glob is glob.glob over os.path.join(base, parts...): every part may hold * and ?; the returned paths start
// with base as it was given. Hidden entries (a leading dot) match only a pattern with a leading dot, as in
// Python. Directory entries come in name order (Python's come in the order of the file system).
func Glob(base string, parts ...string) []string {
	paths := []string{base}
	for i, part := range parts {
		// Like Python, only directories go on to the next part: opening a file as a directory costs a round
		// trip each, which under /mnt/c (WSL) is slow.
		dirOnly := i < len(parts)-1
		var next []string
		for _, dir := range paths {
			if !strings.ContainsAny(part, "*?[") {
				p := Join(dir, part)
				if info, err := os.Stat(p); err == nil && (!dirOnly || info.IsDir()) {
					next = append(next, p)
				} else if err != nil && !dirOnly {
					if _, lerr := os.Lstat(p); lerr == nil { // os.path.lexists: a broken link counts
						next = append(next, p)
					}
				}
				continue
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				name := e.Name()
				if strings.HasPrefix(name, ".") && !strings.HasPrefix(part, ".") {
					continue
				}
				if ok, _ := filepath.Match(part, name); !ok {
					continue
				}
				p := Join(dir, name)
				if dirOnly && !e.IsDir() {
					if e.Type()&os.ModeSymlink == 0 {
						continue
					}
					if info, err := os.Stat(p); err != nil || !info.IsDir() {
						continue
					}
				}
				next = append(next, p)
			}
		}
		paths = next
	}
	return paths
}

// Mtime is os.path.getmtime: seconds as a float, computed as Python does (seconds + nanoseconds × 1e-9).
func Mtime(path string) (float64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	t := info.ModTime()
	return float64(t.Unix()) + float64(t.Nanosecond())*1e-9, nil
}

// AccountFiles are the account-wide SavedVariables of that name (WTF/Account/*/SavedVariables/<name>).
func AccountFiles(wow, name string) []string {
	return Glob(wow, "WTF", "Account", "*", "SavedVariables", name)
}

// FindAuctionator is writs.find_auctionator: the first account's Auctionator.lua, "" if there is none.
func FindAuctionator(wow string) string {
	if p := AccountFiles(wow, "Auctionator.lua"); len(p) > 0 {
		return p[0]
	}
	return ""
}

// FindChars is writs.find_chars: every character's WoWMCP.lua, most recently written first, whose path
// contains char (any case).
func FindChars(wow, char string) []string {
	paths := Glob(wow, "WTF", "Account", "*", "*", "*", "SavedVariables", "WoWMCP.lua")
	times := map[string]float64{}
	for _, p := range paths {
		times[p], _ = Mtime(p)
	}
	sort.SliceStable(paths, func(i, j int) bool { return times[paths[i]] > times[paths[j]] })
	needle := strings.ToLower(char)
	var out []string
	for _, p := range paths {
		if strings.Contains(strings.ToLower(p), needle) {
			out = append(out, p)
		}
	}
	return out
}

// CharKey is app_bridge.char_key: the character's folder name ("Brakk-Düsterfels").
func CharKey(path string) string { return filepath.Base(filepath.Dir(filepath.Dir(path))) }
