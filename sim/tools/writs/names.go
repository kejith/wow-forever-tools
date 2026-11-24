package writs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"

	"github.com/kejith/wow-tools-forever/sim/internal/i18n"
)

// namesFile is the names of the writ and crate items in both languages, next to the Python scripts.
const namesFile = "item_names.json"

// DataDir is the folder scripts/data, where writs.py reads item_names.json. It is looked for, in this order,
// next to the binary (resources/bin -> resources/scripts/data in the installed app, sim/ -> scripts/data in a
// checkout), below the working folder (main.js runs the bridge in the repo or in resources) and in the source
// tree the binary was built from (go run, go test and the parity check, whose binary lives in a temporary
// folder). "" if none has the file.
func DataDir() string {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), "..", "scripts", "data"))
	}
	if wd, err := os.Getwd(); err == nil {
		dirs = append(dirs, filepath.Join(wd, "scripts", "data"))
	}
	if _, file, _, ok := runtime.Caller(0); ok { // sim/tools/writs/names.go
		dirs = append(dirs, filepath.Join(filepath.Dir(file), "..", "..", "..", "scripts", "data"))
	}
	for _, d := range dirs {
		if st, err := os.Stat(filepath.Join(d, namesFile)); err == nil && !st.IsDir() {
			return filepath.Clean(d)
		}
	}
	return ""
}

var (
	namesOnce sync.Once
	names     map[string]map[string]string
	namesErr  error
)

// loadNames reads item_names.json once.
func loadNames() error {
	namesOnce.Do(func() {
		dir := DataDir()
		if dir == "" {
			namesErr = &os.PathError{Op: "open", Path: filepath.Join("scripts", "data", namesFile),
				Err: os.ErrNotExist}
			return
		}
		data, err := os.ReadFile(filepath.Join(dir, namesFile))
		if err != nil {
			namesErr = err
			return
		}
		namesErr = json.Unmarshal(data, &names)
	})
	return namesErr
}

// ItemName is writs.item_name: the name of a writ or crate item in the selected language, English if the
// language has none, "Item <id>" if neither has it (or the names file is missing).
func ItemName(id int64) string {
	_ = loadNames()
	key := strconv.FormatInt(id, 10)
	if n := names[i18n.Language()][key]; n != "" {
		return n
	}
	if n := names["en"][key]; n != "" {
		return n
	}
	return "Item " + key
}
