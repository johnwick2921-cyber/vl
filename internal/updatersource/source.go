// Package updatersource is the ONE-BUTTON P-A release source: the worker's
// network client for GitHub releases. It owns the ONLY network code in the
// update path — the bot process dials nothing but the worker's unix socket.
//
// A3 rules, all enforced here:
//   - https only (an http:// or other-scheme URL refuses before any dial)
//   - host allow-list (default: the four GitHub hosts, verified 2026-10-02
//     against a real release asset redirect: github.com → 302 →
//     release-assets.githubusercontent.com)
//   - redirects followed up to MaxRedirects hops, every hop allow-listed and
//     https — anything else refuses
//   - Content-Length above MaxBytes refuses BEFORE the body is read; the
//     stream is additionally capped at MaxBytes+1 so a lying server cannot
//     grow the file past the bound
//   - total client timeout Timeout
//   - on ANY failure the partial file is removed before the error returns
//
// Nothing in this package unpacks, hashes or executes anything: the caller
// hands the downloaded tarball to updaterworker.FetchRelease, the SAME
// verification the attended CLI runs.
package updatersource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"vl/internal/updaterwire"
)

// ReleaseRepo is the ONE build-time release-repo constant (fold B1): this
// tree checks THIS repo. The partner tree's updatersource carries the
// SAME package/file/identifier with its own value, so every future sync's
// partner carve-out stays one line. The worker reads updatersource.ReleaseRepo
// — never an env var for the repo (VL_RELEASE_SOURCE only toggles on/off).
const ReleaseRepo = "johnwick2921-cyber/vl"

// DefaultHosts is the production allow-list (fold A3). The asset path was
// verified live 2026-10-02 [A]: the asset URL
// /<repo>/releases/download/v2026.10.01.1/v2026.10.01.1.tar.gz
// answers HTTP/2 302 to release-assets.githubusercontent.com, then 200.
// objects.githubusercontent.com stays listed for tarball source links.
var DefaultHosts = []string{
	"api.github.com",
	"github.com",
	"objects.githubusercontent.com",
	"release-assets.githubusercontent.com",
}

// Limits (fold A3, CTO-decision numbers). Tests may shrink them; production
// uses these defaults.
const (
	DefaultMaxBytes     = 512 << 20 // 512 MiB
	DefaultTimeout      = 5 * time.Minute
	DefaultCacheTTL     = 15 * time.Minute
	DefaultMaxRedirects = 3
)

// Latest is the releases/latest answer reduced to the two fields the check
// uses. TargetCommitish is the 40-hex source sha the release was cut from.
type Latest struct {
	Tag             string `json:"tag"`
	TargetCommitish string `json:"target_commitish"`
}

// Config names every bound. Zero values mean the defaults above; Hosts nil
// means DefaultHosts (tests inject 127.0.0.1-backed httptest servers).
type Config struct {
	Repo         string
	Hosts        []string
	MaxBytes     int64
	Timeout      time.Duration
	CacheTTL     time.Duration
	MaxRedirects int
	// APIBase overrides the releases API base for tests (httptest).
	APIBase string
	// DownloadBase overrides the asset base for tests (httptest). The real
	// one is https://github.com/<repo>/releases/download.
	DownloadBase string
	// Transport replaces the dial transport (tests: an httptest TLS server's
	// client transport, which trusts its cert). The scheme/host allow-list
	// and the redirect policy still apply around it.
	Transport http.RoundTripper
}

func (c Config) withDefaults() Config {
	if c.Repo == "" {
		c.Repo = ReleaseRepo
	}
	if c.Hosts == nil {
		c.Hosts = append([]string(nil), DefaultHosts...)
	}
	if c.MaxBytes <= 0 {
		c.MaxBytes = DefaultMaxBytes
	}
	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeout
	}
	if c.CacheTTL <= 0 {
		c.CacheTTL = DefaultCacheTTL
	}
	if c.MaxRedirects <= 0 {
		c.MaxRedirects = DefaultMaxRedirects
	}
	if c.APIBase == "" {
		c.APIBase = "https://api.github.com"
	}
	if c.DownloadBase == "" {
		c.DownloadBase = "https://github.com"
	}
	return c
}

// ErrRateLimited is returned (and cached) when the API answers 403 or 429:
// the page shows "rate limited, try later", never a retry storm.
var ErrRateLimited = errors.New("updatersource: release API rate limited")

// ErrInvalidTag is returned when the API's tag_name is not a valid release id.
// The tag becomes the tarball basename and FetchRelease's release id, so it is
// validated with the SAME validator the bot uses (updaterwire.ValidReleaseID)
// BEFORE any path is built from it (CTO 05:54 fix).
var ErrInvalidTag = errors.New("updatersource: release tag not valid")

