package bedctl

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// newMirrorTestServer serves /base/releases (direct probe) plus per-mirror
// prefixes /m1/ and /m2/ (mirrors prefix the full URL, so their path becomes
// /m1/<base URL>/releases); each route can be up or down.
func newMirrorTestServer(t *testing.T, directOK bool, m1OK, m2OK bool) (*httptest.Server, string, []string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/base/releases", func(w http.ResponseWriter, _ *http.Request) {
		if !directOK {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	for _, spec := range []struct {
		prefix string
		ok     bool
	}{{"m1", m1OK}, {"m2", m2OK}} {
		ok := spec.ok
		mux.HandleFunc("/"+spec.prefix+"/", func(w http.ResponseWriter, _ *http.Request) {
			if !ok {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			_, _ = w.Write([]byte("ok"))
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, srv.URL + "/base", []string{srv.URL + "/m1/", srv.URL + "/m2/"}
}

func mirrorForTest(t *testing.T, base string, builtins []string) (*State, *MirrorSource) {
	t.Helper()
	st := &State{Path: filepath.Join(t.TempDir(), "bedctl.env")}
	ms := &MirrorSource{Base: base, Builtins: builtins, State: st}
	return st, ms
}

func TestMirrorFlagWinsAndPersists(t *testing.T) {
	srv, base, builtins := newMirrorTestServer(t, true, false, false)
	_ = srv
	st, ms := mirrorForTest(t, base, builtins)
	ms.FlagMirror = NormalizeMirror("gh-proxy.example/")
	if err := ms.Pick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := ms.Selected(); got != "https://gh-proxy.example/" {
		t.Fatalf("selected = %q", got)
	}
	if v, _ := st.Get(KeyMirror); v != "https://gh-proxy.example/" {
		t.Fatalf("persisted = %q", v)
	}
}

func TestMirrorNoMirrorClearsSavedValue(t *testing.T) {
	srv, base, builtins := newMirrorTestServer(t, false, false, false)
	_ = srv
	st, ms := mirrorForTest(t, base, builtins)
	_ = st.Set(KeyMirror, "https://stale.example/")
	ms.FlagNoMirror = true
	if err := ms.Pick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := ms.Selected(); got != "" {
		t.Fatalf("selected = %q, want direct", got)
	}
	v, ok := st.Get(KeyMirror)
	if !ok || v != "" {
		t.Fatalf("direct choice must clear stale mirror, got %q (present=%v)", v, ok)
	}
}

func TestMirrorSavedMirrorUsedSilently(t *testing.T) {
	srv, base, builtins := newMirrorTestServer(t, false, true, false)
	_ = srv
	st, ms := mirrorForTest(t, base, builtins)
	_ = st.Set(KeyMirror, ms.Builtins[0])
	// A saved mirror that probes OK must be picked without prompting.
	ms.Interactive = true
	ms.PromptMirrors = func([]string, int) (string, error) {
		t.Fatal("must not prompt when the saved mirror works")
		return "", nil
	}
	if err := ms.Pick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := ms.Selected(); got != ms.Builtins[0] {
		t.Fatalf("selected = %q, want saved mirror %q", got, ms.Builtins[0])
	}
}

func TestMirrorNonInteractiveFallsBackToSavedWhenNothingReachable(t *testing.T) {
	srv, base, builtins := newMirrorTestServer(t, false, false, false)
	_ = srv
	st, ms := mirrorForTest(t, base, builtins)
	_ = st.Set(KeyMirror, "https://last-resort.example/")
	if err := ms.Pick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := ms.Selected(); got != "https://last-resort.example/" {
		t.Fatalf("selected = %q, want saved fallback", got)
	}
}

func TestMirrorInteractivePromptDefault(t *testing.T) {
	srv, base, builtins := newMirrorTestServer(t, false, true, false)
	_ = srv
	st, ms := mirrorForTest(t, base, builtins)
	_ = st
	ms.Interactive = true
	ms.PromptMirrors = func(builtins []string, def int) (string, error) {
		if def != 2 {
			t.Fatalf("prompt default = %d, want 2 (first reachable mirror)", def)
		}
		return builtins[0], nil
	}
	if err := ms.Pick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := ms.Selected(); got != ms.Builtins[0] {
		t.Fatalf("selected = %q", got)
	}
}

func TestMirrorPromptErrorPropagates(t *testing.T) {
	srv, base, builtins := newMirrorTestServer(t, false, false, false)
	_ = srv
	_, ms := mirrorForTest(t, base, builtins)
	ms.Interactive = true
	ms.PromptMirrors = func([]string, int) (string, error) {
		return "", fmt.Errorf("未输入镜像 URL")
	}
	if err := ms.Pick(context.Background()); err == nil {
		t.Fatal("Pick must surface the prompt error")
	}
}

func TestMirrorRemember(t *testing.T) {
	st := &State{Path: filepath.Join(t.TempDir(), "bedctl.env")}
	ms := &MirrorSource{Base: "https://example.invalid", State: st}
	ms.Remember("https://worked.example/")
	if v, _ := st.Get(KeyMirror); v != "https://worked.example/" {
		t.Fatalf("Remember not persisted: %q", v)
	}
}

func TestCandidateMirrors(t *testing.T) {
	builtins := []string{"https://b1/", "https://b2/"}
	github := "https://github.com/cabinet-fe/bedrock"
	got := CandidateMirrors(github, "https://preferred/", builtins)
	want := []string{"https://preferred/", "", "https://b1/", "https://b2/"}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidates[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// Custom bases (test servers) never rotate.
	if got := CandidateMirrors("http://127.0.0.1:1/x", "https://preferred/", builtins); len(got) != 1 || got[0] != "" {
		t.Fatalf("custom base candidates = %v, want [\"\"]", got)
	}
}
