package main

// The app's own update: the newest release of kejith/wow-tools-forever. The Windows installer of a newer
// version is downloaded into the app's data folder, compared with the release's SHA256SUMS.txt and started
// silently; it replaces the installation where it is and starts the app again.
// Nothing here needs the window (app.go hands in the folder, the version and what happens on a change of
// state), so update_test.go runs all of it against a stand-in for the GitHub API.
//
// The checksum catches a damaged or cut-off download and an installer that is not the one the release lists.
// It is no signature: whoever can publish a release of the repository can publish both files.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	appRepo           = "kejith/wow-tools-forever"
	updateCheckAge    = time.Hour // a start within the hour does not ask GitHub again
	maxInstallerBytes = 512 << 20
	maxSumsBytes      = 64 << 10
	downloadIdle      = 30 * time.Second // a download without data for this long is given up
	sumsName          = "SHA256SUMS.txt"
)

var (
	plainVersion = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	sha256Hex    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	installerRe  = regexp.MustCompile(`^WoW-Tools-Forever-Setup-(\d+\.\d+\.\d+)\.exe$`)
	sumsLine     = regexp.MustCompile(`^([0-9a-fA-F]{64})\s+\*?(.+)$`)
	digestRe     = regexp.MustCompile(`^sha256:([0-9a-f]{64})$`)
)

func installerName(version string) string { return "WoW-Tools-Forever-Setup-" + version + ".exe" }

// releaseURL is the release page, built from the checked version, never taken from an answer.
func releaseURL(version, repo string) string {
	if repo == "" {
		repo = appRepo
	}
	return fmt.Sprintf("https://github.com/%s/releases/tag/v%s", repo, version)
}

// latestInstaller is what a check remembers of a release; SHA256 only for a version newer than the running
// one.
type latestInstaller struct {
	Version     string  `json:"version"`
	Tag         string  `json:"tag"`
	PublishedAt *string `json:"publishedAt"`
	Size        int64   `json:"size"`
	AssetID     int64   `json:"assetId"`
	SHA256      *string `json:"sha256"`
}

// updateFile is `<dir>/update.json`.
type updateFile struct {
	Repo      string           `json:"repo"`
	CheckedAt int64            `json:"checkedAt"`
	Latest    *latestInstaller `json:"latest"`
}

func validLatest(l *latestInstaller) bool {
	return l != nil && plainVersion.MatchString(l.Version) && l.Size > 0 && l.Size <= maxInstallerBytes &&
		(l.SHA256 == nil || sha256Hex.MatchString(*l.SHA256))
}

// parseSums is the checksum of name in the text sha256sum writes: "<hex>  <name>" per line ("*<name>" in
// binary mode); "" if it is not there.
func parseSums(text, name string) string {
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if m := sumsLine.FindStringSubmatch(line); m != nil && strings.TrimSpace(m[2]) == name {
			return strings.ToLower(m[1])
		}
	}
	return ""
}

