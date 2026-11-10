package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
)

// newTestAPI returns a restAPI whose requests are served by handler, through go-gh's client as in production.
func newTestAPI(t *testing.T, handler http.HandlerFunc) *restAPI {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := api.NewRESTClient(api.ClientOptions{
		Host:         "github.com",
		AuthToken:    "test-token",
		Transport:    rewriteTransport{target: server.URL},
		LogIgnoreEnv: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &restAPI{client: client}
}

// rewriteTransport sends requests for api.github.com to a test server.
type rewriteTransport struct {
	target string
}

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(rt.target, "http://")
	return http.DefaultTransport.RoundTrip(req)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func notFoundHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
}

func TestRESTCheckRepo(t *testing.T) {
	a := newTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo" {
			notFoundHandler(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"full_name": "owner/repo"})
	})
	if err := a.CheckRepo("owner/repo"); err != nil {
		t.Errorf("CheckRepo(owner/repo) = %v", err)
	}
	if err := a.CheckRepo("owner/missing"); !errors.Is(err, errNotFound) {
		t.Errorf("got %v, want errNotFound", err)
	}
}

func TestRESTLatestRelease(t *testing.T) {
	a := newTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/actions/checkout/releases/latest" {
			notFoundHandler(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "token test-token" {
			t.Errorf("Authorization = %q", got)
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"tag_name": "v5.0.0", "created_at": "2026-01-01T00:00:00Z", "published_at": "2026-01-02T00:00:00Z",
		})
	})
	rel, err := a.LatestRelease("actions/checkout")
	if err != nil {
		t.Fatal(err)
	}
	want := release{Tag: "v5.0.0", Published: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}
	if rel.Tag != want.Tag || !rel.Published.Equal(want.Published) {
		t.Errorf("got %+v, want %+v", rel, want)
	}
	if _, err := a.LatestRelease("actions/none"); !errors.Is(err, errNotFound) {
		t.Errorf("got %v, want errNotFound", err)
	}
}

func TestRESTCommit(t *testing.T) {
	a := newTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/actions/checkout/commits/v5.0.0":
			writeJSON(w, http.StatusOK, map[string]any{
				"sha":    shaA,
				"commit": map[string]any{"committer": map[string]string{"date": "2026-03-04T05:06:07Z"}},
			})
		case "/repos/actions/checkout/commits/" + shaB:
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "No commit found for SHA: " + shaB})
		default:
			notFoundHandler(w, r)
		}
	})
	c, err := a.Commit("actions/checkout", "v5.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if c.SHA != shaA || !c.Date.Equal(time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)) {
		t.Errorf("got %+v", c)
	}
	for _, ref := range []string{shaB, shaC} {
		if _, err := a.Commit("actions/checkout", ref); !errors.Is(err, errNotFound) {
			t.Errorf("Commit(%s) returned %v, want errNotFound", ref, err)
		}
	}
}

func TestRESTServerErrorIsNotNotFound(t *testing.T) {
	a := newTestAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "API rate limit exceeded"})
	})
	_, err := a.LatestRelease("actions/checkout")
	if err == nil || errors.Is(err, errNotFound) || !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("got %v, want a rate limit error", err)
	}
}

func TestRESTListDir(t *testing.T) {
	a := newTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/contents/.github/workflows" {
			notFoundHandler(w, r)
			return
		}
		writeJSON(w, http.StatusOK, []map[string]string{
			{"path": ".github/workflows/ci.yml", "type": "file"},
			{"path": ".github/workflows/nested", "type": "dir"},
			{"path": ".github/workflows/release.yaml", "type": "file"},
		})
	})
	paths, err := a.ListDir("owner/repo", ".github/workflows")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{".github/workflows/ci.yml", ".github/workflows/release.yaml"}; !slices.Equal(paths, want) {
		t.Errorf("got %v, want %v", paths, want)
	}
	if _, err := a.ListDir("owner/other", ".github/workflows"); !errors.Is(err, errNotFound) {
		t.Errorf("got %v, want errNotFound", err)
	}
}

func TestRESTGetAndPutFile(t *testing.T) {
	var put map[string]string
	a := newTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/contents/.github/workflows/ci.yml" {
			notFoundHandler(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			// GitHub wraps base64 content at 60 characters.
			encoded := base64.StdEncoding.EncodeToString([]byte(sampleWorkflow))
			var wrapped strings.Builder
			for len(encoded) > 60 {
				wrapped.WriteString(encoded[:60] + "\n")
				encoded = encoded[60:]
			}
			wrapped.WriteString(encoded)
			writeJSON(w, http.StatusOK, map[string]string{"content": wrapped.String(), "encoding": "base64", "sha": "blob-sha"})
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &put); err != nil {
				t.Errorf("invalid body %s: %v", body, err)
			}
			writeJSON(w, http.StatusOK, map[string]any{"content": map[string]string{}})
		}
	})
	content, sha, err := a.GetFile("owner/repo", ".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != sampleWorkflow || sha != "blob-sha" {
		t.Errorf("got %q, %q", content, sha)
	}
	if _, _, err := a.GetFile("owner/repo", ".github/workflows/missing.yml"); !errors.Is(err, errNotFound) {
		t.Errorf("got %v, want errNotFound", err)
	}

	if err := a.PutFile("owner/repo", ".github/workflows/ci.yml", "chore: update", []byte("new"), "blob-sha"); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"message": "chore: update", "content": base64.StdEncoding.EncodeToString([]byte("new")), "sha": "blob-sha"}
	if len(put) != len(want) || put["message"] != want["message"] || put["content"] != want["content"] || put["sha"] != want["sha"] {
		t.Errorf("PUT body = %v, want %v", put, want)
	}
}

func TestAnonymousClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected Authorization header")
		}
		if r.URL.Path == "/repos/actions/checkout/releases/latest" {
			writeJSON(w, http.StatusOK, map[string]string{"tag_name": "v5.0.0", "created_at": "2026-01-01T00:00:00Z"})
			return
		}
		notFoundHandler(w, r)
	}))
	defer server.Close()
	a := &restAPI{client: anonymousClient{http: server.Client(), baseURL: server.URL + "/"}}
	rel, err := a.LatestRelease("actions/checkout")
	if err != nil {
		t.Fatal(err)
	}
	// Without published_at, the creation time is used.
	if rel.Tag != "v5.0.0" || !rel.Published.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("got %+v", rel)
	}
	if _, err := a.LatestRelease("actions/none"); !errors.Is(err, errNotFound) {
		t.Errorf("got %v, want errNotFound", err)
	}
}

func TestEscapePath(t *testing.T) {
	if got := escapePath("owner/repo/.github/workflows/my workflow.yml"); got != "owner/repo/.github/workflows/my%20workflow.yml" {
		t.Errorf("got %q", got)
	}
}
