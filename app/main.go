// WoW Forever Tools, the desktop app: a Wails window (WebView2 on Windows) around the page in renderer/, with
// the calculations of sim/ built in.
//
//	"WoW Forever Tools.exe"                       the window
//	"WoW Forever Tools.exe" --capture=<folder>    screenshots of every page, then exit (capture.go)
//	"WoW Forever Tools.exe" bridge <command> …    one bridge answer on stdout (what wowtools bridge answers)
//
// Build: build.sh (Windows resources, the exe and the installer); from source also
// `go build -tags desktop,production .` in app/.
package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/logger"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:renderer
var assets embed.FS

//go:embed VERSION
var versionFile string

// packaged is set by the release build (-ldflags "-X main.packaged=1"): the installed app, whose files lie
// next to the executable, and which updates itself.
var packaged string

func main() {
	if len(os.Args) > 1 && os.Args[1] == "bridge" {
		os.Exit(bridgeMain(os.Args[2:]))
	}
	app := newApp(strings.TrimSpace(versionFile))
	page, err := fs.Sub(assets, "renderer")
	if err != nil {
		panic(err)
	}
	err = wails.Run(&options.App{
		Title:            "WoW Forever Tools",
		Width:            1280,
		Height:           860,
		MinWidth:         980,
		MinHeight:        640,
		BackgroundColour: &options.RGBA{R: 0x14, G: 0x11, B: 0x0f, A: 255},
		AssetServer:      &assetserver.Options{Assets: page, Handler: app.iconHandler()},
		OnStartup:        app.startup,
		OnDomReady:       app.domReady,
		OnBeforeClose:    app.beforeClose,
		Bind:             []any{app},
		LogLevel:         logger.WARNING,
		Windows: &windows.Options{
			// localStorage (the saved view) and the WebView2 cache stay in the app's data folder.
			WebviewUserDataPath: filepath.Join(app.userData, "webview"),
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
