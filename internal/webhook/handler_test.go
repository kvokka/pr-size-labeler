package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pr-size/internal/auth"
	"pr-size/internal/githubapi"
)

type requestRecord struct {
	Method string
	Path   string
	Body   string
}

func TestPullRequestOpenedAppliesSingleSizeLabel(t *testing.T) {
	recorded := []requestRecord{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		recorded = append(recorded, requestRecord{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body)})
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/pulls/7/files":
			writeJSON(w, []map[string]any{{"filename": "generated/bundle.js", "additions": 200, "deletions": 50}, {"filename": "internal/service.go", "additions": 20, "deletions": 4}})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/contents/.gitattributes":
			writeJSON(w, map[string]any{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("generated/* linguist-generated=true\n"))})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/contents/.github/labels.yml":
			writeJSON(w, map[string]any{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("S:\n  comment: small enough\n"))})
		case r.Method == http.MethodDelete && r.URL.RequestURI() == "/repos/acme/widgets/issues/7/labels/size%2FM":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.RequestURI() == "/repos/acme/widgets/labels/size%2FS":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/labels":
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/issues/7/labels":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/issues/7/comments":
			writeJSON(w, []map[string]any{{"body": "older comment"}})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/issues/7/comments":
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	handler := NewHandler(
		"secret",
		auth.StaticTokenProvider("test-token"),
		func(token string) *githubapi.Client {
			return githubapi.NewClient(server.URL+"/", token, server.Client())
		},
	)
	var logs bytes.Buffer
	handler.logger = log.New(&logs, "", 0)

	payload := map[string]any{
		"action":       "opened",
		"installation": map[string]any{"id": 42},
		"repository":   map[string]any{"name": "widgets", "owner": map[string]any{"login": "acme"}},
		"pull_request": map[string]any{
			"number":    7,
			"additions": 220,
			"deletions": 54,
			"labels":    []map[string]any{{"name": "size/M"}, {"name": "bug"}},
			"base":      map[string]any{"ref": "main"},
		},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
	req.RemoteAddr = "198.51.100.25:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.10, 203.0.113.11")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", signPayload("secret", body))
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}

	assertContainsRequest(t, recorded, http.MethodDelete, "/repos/acme/widgets/issues/7/labels/size%2FM", "")
	assertContainsRequest(t, recorded, http.MethodPost, "/repos/acme/widgets/labels", `{"color":"55A84B","name":"size/S"}`)
	assertContainsRequest(t, recorded, http.MethodPost, "/repos/acme/widgets/issues/7/labels", `{"labels":["size/S"]}`)
	assertContainsRequest(t, recorded, http.MethodPost, "/repos/acme/widgets/issues/7/comments", `{"body":"small enough"}`)
	assertLogContains(t, logs.String(), `source="203.0.113.10"`)
	assertLogContains(t, logs.String(), `event="pull_request"`)
}

func TestInvalidSignatureRejected(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	handler := NewHandler(
		"secret",
		auth.StaticTokenProvider("test-token"),
		func(token string) *githubapi.Client {
			return githubapi.NewClient(server.URL+"/", token, server.Client())
		},
	)
	var logs bytes.Buffer
	handler.logger = log.New(&logs, "", 0)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"action":"opened"}`))
	req.RemoteAddr = "198.51.100.25:1234"
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", "sha256=bad")
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("ServeHTTP status = %d, want 401", resp.Code)
	}
	if called {
		t.Fatal("unexpected outbound GitHub call for invalid signature")
	}
	assertLogContains(t, logs.String(), `source="198.51.100.25"`)
}

func TestRequestSourcePrefersForwardedHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "198.51.100.25:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.10, 203.0.113.11")
	req.Header.Set("X-Real-IP", "203.0.113.12")

	if got := requestSource(req); got != "203.0.113.10" {
		t.Fatalf("requestSource() = %q, want 203.0.113.10", got)
	}
}

func TestRequestSourceFallsBackToRemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "198.51.100.25:1234"

	if got := requestSource(req); got != "198.51.100.25" {
		t.Fatalf("requestSource() = %q, want 198.51.100.25", got)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(err)
	}
}

func signPayload(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func assertContainsRequest(t *testing.T, recorded []requestRecord, method, requestPath, body string) {
	t.Helper()
	for _, record := range recorded {
		if record.Method == method && record.Path == requestPath {
			if body != "" && record.Body != body {
				t.Fatalf("request %s %s body = %s, want %s", method, requestPath, record.Body, body)
			}
			return
		}
	}
	t.Fatalf("missing request %s %s", method, requestPath)
}

func assertLogContains(t *testing.T, logs, want string) {
	t.Helper()
	if !strings.Contains(logs, want) {
		t.Fatalf("expected logs to contain %q, got %s", want, logs)
	}
}
