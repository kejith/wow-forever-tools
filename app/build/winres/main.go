// Command winres writes the Windows resources of the app: the icon (from build/icon.png), the manifest
// (per-monitor DPI, common controls 6, which the Wails window needs) and the version info, as
// rsrc_windows_amd64.syso next to main.go, which go build links into the exe; and build/icon.ico for the
// installer. Run from app/: go run ./build/winres -version 1.12.0
package main

import (
	"flag"
	"fmt"
	"image/png"
	"log"
	"os"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

// appIconID is the resource the Wails window loads its icon from (winc.AppIconID).
const appIconID = 3

func main() {
	v := flag.String("version", "", "the app version, e.g. 1.12.0")
	flag.Parse()
	if *v == "" {
		log.Fatal("-version is required")
	}
	f, err := os.Open("build/icon.png")
	if err != nil {
		log.Fatal(err)
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		log.Fatal(err)
	}
	icon, err := winres.NewIconFromResizedImage(img, []int{256, 128, 64, 48, 32, 16})
	if err != nil {
		log.Fatal(err)
	}

	rs := winres.ResourceSet{}
	if err := rs.SetIcon(winres.ID(appIconID), icon); err != nil {
		log.Fatal(err)
	}
	rs.SetManifest(winres.AppManifest{
		Identity:            winres.AssemblyIdentity{Name: "io.github.kejith.wowtoolsforever"},
		DPIAwareness:        winres.DPIPerMonitorV2,
		UseCommonControlsV6: true,
		ExecutionLevel:      winres.AsInvoker,
	})
	var info version.Info
	if err := info.Set(version.LangDefault, version.ProductName, "WoW Tools Forever"); err != nil {
		log.Fatal(err)
	}
	for k, val := range map[string]string{
		version.FileDescription:  "WoW Tools Forever",
		version.CompanyName:      "kejith",
		version.LegalCopyright:   "Copyright © 2026 kejith",
		version.OriginalFilename: "WoW Tools Forever.exe",
		version.ProductVersion:   *v,
		version.FileVersion:      *v,
	} {
		if err := info.Set(version.LangDefault, k, val); err != nil {
			log.Fatal(err)
		}
	}
	info.SetFileVersion(*v)
	info.SetProductVersion(*v)
	rs.SetVersionInfo(info)

	out, err := os.Create("rsrc_windows_amd64.syso")
	if err != nil {
		log.Fatal(err)
	}
	if err := rs.WriteObject(out, winres.ArchAMD64); err != nil {
		log.Fatal(err)
	}
	if err := out.Close(); err != nil {
		log.Fatal(err)
	}
	ico, err := os.Create("build/icon.ico")
	if err != nil {
		log.Fatal(err)
	}
	if err := icon.SaveICO(ico); err != nil {
		log.Fatal(err)
	}
	if err := ico.Close(); err != nil {
		log.Fatal(err)
	}
	fmt.Println("wrote rsrc_windows_amd64.syso and build/icon.ico for", *v)
}
