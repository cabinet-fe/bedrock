package bedctl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Downloader fetches release assets with mirror rotation and checksum
// verification. All requests go through one http.Client, which honors the
// standard HTTPS_PROXY/HTTP_PROXY/NO_PROXY environment variables, so a
// system-level proxy applies to GitHub API and asset downloads alike.
type Downloader struct {
	Base   string       // release base, e.g. https://github.com/cabinet-fe/bedrock
	Client *http.Client // nil → default client
	Mirror string       // current preferred mirror prefix ("" = direct)
	// APIBase is the GitHub API root, overridable in tests.
	APIBase string
	Tries   time.Duration // per-attempt overall timeout, default 15m
	// OnMirrorHit, when set, is called with the mirror prefix that served a
	// successful download so callers can persist it.
	OnMirrorHit func(mirror string)
}

// CandidateMirrors lists the mirror prefixes to try in order for a base URL.
// Only github.com bases get rotation (custom test bases hit exactly one URL).
// The preferred mirror goes first, then direct, then the builtins; repeated
// candidates are harmless retries.
func CandidateMirrors(base, preferred string, builtins []string) []string {
	if !strings.HasPrefix(base, "https://github.com/") {
		return []string{""}
	}
	var out []string
	seen := map[string]bool{}
	add := func(m string) {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	add(preferred)
	add("")
	for _, b := range builtins {
		add(b)
	}
	return out
}

func (d *Downloader) client() *http.Client {
	if d.Client != nil {
		return d.Client
	}
	return http.DefaultClient
}

// Fetch downloads a repo-relative path (e.g. /releases/download/v1/asset)
// to dest, rotating mirror candidates on failure. It returns the mirror
// prefix that served the file ("" = direct).
func (d *Downloader) Fetch(ctx context.Context, path, dest string) (string, error) {
	builtins := Builtins
	candidates := CandidateMirrors(d.Base, d.Mirror, builtins)
	tryTimeout := d.Tries
	if tryTimeout == 0 {
		tryTimeout = 15 * time.Minute
	}
	var lastErr error
	for _, m := range candidates {
		base := d.Base
		if m != "" {
			base = m + d.Base
		}
		target := base + path
		Info("下载 %s", target)
		if _, err := os.Stat(dest); err == nil {
			_ = os.Remove(dest)
		}
		err := d.fetchOnce(ctx, target, dest, tryTimeout)
		if err == nil {
			if m != "" && d.OnMirrorHit != nil {
				d.OnMirrorHit(m)
			}
			return m, nil
		}
		lastErr = err
		_ = os.Remove(dest)
	}
	return "", fmt.Errorf("下载失败 %s: %w（可尝试 --mirror 指定镜像后重试）", path, lastErr)
}

func (d *Downloader) fetchOnce(ctx context.Context, target, dest string, timeout time.Duration) error {
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(tctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := d.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, copyErr := io.Copy(f, resp.Body)
	if copyErr != nil {
		return copyErr
	}
	return f.Close()
}

// DownloadVerified fetches asset from the given tag plus its per-platform
// checksum file and verifies the SHA256 before returning.
func (d *Downloader) DownloadVerified(ctx context.Context, asset, tag, suffix, dest string) error {
	if _, err := d.Fetch(ctx, fmt.Sprintf("/releases/download/%s/%s", url.PathEscape(tag), asset), dest); err != nil {
		return err
	}
	sumFile := dest + ".sha256"
	if _, err := d.Fetch(ctx, fmt.Sprintf("/releases/download/%s/bedrock-%s.sha256", url.PathEscape(tag), suffix), sumFile); err != nil {
		return err
	}
	defer os.Remove(sumFile)
	want, err := checksumFor(sumFile, asset)
	if err != nil {
		return err
	}
	got, err := fileSHA256(dest)
	if err != nil {
		return err
	}
	if want != got {
		return fmt.Errorf("SHA256 校验失败: %s\n  期望: %s\n  实际: %s", asset, want, got)
	}
	return nil
}

func checksumFor(sumFile, asset string) (string, error) {
	data, err := os.ReadFile(sumFile)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == asset {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("校验文件中没有 %s 的记录，请确认发布产物", asset)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
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

// LatestTag resolves the newest release tag: the GitHub API first, then the
// releases/latest redirect Location (some mirrors 302 to /releases/tag/<tag>,
// plain GitHub included; mirrors that render HTML are skipped naturally).
func (d *Downloader) LatestTag(ctx context.Context) (string, error) {
	if tag := d.latestViaAPI(ctx); tag != "" {
		return tag, nil
	}
	prefixes := CandidateMirrors(d.Base, d.Mirror, Builtins)
	for _, p := range prefixes {
		if !strings.HasPrefix(d.Base, "https://github.com/") && p != "" {
			continue
		}
		base := d.Base
		if p != "" {
			base = p + d.Base
		}
		if tag := d.latestViaRedirect(ctx, base); tag != "" {
			return tag, nil
		}
	}
	return "", errors.New("无法获取最新版本号（GitHub API 与 release 页均失败）。请用 --version <TAG> 指定版本后重试")
}

func (d *Downloader) latestViaAPI(ctx context.Context) string {
	apiBase := d.APIBase
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		apiBase+"/repos/"+Repo+"/releases/latest", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := d.client().Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.TagName)
}

func (d *Downloader) latestViaRedirect(ctx context.Context, base string) string {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/releases/latest", nil)
	if err != nil {
		return ""
	}
	// Do not follow the redirect; the Location header carries the tag.
	client := *d.client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	loc := resp.Header.Get("Location")
	if i := strings.LastIndex(loc, "/releases/tag/"); i >= 0 {
		tag := loc[i+len("/releases/tag/"):]
		if j := strings.IndexAny(tag, "?#"); j >= 0 {
			tag = tag[:j]
		}
		return tag
	}
	return ""
}
