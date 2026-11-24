package main

// The latest GitHub release of an addon, downloaded once and kept in a cache folder, and the GitHub calls
// the app's own update shares (update.go).

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	githubAPI        = "https://api.github.com"
	releaseMaxAge    = 15 * time.Minute // GitHub allows 60 anonymous API calls per hour and address
	maxZipBytes      = 20 << 20
	maxUnpackedBytes = 50 << 20
	requestTimeout   = 15 * time.Second
)

var safeName = regexp.MustCompile(`^\w[\w.-]*$`)

// ---------- zip ----------

type zipEntry struct {
	name string
	data []byte
}

// readZip returns the files of a zip. It takes what the release packager and ordinary zip tools write (stored
// or deflated, no encryption) and checks size and CRC of every file.
func readZip(buf []byte) ([]zipEntry, error) {
	r, err := zip.NewReader(bytes.NewReader(buf), int64(len(buf)))
	// A name like ../x is refused below, with the message the page knows.
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		if errors.Is(err, zip.ErrFormat) {
			return nil, errors.New(t("not a zip file"))
		}
		return nil, errors.New(t("zip directory is damaged"))
	}
	var files []zipEntry
	var total uint64
	for _, f := range r.File {
		name := f.Name
		if strings.HasSuffix(name, "/") { // folder entry
			continue
		}
		if f.Flags&1 != 0 {
			return nil, errors.New(t("{name}: encrypted", "name", name))
		}
		total += f.UncompressedSize64
		if total > maxUnpackedBytes {
			return nil, errors.New(t("zip is too large when unpacked"))
		}
		if f.Method != zip.Store && f.Method != zip.Deflate {
			return nil, errors.New(t("{name}: unknown compression {method}", "name", name, "method", f.Method))
		}
		rc, err := f.Open()
		if err != nil {
			return nil, errors.New(t("{name}: entry is damaged", "name", name))
		}
		data, err := io.ReadAll(io.LimitReader(rc, int64(f.UncompressedSize64)+1))
		rc.Close()
		switch {
		case errors.Is(err, zip.ErrChecksum) || errors.Is(err, zip.ErrFormat):
			return nil, errors.New(t("{name}: checksum does not match", "name", name))
		case err != nil:
			return nil, errors.New(t("{name}: entry is damaged", "name", name))
		case uint64(len(data)) != f.UncompressedSize64 || crc32.ChecksumIEEE(data) != f.CRC32:
			return nil, errors.New(t("{name}: checksum does not match", "name", name))
		}
		files = append(files, zipEntry{name, data})
	}
	return files, nil
}

// extractAddon unpacks the folder `<name>/` of the zip into target. Anything outside that folder is an error,
// so a zip can never write next to the cache folder.
func extractAddon(buf []byte, name, target string) error {
	files, err := readZip(buf)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return errors.New(t("zip is empty"))
	}
	prefix := name + "/"
	for _, f := range files {
		rel, ok := strings.CutPrefix(f.name, prefix)
		parts := strings.Split(rel, "/")
		bad := !ok || rel == "" || strings.Contains(f.name, `\`)
		for _, p := range parts {
			if p == "" || p == "." || p == ".." || strings.Contains(p, ":") {
				bad = true
			}
		}
		if bad {
			return errors.New(t("Unexpected entry in the zip: {name}", "name", f.name))
		}
		file := filepath.Join(append([]string{target}, parts...)...)
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(file, f.data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// ---------- versions ----------

var tocVersionLine = regexp.MustCompile(`(?m)^## Version:[ \t]*(.+)$`)

// tocVersion is the `## Version:` of `<dir>/<name>.toc`, "" without one.
func tocVersion(dir, name string) string {
	data, err := os.ReadFile(filepath.Join(dir, name+".toc"))
	if err != nil {
		return ""
	}
	if m := tocVersionLine.FindSubmatch(data); m != nil {
		return strings.TrimSpace(string(m[1]))
	}
	return ""
}

var versionNumbers = regexp.MustCompile(`\d+(\.\d+)*`)

// compareVersions is -1, 0 or 1 for "1.4.0" against "1.10"; a leading v and anything after the numbers is
// ignored.
func compareVersions(a, b string) int {
	nums := func(v string) []int {
		var out []int
		for _, n := range strings.Split(versionNumbers.FindString(v), ".") {
			x, _ := strconv.Atoi(n)
			out = append(out, x)
		}
		return out
	}
	x, y := nums(a), nums(b)
	for i := 0; i < max(len(x), len(y)); i++ {
		var p, q int
		if i < len(x) {
			p = x[i]
		}
		if i < len(y) {
			q = y[i]
		}
		if p != q {
			if p < q {
				return -1
			}
			return 1
		}
	}
	return 0
}

// ---------- GitHub ----------

// githubError is an answer of GitHub the callers tell apart: notFound = no release.
type githubError struct {
	msg      string
	notFound bool
}

func (e *githubError) Error() string { return e.msg }

var httpClient = &http.Client{}

// netReason is the short reason of a failed connection, as Node named it: ECONNREFUSED, ENOTFOUND, …
func netReason(err error) string {
	var dns *net.DNSError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "TimeoutError"
	case errors.Is(err, context.Canceled):
		return "AbortError"
	case errors.Is(err, syscall.ECONNREFUSED) || strings.Contains(err.Error(), "refused"):
		return "ECONNREFUSED"
	case errors.Is(err, syscall.ECONNRESET):
		return "ECONNRESET"
	case errors.As(err, &dns):
		return "ENOTFOUND"
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Err != nil {
		return op.Err.Error()
	}
	return err.Error()
}

// githubGet is one GET to the GitHub API. The caller's context limits it; the body is the caller's to close.
func githubGet(ctx context.Context, url, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "wow-tools-forever")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, &githubError{msg: t("GitHub cannot be reached ({reason})", "reason", netReason(err))}
	}
	switch {
	case res.StatusCode == http.StatusNotFound:
		res.Body.Close()
		return nil, &githubError{msg: t("no release found"), notFound: true}
	case res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusTooManyRequests:
		res.Body.Close()
		return nil, &githubError{msg: t("GitHub rate limit reached, try again later")}
	case res.StatusCode < 200 || res.StatusCode > 299:
		res.Body.Close()
		return nil, &githubError{msg: t("GitHub answered with {status}", "status", res.StatusCode)}
	}
	return res, nil
}