// ErrReleaseCommitUnknown is returned when the tag's commit cannot be
// resolved to a 40-hex sha (the commits/<tag> endpoint answered 404 or a
// non-hex sha). Fail-closed: the release is never offered without a known
// commit.
var ErrReleaseCommitUnknown = errors.New("updatersource: release commit unknown")

// commitishRe is the 40-hex commit form the commits/<tag> answer must
// carry. target_commitish on releases/latest is IGNORED: for a tag release
// GitHub stores the DEFAULT BRANCH there (observed live 2026-10-02: "dev").
var commitishRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

// ErrSizeCap is returned when the download exceeds the size bound. The
// partial file is removed before it is returned.
var ErrSizeCap = errors.New("updatersource: download exceeds the size bound")

// Source is one release source. It is safe for concurrent use; the Latest
// answer is cached for CacheTTL per Source instance.
type Source struct {
	cfg    Config
	client *http.Client

	mu     sync.Mutex
	cached *cachedLatest
}

type cachedLatest struct {
	latest Latest
	err    error
	at     time.Time
}

// New builds a Source with the production client. Tests may also call
// NewWithClient after shrinking the bounds.
func New(cfg Config) *Source {
	cfg = cfg.withDefaults()
	transport := cfg.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	s := &Source{cfg: cfg}
	s.client = &http.Client{
		Timeout:   cfg.Timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= cfg.MaxRedirects {
				return fmt.Errorf("updatersource: more than %d redirects", cfg.MaxRedirects)
			}
			if err := checkURL(req.URL, cfg.Hosts); err != nil {
				return err
			}
			return nil
		},
	}
	return s
}

// checkURL enforces https + the host allow-list for one hop.
func checkURL(u *url.URL, hosts []string) error {
	if u == nil {
		return errors.New("updatersource: nil URL")
	}
	if u.Scheme != "https" {
		return fmt.Errorf("updatersource: scheme %q is not https", u.Scheme)
	}
	h := u.Hostname()
	for _, allowed := range hosts {
		if h == allowed {
			return nil
		}
	}
	return fmt.Errorf("updatersource: host %q is not allow-listed", h)
}

// AllowedHost reports whether h is on the production allow-list.
func AllowedHost(h string) bool {
	for _, allowed := range DefaultHosts {
		if h == allowed {
			return true
		}
	}
	return false
}

func (s *Source) apiLatestURL() string {
	return s.cfg.APIBase + "/repos/" + s.cfg.Repo + "/releases/latest"
}

func (s *Source) assetURL(tag string) string {
	return s.cfg.DownloadBase + "/" + s.cfg.Repo + "/releases/download/" + tag + "/" + tag + ".tar.gz"
}

// Latest returns the newest release for the repo, cached for CacheTTL. A
// 403/429 is cached as ErrRateLimited (the page shows "rate limited, try
// later"); every other error is also cached so a broken source cannot hammer
// the API.
func (s *Source) Latest(ctx context.Context) (Latest, error) {
	s.mu.Lock()
	if s.cached != nil && time.Since(s.cached.at) < s.cfg.CacheTTL {
		c := *s.cached
		s.mu.Unlock()
		return c.latest, c.err
	}
	s.mu.Unlock()

	latest, err := s.latest(ctx)

	s.mu.Lock()
	s.cached = &cachedLatest{latest: latest, err: err, at: time.Now()}
	s.mu.Unlock()
	return latest, err
}

