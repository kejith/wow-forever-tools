// Package api implements the HTTP server for the WoW Tools Forever AH community sync API.
package api

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kejith/wow-tools-forever/syncserver/internal/db"
)

// Store is the persistence contract used by the HTTP handlers.
type Store interface {
	Ping(ctx context.Context) error
	RecordUpload(ctx context.Context, in db.UploadInput) (*db.UploadResult, error)
}

var (
	validRegions  = map[string]bool{"eu": true, "us": true, "kr": true, "tw": true, "cn": true}
	validFactions = map[string]bool{"horde": true, "alliance": true}
	validRealm    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	validUUID     = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	day0          = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
)

const (
	maxCompressedBytes   = 4 << 20  // 4 MiB compressed
	maxDecompressedBytes = 32 << 20 // 32 MiB decompressed (gzip bomb guard)
	maxUploadEntries     = 250_000
	minItemID            = 1
	maxItemID            = 10_000_000
	minDay               = 2000 // ~2025-06
	maxCopper            = 10_000_000_000 // 1,000,000 gold
	maxOffers            = 1_000_000
)

// UploadPayload is the JSON body accepted by POST /v1/markets/{region}/{realm}/{faction}/uploads.
type UploadPayload struct {
	Schema       int       `json:"schema"`
	InstallID    string    `json:"installId"`
	AppVersion   string    `json:"appVersion"`
	RealmDisplay string    `json:"realmDisplay"`
	ScanTime     int64     `json:"scanTime"`
	FullScanDays []int     `json:"fullScanDays"`
	Entries      [][]int64 `json:"entries"` // [itemId, day, low, high, offers, lastPriceOr0]
}

// Server serves the sync HTTP API and cached blob files.
type Server struct {
	store          Store
	blobDir        string
	cooldownSec    int
	onUploadNotify func() // wakes publisher

	saltMu   sync.Mutex
	saltDay  string
	saltData []byte
}

// New creates a new API server.
func New(store Store, blobDir string, cooldownSec int, onUploadNotify func()) *Server {
	return &Server{
		store:          store,
		blobDir:        blobDir,
		cooldownSec:    cooldownSec,
		onUploadNotify: onUploadNotify,
	}
}

// Handler returns the configured http.Handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /v1/markets/{region}/{realm}/{faction}/uploads", s.handleUpload)
	mux.HandleFunc("GET /v1/markets/{region}/{realm}/{faction}/manifest.json", s.handleManifest)
	mux.HandleFunc("GET /v1/markets/{region}/{realm}/{faction}/snapshots/{file}", s.handleSnapshot)
	mux.HandleFunc("GET /v1/markets/{region}/{realm}/{faction}/days/{day}/{file}", s.handleDayBlob)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "db unavailable"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func parseMarketParams(r *http.Request) (region, realm, faction string, ok bool) {
	region = strings.ToLower(r.PathValue("region"))
	realm = strings.ToLower(r.PathValue("realm"))
	faction = strings.ToLower(r.PathValue("faction"))
	if !validRegions[region] || !validRealm.MatchString(realm) || !validFactions[faction] {
		return "", "", "", false
	}
	return region, realm, faction, true
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	region, realm, faction, ok := parseMarketParams(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid market path"})
		return
	}

	rawBody, err := readUploadBody(r)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errPayloadTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}

	sum := sha256.Sum256(rawBody)
	payloadSHA := hex.EncodeToString(sum[:])

	var payload UploadPayload
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON payload"})
		return
	}
	if payload.Schema != 1 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported schema version"})
		return
	}
	if !validUUID.MatchString(payload.InstallID) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid installId"})
		return
	}
	if len(payload.Entries) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "no entries in upload"})
		return
	}
	if len(payload.Entries) > maxUploadEntries {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "too many entries"})
		return
	}

	maxDay := currentScanDay(time.Now().UTC()) + 1
	fullDays := map[int]bool{}
	for _, d := range payload.FullScanDays {
		if d >= minDay && d <= maxDay {
			fullDays[d] = true
		}
	}

	// Deduplicate (itemId, day) within the same upload so PostgreSQL UNNEST ON CONFLICT never hits the same key twice.
	type key struct {
		day    int32
		itemID int32
	}
	dedup := make(map[key]db.UploadEntry, len(payload.Entries))
	for _, row := range payload.Entries {
		if len(row) < 5 || len(row) > 6 {
			continue
		}
		itemID, day, low, high, offers := row[0], row[1], row[2], row[3], row[4]
		var lastPrice int64
		if len(row) == 6 {
			lastPrice = row[5]
		}
		if itemID < minItemID || itemID > maxItemID {
			continue
		}
		if int(day) < minDay || int(day) > maxDay {
			continue
		}
		if low <= 0 || low > maxCopper || high < low || high > maxCopper {
			continue
		}
		if offers < 0 || offers > maxOffers {
			continue
		}
		if lastPrice < 0 || lastPrice > maxCopper {
			lastPrice = 0
		}
		k := key{day: int32(day), itemID: int32(itemID)}
		e := db.UploadEntry{
			ItemID:     int32(itemID),
			Day:        int32(day),
			Low:        low,
			High:       high,
			Offers:     int32(offers),
			LastPrice:  lastPrice,
			IsFullScan: fullDays[int(day)],
		}
		if prev, exists := dedup[k]; exists {
			if e.Low < prev.Low {
				prev.Low = e.Low
			}
			if e.High > prev.High {
				prev.High = e.High
			}
			if e.Offers > prev.Offers {
				prev.Offers = e.Offers
			}
			if e.LastPrice > 0 {
				prev.LastPrice = e.LastPrice
			}
			prev.IsFullScan = prev.IsFullScan || e.IsFullScan
			dedup[k] = prev
		} else {
			dedup[k] = e
		}
	}

	if len(dedup) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "no valid entries after validation"})
		return
	}

	entries := make([]db.UploadEntry, 0, len(dedup))
	for _, e := range dedup {
		entries = append(entries, e)
	}

	scanTime := time.Now().UTC()
	if payload.ScanTime > 1577836800 && payload.ScanTime <= time.Now().Unix()+86400 {
		scanTime = time.Unix(payload.ScanTime, 0).UTC()
	}

	displayRealm := strings.TrimSpace(payload.RealmDisplay)
	if len(displayRealm) > 64 {
		displayRealm = displayRealm[:64]
	}
	if displayRealm == "" {
		displayRealm = realm
	}
	appVer := strings.TrimSpace(payload.AppVersion)
	if len(appVer) > 32 {
		appVer = appVer[:32]
	}

	res, err := s.store.RecordUpload(r.Context(), db.UploadInput{
		Region:       region,
		Realm:        realm,
		Faction:      faction,
		DisplayRealm: displayRealm,
		InstallID:    strings.ToLower(payload.InstallID),
		AppVersion:   appVer,
		IPHash:       s.hashClientIP(r),
		ScanTime:     scanTime,
		PayloadSHA:   payloadSHA,
		IsFullScan:   len(fullDays) > 0,
		Entries:      entries,
	})
	if errors.Is(err, db.ErrRateLimited) {
		w.Header().Set("Retry-After", strconv.Itoa(s.cooldownSec))
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error":              "upload rate limit reached",
			"nextUploadAfterSec": s.cooldownSec,
		})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to store upload"})
		return
	}

	if !res.Deduped && !res.Quarantined && s.onUploadNotify != nil {
		s.onUploadNotify()
	}

	status := http.StatusAccepted
	if res.Deduped {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{
		"accepted":           res.Accepted,
		"deduped":            res.Deduped,
		"daysTouched":        res.DaysTouched,
		"nextUploadAfterSec": s.cooldownSec,
	})
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	region, realm, faction, ok := parseMarketParams(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	p := filepath.Join(s.blobDir, region, realm, faction, "manifest.json")
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			w.Header().Set("Cache-Control", "no-store")
			http.NotFound(w, r)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	sum := sha256.Sum256(data)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

var validBlobFile = regexp.MustCompile(`^[1-9][0-9]{0,9}\.json\.gz$`)

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	region, realm, faction, ok := parseMarketParams(r)
	file := r.PathValue("file")
	if !ok || !validBlobFile.MatchString(file) {
		http.NotFound(w, r)
		return
	}
	p := filepath.Join(s.blobDir, region, realm, faction, "snapshots", file)
	serveGzipBlob(w, r, p)
}