// githubBytes is the whole answer of a GET, at most `limit` bytes (more is an error), within requestTimeout.
func githubBytes(url, accept string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	res, err := githubGet(ctx, url, accept)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, &githubError{msg: t("GitHub cannot be reached ({reason})", "reason", netReason(err))}
	}
	if int64(len(data)) > limit {
		return nil, errTooLarge
	}
	return data, nil
}

var errTooLarge = errors.New("too large")

// githubRelease is the answer of releases/latest, as far as the app reads it.
type githubRelease struct {
	TagName     any              `json:"tag_name"`
	HTMLURL     any              `json:"html_url"`
	PublishedAt any              `json:"published_at"`
	Assets      []map[string]any `json:"assets"`
}

func latestGithubRelease(api, repo string) (*githubRelease, error) {
	data, err := githubBytes(fmt.Sprintf("%s/repos/%s/releases/latest", api, repo), "application/vnd.github+json", 8<<20)
	if err != nil {
		return nil, err
	}
	var rel githubRelease
	if err := json.Unmarshal(data, &rel); err != nil {
		return nil, &githubError{msg: t("GitHub answered with {status}", "status", "invalid JSON")}
	}
	return &rel, nil
}

// str is a JSON string value, "" for anything else.
func str(v any) string {
	s, _ := v.(string)
	return s
}

// strPtr is a JSON string value, nil for anything else.
func strPtr(v any) *string {
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}

// integer is a JSON number that is a whole number.
func integer(v any) (int64, bool) {
	f, ok := v.(float64)
	if !ok || f != float64(int64(f)) {
		return 0, false
	}
	return int64(f), true
}

// ---------- the cached release ----------

// source is where an addon's files come from: a local checkout or a downloaded release.
type source struct {
	Kind        string  `json:"kind"`
	Dir         string  `json:"dir"`
	Repo        string  `json:"repo"`
	Version     *string `json:"version"`
	Tag         string  `json:"tag,omitempty"`
	URL         *string `json:"url,omitempty"`
	PublishedAt *string `json:"publishedAt,omitempty"`
	CheckedAt   *int64  `json:"checkedAt,omitempty"`
	Offline     bool    `json:"offline,omitempty"`
	Error       string  `json:"error,omitempty"`
}

