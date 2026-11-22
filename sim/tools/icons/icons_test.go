package icons

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDecodeLikePillow(t *testing.T) {
	for _, c := range pillowCases {
		data, _ := hex.DecodeString(c.blp)
		img, err := DecodeBLP(data)
		if c.rgba == "" {
			if err == nil {
				t.Errorf("%s: decoded, Pillow cannot", c.name)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if img.Rect.Dx() != c.w || img.Rect.Dy() != c.h || hex.EncodeToString(img.Pix) != c.rgba {
			t.Errorf("%s: other pixels than Pillow's", c.name)
		}
	}
}

// TestRetryWhenThrottled: wago.tools answers 429 twice, then the icon; a server that stays busy gives up after
// the retries.
func TestRetryWhenThrottled(t *testing.T) {
	waits := retryWaits
	retryWaits = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	defer func() { retryWaits = waits }()
	blp := blpOf(t)
	calls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[r.URL.Path]++
		if r.URL.Path == "/api/casc/700" && calls[r.URL.Path] <= 2 || r.URL.Path == "/api/casc/701" {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write(blp)
	}))
	defer srv.Close()
	baseURL = srv.URL
	defer func() { baseURL = "https://wago.tools" }()
	cache := t.TempDir()
	os.MkdirAll(Dir(cache), 0o755)
	if !Fetch(cache, 700) || calls["/api/casc/700"] != 3 {
		t.Errorf("throttled twice: fetched %v after %d calls", Fetch(cache, 700), calls["/api/casc/700"])
	}
	if Fetch(cache, 701) || calls["/api/casc/701"] != 1+len(retryWaits) {
		t.Errorf("always throttled: %d calls", calls["/api/casc/701"])
	}
}

func blpOf(t *testing.T) []byte {
	for _, c := range pillowCases {
		if c.name == "dxt5" {
			data, _ := hex.DecodeString(c.blp)
			return data
		}
	}
	t.Fatal("no dxt5 case")
	return nil
}

func TestResolve(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("WOW_TOOLS_CACHE", cache)
	// Item 1 has an appearance with an icon (modifier 0 wins), item 2 only Item.IconFileDataID, item 3 nothing;
	// icon 600 is no BLP on the server.
	tables := map[string]string{
		"ItemModifiedAppearance": "ID,ItemID,ItemAppearanceModifierID,ItemAppearanceID\n1,1,1,11\n2,1,0,10\n3,2,0,12\n",
		"ItemAppearance":         "ID,DefaultIconFileDataID\n10,500\n11,501\n12,0\n",
		"Item":                   "ID,IconFileDataID\n1,999\n2,502\n3,0\n4,600\n",
	}
	for name, text := range tables {
		os.WriteFile(filepath.Join(cache, name+"-1.0.0.1.csv"), []byte(text), 0o644)
	}
	blp := blpOf(t)
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		if r.URL.Path == "/api/casc/600" {
			w.Write([]byte("<html>"))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/casc/") {
			w.Write(blp)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	baseURL = srv.URL
	defer func() { baseURL = "https://wago.tools" }()

	r, err := Resolve(t.TempDir(), []int64{4, 2, 1, 3, 0})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := r.MarshalJSON()
	want := fmt.Sprintf(`{"icons": {"1": 500, "2": 502, "3": null, "4": null}, "fallback": 134400, "dir": %q}`,
		filepath.Join(cache, "icons"))
	if string(b) != want {
		t.Errorf("got %s\nwant %s", b, want)
	}
	f, err := os.Open(filepath.Join(cache, "icons", "500.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil || img.Bounds().Dx() != 8 {
		t.Fatal("the PNG", err)
	}
	// A second call asks nothing: the PNGs are there.
	n := len(asked)
	Resolve(t.TempDir(), []int64{1})
	if len(asked) != n {
		t.Error("downloaded again", asked[n:])
	}
}

// TestCacheLikePillow decodes the BLP of every icon in BLP_DIR (<FileDataID>.blp, fetched once from
// wago.tools) and compares the pixels with the PNG Pillow made of it in ICON_DIR (scripts/cache/icons).
func TestCacheLikePillow(t *testing.T) {
	blpDir, iconDir := os.Getenv("BLP_DIR"), os.Getenv("ICON_DIR")
	if blpDir == "" || iconDir == "" {
		t.Skip("BLP_DIR and ICON_DIR not set")
	}
	files, _ := filepath.Glob(filepath.Join(blpDir, "*.blp"))
	same := 0
	for _, path := range files {
		data, _ := os.ReadFile(path)
		img, err := DecodeBLP(data)
		pngPath := filepath.Join(iconDir, strings.TrimSuffix(filepath.Base(path), ".blp")+".png")
		f, perr := os.Open(pngPath)
		if perr != nil {
			continue
		}
		want, derr := png.Decode(f)
		f.Close()
		if derr != nil {
			t.Fatal(pngPath, derr)
		}
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		rgba := image.NewNRGBA(want.Bounds())
		draw.Draw(rgba, rgba.Rect, want, want.Bounds().Min, draw.Src)
		if !bytes.Equal(rgba.Pix, img.Pix) || rgba.Rect != img.Rect {
			t.Errorf("%s: other pixels than Pillow's", path)
			continue
		}
		same++
	}
	t.Logf("%d of %d icons equal", same, len(files))
	if same == 0 {
		t.Error("nothing compared")
	}
}