func hashFile(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// matches tells whether file is the installer the release lists: the same size and the same checksum.
func matches(file string, l *latestInstaller) bool {
	if l == nil || l.SHA256 == nil {
		return false
	}
	st, err := os.Stat(file)
	if err != nil || st.Size() != l.Size {
		return false
	}
	sum, err := hashFile(file)
	return err == nil && sum == *l.SHA256
}

// updateSource is where updates come from; the zero values are the real GitHub and the real waits.
type updateSource struct {
	repo   string
	api    string
	maxAge time.Duration
	idle   time.Duration
}

func (s updateSource) withDefaults() updateSource {
	if s.repo == "" {
		s.repo = appRepo
	}
	if s.api == "" {
		s.api = githubAPI
	}
	if s.maxAge == 0 {
		s.maxAge = updateCheckAge
	}
	if s.idle == 0 {
		s.idle = downloadIdle
	}
	return s
}

// foundInstaller is the newest release with the ids of its installer and of its SHA256SUMS.txt.
type foundInstaller struct {
	version, tag string
	publishedAt  *string
	size, asset  int64
	digest       string
	sums         int64 // 0 = none
}

// findInstaller reads the newest release. Its installer must carry exactly the name the release workflow
// gives it.
func findInstaller(s updateSource) (*foundInstaller, error) {
	rel, err := latestGithubRelease(s.api, s.repo)
	if err != nil {
		return nil, err
	}
	tag := str(rel.TagName)
	version := strings.TrimPrefix(tag, "v")
	if !strings.HasPrefix(tag, "v") || !plainVersion.MatchString(version) {
		return nil, errors.New(t("Unexpected release name: {tag}", "tag", truncate(tag, 40)))
	}
	f := &foundInstaller{version: version, tag: tag, publishedAt: strPtr(rel.PublishedAt)}
	found := false
	for _, a := range rel.Assets {
		id, ok := integer(a["id"])
		if !ok {
			continue
		}
		size, sized := integer(a["size"])
		switch a["name"] {
		case installerName(version):
			if !sized || size <= 0 {
				return nil, errors.New(t("Release {tag} has no installer", "tag", tag))
			}
			found = true
			f.asset, f.size = id, size
			if m := digestRe.FindStringSubmatch(str(a["digest"])); m != nil {
				f.digest = m[1]
			}
		case sumsName:
			if sized && size <= maxSumsBytes {
				f.sums = id
			}
		}
	}
	if !found {
		return nil, errors.New(t("Release {tag} has no installer", "tag", tag))
	}
	if f.size > maxInstallerBytes {
		return nil, errors.New(t("The installer of release {tag} is too large", "tag", tag))
	}
	return f, nil
}

// checksumOf is the installer's checksum from the release's SHA256SUMS.txt. GitHub's own digest of the asset,
// where the API has one, must say the same.
func checksumOf(s updateSource, f *foundInstaller) (string, error) {
	missing := errors.New(t("Release {tag} has no checksum for {name}", "tag", f.tag, "name", installerName(f.version)))
	if f.sums == 0 {
		return "", missing
	}
	data, err := githubBytes(fmt.Sprintf("%s/repos/%s/releases/assets/%d", s.api, s.repo, f.sums), "application/octet-stream", maxSumsBytes)
	if errors.Is(err, errTooLarge) {
		return "", missing
	}
	if err != nil {
		return "", err
	}
	sum := parseSums(string(data), installerName(f.version))
	if sum == "" {
		return "", missing
	}
	if f.digest != "" && f.digest != sum {
		return "", errors.New(t("The checksums of release {tag} do not agree", "tag", f.tag))
	}
	return sum, nil
}

type checkResult struct {
	latest    *latestInstaller
	available bool
	checkedAt *int64
	err       string
}

// checkRelease asks for the newest release, at most every maxAge (force asks now), and remembers the answer
// in `<dir>/update.json`. It never fails: without an answer the last one counts, with err as the reason.
func checkRelease(dir, current string, s updateSource, force bool) checkResult {
	var known *latestInstaller
	var lastCheck *int64
	if data, err := os.ReadFile(filepath.Join(dir, "update.json")); err == nil {
		var saved updateFile
		if json.Unmarshal(data, &saved) == nil && saved.Repo == s.repo && validLatest(saved.Latest) {
			known = saved.Latest
			if saved.CheckedAt > 0 {
				at := saved.CheckedAt
				lastCheck = &at
			}
		}
	}
	newer := func(l *latestInstaller) bool { return l != nil && compareVersions(l.Version, current) > 0 }
	// A remembered release newer than this version always has its checksum; one without is asked for again.
	usable := known != nil && (!newer(known) || known.SHA256 != nil)
	if usable && !force && lastCheck != nil && time.Since(time.UnixMilli(*lastCheck)) < s.maxAge {
		return checkResult{latest: known, available: newer(known), checkedAt: lastCheck}
	}
	latest, err := func() (*latestInstaller, error) {
		f, err := findInstaller(s)
		if err != nil {
			return nil, err
		}
		l := &latestInstaller{Version: f.version, Tag: f.tag, PublishedAt: f.publishedAt, Size: f.size, AssetID: f.asset}
		if newer(l) {
			sum, err := checksumOf(s, f)
			if err != nil {
				return nil, err
			}
			l.SHA256 = &sum
		}
		return l, nil
	}()
	if err != nil {
		reason := err.Error()
		var gh *githubError
		if errors.As(err, &gh) && gh.notFound {
			reason = t("{repo} has no release yet.", "repo", s.repo)
		}
		if !usable {
			return checkResult{err: reason}
		}
		return checkResult{latest: known, available: newer(known), checkedAt: lastCheck, err: reason}
	}
	now := time.Now().UnixMilli()
	if err := os.MkdirAll(dir, 0o755); err == nil {
		data, _ := json.MarshalIndent(updateFile{Repo: s.repo, CheckedAt: now, Latest: latest}, "", "  ")
		os.WriteFile(filepath.Join(dir, "update.json"), data, 0o644)
	}
	return checkResult{latest: latest, available: newer(latest), checkedAt: &now}
}

// downloadInstaller fetches the installer of latest to `<dir>/WoW-Tools-Forever-Setup-<version>.exe` and
// returns the path. The file gets its name only after size and checksum fit; a download that is already
// there is kept.
func downloadInstaller(dir string, latest *latestInstaller, s updateSource, onProgress func(received, total int64)) (string, error) {
	name := installerName(latest.Version)
	if !validLatest(latest) || latest.SHA256 == nil {
		return "", errors.New(t("Release {tag} has no checksum for {name}", "tag", latest.Tag, "name", name))
	}
	file := filepath.Join(dir, name)
	if matches(file, latest) {
		return file, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	part := file + ".part"
	defer os.Remove(part)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // ends a download that was left early
	var idleMu sync.Mutex
	stalled := false
	timer := time.AfterFunc(s.idle, func() {
		idleMu.Lock()
		stalled = true
		idleMu.Unlock()
		cancel()
	})
	defer timer.Stop()
	alive := func() { timer.Reset(s.idle) }
	stopped := func(err error) error {
		idleMu.Lock()
		defer idleMu.Unlock()
		reason := netReason(err)
		if stalled {
			reason = t("no data for {n} s", "n", int(math.Floor(s.idle.Seconds()+0.5)))
		}
		return errors.New(t("The download of the update stopped ({reason})", "reason", reason))
	}

	// The asset is addressed by its id on the API host, never by a URL taken from the answer.
	res, err := githubGet(ctx, fmt.Sprintf("%s/repos/%s/releases/assets/%d", s.api, s.repo, latest.AssetID), "application/octet-stream")
	if err != nil {
		if ctx.Err() != nil {
			return "", stopped(ctx.Err())
		}
		return "", err
	}
	defer res.Body.Close()
	out, err := os.Create(part)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	var received int64
	buf := make([]byte, 64<<10)
	for {
		alive()
		n, err := res.Body.Read(buf)
		if n > 0 {
			received += int64(n)
			if received > latest.Size { // more than the release lists: not the announced file
				break
			}
			h.Write(buf[:n])
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				return "", werr
			}
			onProgress(received, latest.Size)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			out.Close()
			return "", stopped(err)
		}
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	if received != latest.Size || hex.EncodeToString(h.Sum(nil)) != *latest.SHA256 {
		return "", errors.New(t("The downloaded installer does not match its checksum."))
	}
	if err := os.Rename(part, file); err != nil {
		return "", err
	}
	cleanupUpdates(dir, "", name)
	return file, nil
}

// cleanupUpdates removes what is no longer needed in the update folder: unfinished downloads, and installers
// of versions that are not newer than current (the update is done). keep names the one file that stays
// whatever its version; with it and without current every other installer goes.
func cleanupUpdates(dir, current, keep string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if name == keep {
			continue
		}
		m := installerRe.FindStringSubmatch(name)
		stale := strings.HasSuffix(name, ".part") || (m != nil && (current == "" || compareVersions(m[1], current) <= 0))
		if !stale {
			continue
		}
		// Right after an update the installer still runs and Windows does not let go of its file: it stays for
		// the next tidying, and the other files go now.
		os.Remove(filepath.Join(dir, name))
	}
}

// ---------- installer ----------

// installerArgs are what the installer (installer/installer.nsi) reads: --updated = an update (no pages,
// shortcuts stay as the user left them), /S = silent, --force-run = start the app when done. It installs
// where the app is and waits for the running app to exit.
func installerArgs(relaunch bool) []string {
	args := []string{"--updated", "/S"}
	if relaunch {
		args = append(args, "--force-run")
	}
	return args
}

// launchInstaller starts the installer detached from the app, which quits right after.
func launchInstaller(file string, relaunch bool) error {
	cmd := exec.Command(file, installerArgs(relaunch)...)
	cmd.SysProcAttr = detachedProcess()
	if err := cmd.Start(); err != nil {
		reason := err.Error()
		var pe *os.PathError
		if errors.As(err, &pe) {
			reason = pe.Err.Error()
		}
		return errors.New(t("The installer could not be started ({reason})", "reason", reason))
	}
	return cmd.Process.Release()
}

// ---------- the updater ----------

type updateLatest struct {
	Version     string  `json:"version"`
	Tag         string  `json:"tag"`
	URL         string  `json:"url"`
	PublishedAt *string `json:"publishedAt"`
	Size        int64   `json:"size"`
}

type updateProgress struct {
	Received int64 `json:"received"`
	Total    int64 `json:"total"`
}

// updateState is what the window shows:
//
//	reason   why this app cannot install an update: "source" (not the installed app), "platform" (no
//	         Windows), or null. It still checks and shows what is new.
//	phase    idle (never checked) | checking | current | available | downloading | ready | installing
type updateState struct {
	Current    string          `json:"current"`
	CanInstall bool            `json:"canInstall"`
	Reason     *string         `json:"reason"`
	Automatic  bool            `json:"automatic"`
	Updated    bool            `json:"updated"`
	Phase      string          `json:"phase"`
	Latest     *updateLatest   `json:"latest"`
	CheckedAt  *int64          `json:"checkedAt"`
	Progress   *updateProgress `json:"progress"`
	Error      *string         `json:"error"`
}

type installResult struct {
	updateState
	Started bool `json:"started"`
}

// updater holds what the window shows and runs the steps one at a time.
type updater struct {
	dir, current string
	reason       string
	automatic    func() bool              // download a found update right away and install it when the app closes?
	onState      func(updateState)        // called with every new state
	launch       func(string, bool) error // starts the installer
	src          updateSource

	mu   sync.Mutex
	s    updateState
	full *latestInstaller // the release as the check gave it, with asset id and checksum
	file string           // the downloaded installer, once it is there
}

type updaterOpts struct {
	dir, current, reason string
	automatic            func() bool
	onState              func(updateState)
	updated              bool
	launch               func(string, bool) error
	src                  updateSource
}

func newUpdater(o updaterOpts) *updater {
	u := &updater{dir: o.dir, current: o.current, reason: o.reason, automatic: o.automatic, onState: o.onState,
		launch: o.launch, src: o.src.withDefaults()}
	if u.automatic == nil {
		u.automatic = func() bool { return true }
	}
	if u.onState == nil {
		u.onState = func(updateState) {}
	}
	if u.launch == nil {
		u.launch = launchInstaller
	}
	u.s = updateState{Current: o.current, CanInstall: o.reason == "", Reason: nullable(o.reason), Updated: o.updated, Phase: "idle"}
	cleanupUpdates(u.dir, u.current, "")
	// The installer starts the updated app before it exits itself, so its file is still locked now.
	if o.updated {
		time.AfterFunc(time.Minute, func() { cleanupUpdates(u.dir, u.current, "") })
	}
	return u
}

func (u *updater) snapshot() updateState {
	s := u.s
	s.Automatic = u.automatic()
	if s.Latest != nil {
		l := *s.Latest
		s.Latest = &l
	}
	if s.Progress != nil {
		p := *s.Progress
		s.Progress = &p
	}
	return s
}

func (u *updater) state() updateState {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.snapshot()
}

// set changes the state and tells the window.
func (u *updater) set(change func(*updateState)) updateState {
	u.mu.Lock()
	change(&u.s)
	now := u.snapshot()
	u.mu.Unlock()
	u.onState(now)
	return now
}

func (u *updater) busy() bool {
	return u.s.Phase == "checking" || u.s.Phase == "downloading" || u.s.Phase == "installing"
}

func errPtr(msg string) *string { return nullable(msg) }

func (u *updater) check(force bool) updateState {
	u.mu.Lock()
	if u.busy() {
		defer u.mu.Unlock()
		return u.snapshot()
	}
	u.s.Phase, u.s.Error = "checking", nil
	now := u.snapshot()
	u.mu.Unlock()
	u.onState(now)
	cleanupUpdates(u.dir, u.current, "")

	r := checkRelease(u.dir, u.current, u.src, force)
	var latest *updateLatest
	if r.latest != nil {
		latest = &updateLatest{Version: r.latest.Version, Tag: r.latest.Tag, URL: releaseURL(r.latest.Version, u.src.repo),
			PublishedAt: r.latest.PublishedAt, Size: r.latest.Size}
	}
	seen := func(s *updateState) {
		s.Latest, s.CheckedAt, s.Error, s.Progress = latest, r.checkedAt, errPtr(r.err), nil
	}
	u.mu.Lock()
	u.full = r.latest
	u.mu.Unlock()
	if !r.available {
		return u.set(func(s *updateState) {
			seen(s)
			s.Phase = "idle"
			if r.latest != nil {
				s.Phase = "current"
			}
		})
	}
	target := filepath.Join(u.dir, installerName(r.latest.Version))
	if u.reason == "" && matches(target, r.latest) {
		u.mu.Lock()
		u.file = target
		u.mu.Unlock()
		return u.set(func(s *updateState) { seen(s); s.Phase = "ready" })
	}
	st := u.set(func(s *updateState) { seen(s); s.Phase = "available" })
	if u.reason == "" && u.automatic() && r.err == "" {
		return u.download()
	}
	return st
}

func (u *updater) download() updateState {
	u.mu.Lock()
	if u.s.Phase != "available" || u.reason != "" || u.full == nil {
		defer u.mu.Unlock()
		return u.snapshot()
	}
	full := u.full
	u.s.Phase, u.s.Error, u.s.Progress = "downloading", nil, &updateProgress{Received: 0, Total: full.Size}
	now := u.snapshot()
	u.mu.Unlock()
	u.onState(now)
	var told time.Time
	file, err := downloadInstaller(u.dir, full, u.src, func(received, total int64) {
		if time.Since(told) < 250*time.Millisecond {
			return
		}
		told = time.Now()
		u.set(func(s *updateState) { s.Progress = &updateProgress{Received: received, Total: total} })
	})
	if err != nil {
		return u.set(func(s *updateState) { s.Phase, s.Progress, s.Error = "available", nil, errPtr(err.Error()) })
	}
	u.mu.Lock()
	u.file = file
	u.mu.Unlock()
	return u.set(func(s *updateState) { s.Phase, s.Progress = "ready", nil })
}

// install starts the installer; Started tells the caller to quit the app so the installer can replace it.
func (u *updater) install(relaunch bool) installResult {
	u.mu.Lock()
	if u.s.Phase != "ready" || u.reason != "" {
		defer u.mu.Unlock()
		return installResult{updateState: u.snapshot()}
	}
	u.s.Phase, u.s.Error = "installing", nil
	file, full := u.file, u.full
	now := u.snapshot()
	u.mu.Unlock()
	u.onState(now)
	err := func() error {
		// The file waited in the data folder, perhaps for days: it is checked again right before it runs.
		if !matches(file, full) {
			os.Remove(file)
			return errors.New(t("The downloaded installer does not match its checksum."))
		}
		return u.launch(file, relaunch)
	}()
	if err != nil {
		st := u.set(func(s *updateState) {
			s.Phase = "available"
			if exists(file) {
				s.Phase = "ready"
			}
			s.Error = errPtr(err.Error())
		})
		return installResult{updateState: st}
	}
	return installResult{updateState: u.state(), Started: true}
}

// installsOnQuit: a downloaded update goes in when the app closes, unless the user turned the automatic off.
func (u *updater) installsOnQuit() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.s.Phase == "ready" && u.reason == "" && u.automatic()
}

// settingsChanged: the switch "automatically" changed. Tell the window, and fetch an update that was only
// shown so far.
func (u *updater) settingsChanged() updateState {
	u.mu.Lock()
	fetch := u.s.Phase == "available" && u.reason == "" && u.automatic()
	u.mu.Unlock()
	if fetch {
		return u.download()
	}
	return u.set(func(*updateState) {})
}