// releaseState is `<cacheDir>/<name>/release.json`.
type releaseState struct {
	Repo        string  `json:"repo"`
	Tag         string  `json:"tag"`
	URL         *string `json:"url"`
	PublishedAt *string `json:"publishedAt"`
	CheckedAt   int64   `json:"checkedAt"`
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// cachedSource is the cached release as a source, nil when the cache is empty or incomplete.
func cachedSource(root, name string, state *releaseState) *source {
	if state == nil || !safeName.MatchString(state.Tag) {
		return nil
	}
	dir := filepath.Join(root, state.Tag)
	if _, err := os.Stat(filepath.Join(dir, name+".toc")); err != nil {
		return nil
	}
	checked := state.CheckedAt
	return &source{Kind: "release", Dir: dir, Repo: state.Repo, Tag: state.Tag, URL: state.URL, PublishedAt: state.PublishedAt,
		CheckedAt: &checked, Version: nullable(tocVersion(dir, name))}
}

type releaseOpts struct {
	repo, name, cacheDir, api string
	maxAge                    time.Duration
	force                     bool
}

// latestRelease is the newest release of opts.repo, unpacked under `<cacheDir>/<name>/<tag>`. It asks GitHub
// at most every maxAge (force asks now). Without network the last download is returned with Offline and the
// reason.
func latestRelease(o releaseOpts) (*source, error) {
	if o.cacheDir == "" {
		return nil, errors.New(t("No cache folder for addon releases."))
	}
	if o.api == "" {
		o.api = githubAPI
	}
	if o.maxAge == 0 {
		o.maxAge = releaseMaxAge
	}
	root := filepath.Join(o.cacheDir, o.name)
	stateFile := filepath.Join(root, "release.json")
	var state *releaseState
	if data, err := os.ReadFile(stateFile); err == nil {
		var s releaseState
		if json.Unmarshal(data, &s) == nil {
			state = &s
		}
	}
	var cached *source
	if state != nil && state.Repo == o.repo {
		cached = cachedSource(root, o.name, state)
	}
	if cached != nil && !o.force && time.Since(time.UnixMilli(state.CheckedAt)) < o.maxAge {
		return cached, nil
	}
	found, err := fetchRelease(o, root, stateFile)
	if err == nil {
		return found, nil
	}
	var gh *githubError
	notFound := errors.As(err, &gh) && gh.notFound
	reason := err.Error()
	if notFound {
		reason = t("{repo} has no release yet.", "repo", o.repo)
	}
	if cached != nil {
		cached.Offline = true
		cached.Error = reason
		return cached, nil
	}
	if notFound {
		return nil, errors.New(reason)
	}
	return nil, errors.New(t("Release of {repo} not loaded: {reason}", "repo", o.repo, "reason", reason))
}

func fetchRelease(o releaseOpts, root, stateFile string) (*source, error) {
	rel, err := latestGithubRelease(o.api, o.repo)
	if err != nil {
		return nil, err
	}
	tag := str(rel.TagName)
	if !safeName.MatchString(tag) {
		return nil, errors.New(t("Unexpected release name: {tag}", "tag", truncate(tag, 40)))
	}
	var zips []map[string]any
	for _, a := range rel.Assets {
		if name, ok := a["name"].(string); ok && strings.HasSuffix(strings.ToLower(name), ".zip") {
			zips = append(zips, a)
		}
	}
	var asset map[string]any
	for _, a := range zips {
		if a["name"] == fmt.Sprintf("%s-%s.zip", o.name, tag) {
			asset = a
			break
		}
	}
	if asset == nil && len(zips) > 0 {
		asset = zips[0]
	}
	id, ok := integer(asset["id"])
	if asset == nil || !ok {
		return nil, errors.New(t("Release {tag} has no zip", "tag", tag))
	}
	if size, _ := asset["size"].(float64); size > maxZipBytes {
		return nil, errors.New(t("The zip of release {tag} is too large", "tag", tag))
	}

	dir := filepath.Join(root, tag)
	if _, err := os.Stat(filepath.Join(dir, o.name+".toc")); err != nil {
		// The asset is addressed by its id on the API host, never by a URL taken from the answer.
		buf, err := githubBytes(fmt.Sprintf("%s/repos/%s/releases/assets/%d", o.api, o.repo, id), "application/octet-stream", maxZipBytes)
		if errors.Is(err, errTooLarge) {
			return nil, errors.New(t("The zip of release {tag} is too large", "tag", tag))
		}
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(root, 0o755); err != nil {
			return nil, err
		}
		tmp := filepath.Join(root, fmt.Sprintf(".%s.%d.tmp", tag, os.Getpid()))
		os.RemoveAll(tmp)
		err = func() error {
			defer os.RemoveAll(tmp)
			if err := extractAddon(buf, o.name, tmp); err != nil {
				return err
			}
			if _, err := os.Stat(filepath.Join(tmp, o.name+".toc")); err != nil {
				return errors.New(t("The zip of release {tag} contains no {toc}", "tag", tag, "toc", o.name+".toc"))
			}
			os.RemoveAll(dir)
			return os.Rename(tmp, dir)
		}()
		if err != nil {
			return nil, err
		}
	}
	next := &releaseState{Repo: o.repo, Tag: tag, URL: strPtr(rel.HTMLURL), PublishedAt: strPtr(rel.PublishedAt),
		CheckedAt: time.Now().UnixMilli()}
	data, _ := json.MarshalIndent(next, "", "  ")
	if err := os.WriteFile(stateFile, data, 0o644); err != nil {
		return nil, err
	}
	// Older downloads are no longer needed.
	if entries, err := os.ReadDir(root); err == nil {
		for _, e := range entries {
			if e.IsDir() && e.Name() != tag {
				os.RemoveAll(filepath.Join(root, e.Name()))
			}
		}
	}
	return cachedSource(root, o.name, next), nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