func (s *Source) latest(ctx context.Context) (Latest, error) {
	u, err := url.Parse(s.apiLatestURL())
	if err != nil {
		return Latest{}, err
	}
	if err := checkURL(u, s.cfg.Hosts); err != nil {
		return Latest{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Latest{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "vl-updater")
	resp, err := s.client.Do(req)
	if err != nil {
		return Latest{}, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusForbidden, http.StatusTooManyRequests:
		return Latest{}, ErrRateLimited
	case http.StatusOK:
	default:
		return Latest{}, fmt.Errorf("updatersource: releases API answered %d", resp.StatusCode)
	}
	var raw struct {
		TagName         string `json:"tag_name"`
		TargetCommitish string `json:"target_commitish"`
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := dec.Decode(&raw); err != nil {
		return Latest{}, fmt.Errorf("updatersource: releases API body: %w", err)
	}
	// The tag is network data that becomes a PATH element and a release id:
	// validate it with the wire's own validator before anything else uses it.
	if !updaterwire.ValidReleaseID(raw.TagName) {
		return Latest{}, fmt.Errorf("%w: %q", ErrInvalidTag, raw.TagName)
	}
	// target_commitish is IGNORED (for a tag release GitHub stores the default
	// branch there — observed live 2026-10-02: "dev"). The release's commit is
	// the tag's commit, resolved with the same client bounds.
	sha, err := s.commitForTag(ctx, raw.TagName)
	if err != nil {
		return Latest{}, err
	}
	return Latest{Tag: raw.TagName, TargetCommitish: sha}, nil
}

// commitForTag resolves the commit the tag points at: GET
// /repos/<repo>/commits/<tag> → .sha, validated 40-hex. The tag is already
// ValidReleaseID-validated by latest() before it is placed in the URL.
func (s *Source) commitForTag(ctx context.Context, tag string) (string, error) {
	u, err := url.Parse(s.cfg.APIBase + "/repos/" + s.cfg.Repo + "/commits/" + tag)
	if err != nil {
		return "", err
	}
	if err := checkURL(u, s.cfg.Hosts); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "vl-updater")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusForbidden, http.StatusTooManyRequests:
		return "", ErrRateLimited
	case http.StatusOK:
	case http.StatusNotFound:
		return "", fmt.Errorf("%w: tag %q", ErrReleaseCommitUnknown, tag)
	default:
		return "", fmt.Errorf("updatersource: commits API answered %d", resp.StatusCode)
	}
	var raw struct {
		SHA string `json:"sha"`
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := dec.Decode(&raw); err != nil {
		return "", fmt.Errorf("updatersource: commits API body: %w", err)
	}
	if !commitishRe.MatchString(raw.SHA) {
		return "", fmt.Errorf("%w: tag %q", ErrReleaseCommitUnknown, tag)
	}
	return raw.SHA, nil
}

// Download fetches <tag>.tar.gz into destDir and returns the final path.
// destDir must be an existing absolute directory. The file is written as
// <destDir>/<tag>.tar.gz.part and renamed only on success; on ANY failure
// (size cap, timeout, redirect off-list, http error) the partial is removed
// and nothing else is written.
func (s *Source) Download(ctx context.Context, tag, destDir string) (string, error) {
	// Defense in depth: the caller only passes a validated latest.Tag, but a
	// path element is built from this string — refuse anything the wire would
	// refuse BEFORE touching the filesystem.
	if !updaterwire.ValidReleaseID(tag) {
		return "", fmt.Errorf("%w: %q", ErrInvalidTag, tag)
	}
	if !filepath.IsAbs(destDir) {
		return "", fmt.Errorf("updatersource: destination %q must be an absolute path", destDir)
	}
	if fi, err := os.Stat(destDir); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("updatersource: destination %q is not a directory", destDir)
	}
	u, err := url.Parse(s.assetURL(tag))
	if err != nil {
		return "", err
	}
	if err := checkURL(u, s.cfg.Hosts); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "vl-updater")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("updatersource: asset download answered %d", resp.StatusCode)
	}
	// Content-Length bound BEFORE the body (fold A3): a declared oversize
	// asset refuses without reading it.
	if resp.ContentLength > s.cfg.MaxBytes {
		return "", ErrSizeCap
	}
	final := filepath.Join(destDir, tag+".tar.gz")
	part := final + ".part"
	f, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("updatersource: partial file: %w", err)
	}
	remove := true
	defer func() {
		f.Close()
		if remove {
			_ = os.Remove(part)
		}
	}()
	// Streaming cap: the reader stops at MaxBytes+1 so a lying server (no or
	// wrong Content-Length) cannot grow the file past the bound.
	n, err := io.Copy(f, io.LimitReader(resp.Body, s.cfg.MaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("updatersource: download stream: %w", err)
	}
	if n > s.cfg.MaxBytes {
		return "", ErrSizeCap
	}
	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("updatersource: sync: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("updatersource: close: %w", err)
	}
	if err := os.Rename(part, final); err != nil {
		return "", fmt.Errorf("updatersource: rename: %w", err)
	}
	remove = false
	return final, nil
}

// LatestURL returns the API URL the Source will call (for tests and logs).
func (s *Source) LatestURL() string { return s.apiLatestURL() }

// AssetURL returns the asset URL the Source will download (for tests/logs).
func (s *Source) AssetURL(tag string) string { return s.assetURL(tag) }

// Hosts reports the effective allow-list (for the pin test).
func (s *Source) Hosts() []string { return append([]string(nil), s.cfg.Hosts...) }

// TrimmedHost returns u's hostname — a helper tests use when building
// fixtures. Kept tiny: no validation, tests only.
func TrimmedHost(u string) string { return strings.TrimSpace(u) }