func (s *Server) handleDayBlob(w http.ResponseWriter, r *http.Request) {
	region, realm, faction, ok := parseMarketParams(r)
	dayStr := r.PathValue("day")
	file := r.PathValue("file")
	day, err := strconv.Atoi(dayStr)
	if !ok || err != nil || day < minDay || !validBlobFile.MatchString(file) {
		http.NotFound(w, r)
		return
	}
	p := filepath.Join(s.blobDir, region, realm, faction, "days", dayStr, file)
	serveGzipBlob(w, r, p)
}

func serveGzipBlob(w http.ResponseWriter, r *http.Request, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			w.Header().Set("Cache-Control", "no-store")
			http.NotFound(w, r)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	sum := sha256.Sum256(data)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	// Served as raw .json.gz (application/gzip) so clients verify SHA-256 directly over the downloaded bytes
	// without proxies re-encoding Content-Encoding.
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

var errPayloadTooLarge = errors.New("payload too large")

func readUploadBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	limited := io.LimitReader(r.Body, maxCompressedBytes+1)
	var reader io.Reader = limited
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(limited)
		if err != nil {
			return nil, fmt.Errorf("invalid gzip stream")
		}
		defer gz.Close()
		reader = io.LimitReader(gz, maxDecompressedBytes+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if len(data) > maxDecompressedBytes || (!strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") && len(data) > maxCompressedBytes) {
		return nil, errPayloadTooLarge
	}
	return data, nil
}

func (s *Server) hashClientIP(r *http.Request) []byte {
	ip := r.Header.Get("X-CF-Connecting-IP")
	if ip == "" {
		ip = r.Header.Get("CF-Connecting-IP")
	}
	if ip == "" {
		ip = r.RemoteAddr
	}
	today := time.Now().UTC().Format("2006-01-02")
	s.saltMu.Lock()
	if s.saltDay != today || len(s.saltData) == 0 {
		s.saltDay = today
		s.saltData = make([]byte, 32)
		_, _ = rand.Read(s.saltData)
	}
	salt := s.saltData
	s.saltMu.Unlock()

	h := sha256.New()
	h.Write(salt)
	h.Write([]byte(ip))
	return h.Sum(nil)
}

func currentScanDay(now time.Time) int {
	return int(now.Sub(day0) / (24 * time.Hour))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
