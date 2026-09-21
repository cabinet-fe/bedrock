package bedctl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFetchRotatesAndReportsMirror(t *testing.T) {
	// Custom (non-github) bases never rotate: the mirror prefix is ignored
	// and the single URL is fetched directly.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/asset":
			_, _ = w.Write([]byte("hello"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	var hits int
	d := &Downloader{Base: srv.URL, Mirror: "https://mirror.example/"}
	d.OnMirrorHit = func(string) { hits++ }
	dest := filepath.Join(t.TempDir(), "out")
	m, err := d.Fetch(context.Background(), "/asset", dest)
	if err != nil {
		t.Fatal(err)
	}
	if m != "" || hits != 0 {
		t.Fatalf("mirror = %q hits = %d, want direct without hits", m, hits)
	}
	if data, err := os.ReadFile(dest); err != nil || string(data) != "hello" {
		t.Fatalf("content = %q, %v", data, err)
	}
}

func TestDownloadVerifiedChecksums(t *testing.T) {
	payload := []byte("bedrock-binary-bytes")
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/download/v1.0.0/bedrock-linux-amd64":
			_, _ = w.Write(payload)
		case "/releases/download/v1.0.0/bedrock-linux-amd64.sha256":
			_, _ = w.Write([]byte(hexSum + "  bedrock-linux-amd64\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	d := &Downloader{Base: srv.URL}
	dest := filepath.Join(t.TempDir(), "bedrock-linux-amd64")
	if err := d.DownloadVerified(context.Background(), "bedrock-linux-amd64", "v1.0.0", "linux-amd64", dest); err != nil {
		t.Fatal(err)
	}
	// Corrupt checksum file must fail the verification.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if filepath.Base(r.URL.Path) == "bedrock-linux-amd64" {
			_, _ = w.Write(payload)
			return
		}
		_, _ = w.Write([]byte("0000  bedrock-linux-amd64\n"))
	}))
	defer bad.Close()
	dbad := &Downloader{Base: bad.URL}
	if err := dbad.DownloadVerified(context.Background(), "bedrock-linux-amd64", "v1.0.0", "linux-amd64", dest); err == nil {
		t.Fatal("corrupt checksum must fail")
	}
}

func TestLatestTagViaAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/"+Repo+"/releases/latest" {
			_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v2.5.9"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	d := &Downloader{Base: "https://github.com/" + Repo, APIBase: srv.URL}
	tag, err := d.LatestTag(context.Background())
	if err != nil || tag != "v2.5.9" {
		t.Fatalf("tag = %q, %v", tag, err)
	}
}

func TestLatestTagViaRedirect(t *testing.T) {
	// API fails; releases/latest 302 carries the tag like GitHub does.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/"+Repo+"/releases/latest" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.URL.Path == "/releases/latest" {
			w.Header().Set("Location", "https://github.com/"+Repo+"/releases/tag/v9.9.9")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	d := &Downloader{Base: srv.URL, APIBase: srv.URL + "/api"}
	tag, err := d.LatestTag(context.Background())
	if err != nil || tag != "v9.9.9" {
		t.Fatalf("tag = %q, %v", tag, err)
	}
}

func TestLatestTagUnreachable(t *testing.T) {
	// Custom base without rotation: only direct is probed and fails.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	d := &Downloader{Base: srv.URL, APIBase: srv.URL + "/api"}
	if _, err := d.LatestTag(context.Background()); err == nil {
		t.Fatal("expected error when API and redirect probes fail")
	}
}
