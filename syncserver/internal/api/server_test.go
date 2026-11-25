package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kejith/wow-tools-forever/syncserver/internal/db"
)

type fakeStore struct {
	lastInput db.UploadInput
	res       *db.UploadResult
	err       error
}

func (f *fakeStore) Ping(context.Context) error { return nil }
func (f *fakeStore) RecordUpload(_ context.Context, in db.UploadInput) (*db.UploadResult, error) {
	f.lastInput = in
	if f.err != nil {
		return nil, f.err
	}
	if f.res != nil {
		return f.res, nil
	}
	return &db.UploadResult{MarketID: 1, Accepted: len(in.Entries), DaysTouched: []int{2519}}, nil
}

func gzipJSON(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if err := json.NewEncoder(gz).Encode(v); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestUploadValidationAndAccept(t *testing.T) {
	st := &fakeStore{}
	notified := false
	srv := New(st, t.TempDir(), 600, func() { notified = true })
	h := srv.Handler()

	// Valid gzipped upload (including one invalid row that should be skipped, and duplicate item-day merged)
	body := gzipJSON(t, UploadPayload{
		Schema:       1,
		InstallID:    "550e8400-e29b-41d4-a716-446655440000",
		AppVersion:   "2.4.0",
		RealmDisplay: "PvP",
		FullScanDays: []int{2519},
		Entries: [][]int64{
			{2770, 2519, 200, 220, 100, 200},
			{2770, 2519, 190, 250, 120, 195}, // merges with above
			{2770, 2519, -5, 10, 1, 0},       // invalid, dropped
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/markets/eu/pvp/horde/uploads", bytes.NewReader(body))
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CF-Connecting-IP", "203.0.113.10")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	if !notified {
		t.Fatal("expected publisher notify")
	}
	if len(st.lastInput.Entries) != 1 || st.lastInput.Entries[0].Low != 190 || st.lastInput.Entries[0].High != 250 || st.lastInput.Entries[0].Offers != 120 {
		t.Fatalf("unexpected merged upload entry: %+v", st.lastInput.Entries)
	}

	// Rate limited returns 429 + Retry-After
	st.err = db.ErrRateLimited
	req2 := httptest.NewRequest(http.MethodPost, "/v1/markets/eu/pvp/horde/uploads", bytes.NewReader(body))
	req2.Header.Set("Content-Encoding", "gzip")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests || rec2.Header().Get("Retry-After") != "600" {
		t.Fatalf("expected 429 with Retry-After, got %d %q", rec2.Code, rec2.Header().Get("Retry-After"))
	}
}

func TestManifestAndBlobCachingHeaders(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "eu", "pvp", "horde")
	if err := os.MkdirAll(filepath.Join(base, "days", "2519"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "manifest.json"), []byte(`{"schema":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "days", "2519", "1.json.gz"), []byte("gzbytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := New(&fakeStore{}, dir, 600, nil).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/markets/eu/pvp/horde/manifest.json", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "public, max-age=300" {
		t.Fatalf("unexpected manifest response: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	etag := rec.Header().Get("ETag")

	// Conditional GET returns 304
	req304 := httptest.NewRequest(http.MethodGet, "/v1/markets/eu/pvp/horde/manifest.json", nil)
	req304.Header.Set("If-None-Match", etag)
	rec304 := httptest.NewRecorder()
	h.ServeHTTP(rec304, req304)
	if rec304.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", rec304.Code)
	}

	// Immutable day blob
	recDay := httptest.NewRecorder()
	h.ServeHTTP(recDay, httptest.NewRequest(http.MethodGet, "/v1/markets/eu/pvp/horde/days/2519/1.json.gz", nil))
	if recDay.Code != http.StatusOK || recDay.Header().Get("Cache-Control") != "public, max-age=86400, immutable" {
		t.Fatalf("unexpected day blob response: %d %q", recDay.Code, recDay.Header().Get("Cache-Control"))
	}
}
