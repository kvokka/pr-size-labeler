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

	"pr-size-labeler/internal/auth"
	"pr-size-labeler/internal/githubapi"
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
			writeJSON(w, []map[string]any{
				{"filename": "generated/bundle.js", "additions": 200, "deletions": 50, "patch": "@@ -1 +1 @@\n-const old = 1;\n+const newer = 2;\n"},
				{"filename": "internal/service.go", "additions": 20, "deletions": 4, "patch": "@@ -1,2 +1,2 @@\n-old\n+new\n-abc\n+xy\n context\n"},
			})
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
		false,
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
	for _, want := range []string{
		`pull_request stage=start action="opened" repo=acme/widgets pr_number=7 accepted pull_request event`,
		`pull_request stage=token action="opened" repo=acme/widgets pr_number=7 requesting installation token`,
		`pull_request stage=files action="opened" repo=acme/widgets pr_number=7 fetched 2 pull request files`,
		`pull_request stage=selection action="opened" repo=acme/widgets pr_number=7 effective_lines=24 effective_symbols=11 selected_label=size/S`,
		`pull_request stage=done action="opened" repo=acme/widgets pr_number=7 completed pull request processing with label size/S`,
	} {
		assertLogContains(t, logs.String(), want)
	}
	assertLogContains(t, logs.String(), `incoming request method=POST path=/ event="pull_request"`)
	assertLogContains(t, logs.String(), `event="pull_request"`)
	assertLogNotContains(t, logs.String(), `source="203.0.113.10"`)
	assertLogNotContains(t, logs.String(), `installation_id=42`)
}

func TestPullRequestLogsFailingStage(t *testing.T) {
	handler := NewHandler(
		"secret",
		auth.StaticTokenProvider("test-token"),
		func(token string) *githubapi.Client {
			return githubapi.NewClient("http://127.0.0.1:1/", token, &http.Client{})
		},
		false,
	)
	var logs bytes.Buffer
	handler.logger = log.New(&logs, "", 0)

	payload := map[string]any{
		"action":       "opened",
		"installation": map[string]any{"id": 42},
		"repository":   map[string]any{"name": "widgets", "owner": map[string]any{"login": "acme"}},
		"pull_request": map[string]any{
			"number":    7,
			"additions": 10,
			"deletions": 1,
			"labels":    []map[string]any{},
			"base":      map[string]any{"ref": "main"},
		},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", signPayload("secret", body))
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadGateway {
		t.Fatalf("ServeHTTP status = %d, want 502; body=%s", resp.Code, resp.Body.String())
	}
	assertLogContains(t, logs.String(), `pull_request stage=files action="opened" repo=acme/widgets pr_number=7 fetching pull request files`)
	assertLogContains(t, logs.String(), `pull_request stage=files action="opened" repo=acme/widgets pr_number=7 error=`)
}

func TestPullRequestOpenedSelectsLargerLabelFromSymbols(t *testing.T) {
	recorded := []requestRecord{}
	largePatch := "@@ -1 +1 @@\n+" + strings.Repeat("x", 10000) + "\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		recorded = append(recorded, requestRecord{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body)})
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/pulls/7/files":
			writeJSON(w, []map[string]any{{"filename": "internal/symbol_heavy.go", "additions": 1, "deletions": 0, "patch": largePatch}})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/contents/.gitattributes":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/contents/.github/labels.yml":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && r.URL.RequestURI() == "/repos/acme/widgets/labels/size%2FL":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/labels":
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/issues/7/labels":
			w.WriteHeader(http.StatusOK)
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
		false,
	)

	payload := map[string]any{
		"action":       "opened",
		"installation": map[string]any{"id": 42},
		"repository":   map[string]any{"name": "widgets", "owner": map[string]any{"login": "acme"}},
		"pull_request": map[string]any{
			"number":    7,
			"additions": 1,
			"deletions": 0,
			"labels":    []map[string]any{},
			"base":      map[string]any{"ref": "main"},
		},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", signPayload("secret", body))
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	assertContainsRequest(t, recorded, http.MethodPost, "/repos/acme/widgets/issues/7/labels", `{"labels":["size/L"]}`)
}

func TestPullRequestOpenedExcludesGeneratedFilesFromLineAndSymbolTotals(t *testing.T) {
	recorded := []requestRecord{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		recorded = append(recorded, requestRecord{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body)})
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/pulls/7/files":
			writeJSON(w, []map[string]any{
				{"filename": "generated/huge.js", "additions": 1, "deletions": 0, "patch": "@@ -1 +1 @@\n+" + strings.Repeat("x", 10000) + "\n"},
				{"filename": "internal/small.go", "additions": 1, "deletions": 0, "patch": "@@ -0,0 +1 @@\n+abcd\n"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/contents/.gitattributes":
			writeJSON(w, map[string]any{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("generated/* linguist-generated=true\n"))})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/contents/.github/labels.yml":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && r.URL.RequestURI() == "/repos/acme/widgets/labels/size%2FXS":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/labels":
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/issues/7/labels":
			w.WriteHeader(http.StatusOK)
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
		false,
	)
	var logs bytes.Buffer
	handler.logger = log.New(&logs, "", 0)

	payload := map[string]any{
		"action":       "opened",
		"installation": map[string]any{"id": 42},
		"repository":   map[string]any{"name": "widgets", "owner": map[string]any{"login": "acme"}},
		"pull_request": map[string]any{
			"number":    7,
			"additions": 2,
			"deletions": 0,
			"labels":    []map[string]any{},
			"base":      map[string]any{"ref": "main"},
		},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", signPayload("secret", body))
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	assertContainsRequest(t, recorded, http.MethodPost, "/repos/acme/widgets/issues/7/labels", `{"labels":["size/XS"]}`)
	assertLogContains(t, logs.String(), `effective_lines=1 effective_symbols=4 selected_label=size/XS`)
}

func TestPullRequestOpenedUsesExplicitConfiguredSymbolThresholds(t *testing.T) {
	recorded := []requestRecord{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		recorded = append(recorded, requestRecord{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body)})
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/pulls/7/files":
			writeJSON(w, []map[string]any{{"filename": "internal/symbol_custom.go", "additions": 1, "deletions": 0, "patch": "@@ -0,0 +1 @@\n+" + strings.Repeat("x", 250) + "\n"}})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/contents/.gitattributes":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/contents/.github/labels.yml":
			writeJSON(w, map[string]any{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("L:\n  symbols: 250\n"))})
		case r.Method == http.MethodGet && r.URL.RequestURI() == "/repos/acme/widgets/labels/size%2FL":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/labels":
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/issues/7/labels":
			w.WriteHeader(http.StatusOK)
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
		false,
	)

	payload := map[string]any{
		"action":       "opened",
		"installation": map[string]any{"id": 42},
		"repository":   map[string]any{"name": "widgets", "owner": map[string]any{"login": "acme"}},
		"pull_request": map[string]any{
			"number":    7,
			"additions": 1,
			"deletions": 0,
			"labels":    []map[string]any{},
			"base":      map[string]any{"ref": "main"},
		},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", signPayload("secret", body))
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	assertContainsRequest(t, recorded, http.MethodPost, "/repos/acme/widgets/issues/7/labels", `{"labels":["size/L"]}`)
}

func TestPullRequestUsesTopLevelNumberForLogsAndAPICalls(t *testing.T) {
	recorded := []requestRecord{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		recorded = append(recorded, requestRecord{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body)})
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/pulls/9/files":
			writeJSON(w, []map[string]any{{"filename": "internal/service.go", "additions": 1, "deletions": 0, "patch": "@@ -0,0 +1 @@\n+ok\n"}})
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/contents/.gitattributes":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widgets/contents/.github/labels.yml":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && r.URL.RequestURI() == "/repos/acme/widgets/labels/size%2FXS":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/labels":
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/widgets/issues/9/labels":
			w.WriteHeader(http.StatusOK)
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
		false,
	)
	var logs bytes.Buffer
	handler.logger = log.New(&logs, "", 0)

	payload := map[string]any{
		"number":       9,
		"action":       "opened",
		"installation": map[string]any{"id": 42},
		"repository":   map[string]any{"name": "widgets", "owner": map[string]any{"login": "acme"}},
		"pull_request": map[string]any{
			"number": 1,
			"labels": []map[string]any{},
			"base":   map[string]any{"ref": "main"},
		},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", signPayload("secret", body))
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	assertContainsRequest(t, recorded, http.MethodPost, "/repos/acme/widgets/issues/9/labels", `{"labels":["size/XS"]}`)
	assertLogContains(t, logs.String(), `repo=acme/widgets pr_number=9`)
	assertLogNotContains(t, logs.String(), `pr_number=1`)
	assertLogNotContains(t, logs.String(), `pr_number=0`)
}

func TestPrivateRequestLoggingCanBeEnabled(t *testing.T) {
	handler := NewHandler(
		"secret",
		auth.StaticTokenProvider("test-token"),
		func(token string) *githubapi.Client {
			return githubapi.NewClient("http://127.0.0.1:1/", token, &http.Client{})
		},
		true,
	)
	var logs bytes.Buffer
	handler.logger = log.New(&logs, "", 0)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.25:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.10, 203.0.113.11")
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("User-Agent", "GitHub-Hookshot/test")
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("ServeHTTP status = %d, want 405", resp.Code)
	}
	assertLogContains(t, logs.String(), `source="203.0.113.10"`)
	assertLogContains(t, logs.String(), `remote_addr="198.51.100.25:1234"`)
	assertLogContains(t, logs.String(), `user_agent="GitHub-Hookshot/test"`)
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
		false,
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
	assertLogContains(t, logs.String(), `incoming request method=POST path=/ event="pull_request"`)
	assertLogNotContains(t, logs.String(), `source="198.51.100.25"`)
}

func TestPingReturnsOKWithoutGitHubAPIRequests(t *testing.T) {
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
		false,
	)
	var logs bytes.Buffer
	handler.logger = log.New(&logs, "", 0)

	body := []byte(`{"zen":"Favor focus over features."}`)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
	req.RemoteAddr = "198.51.100.25:1234"
	req.Header.Set("X-GitHub-Event", "ping")
	req.Header.Set("X-Hub-Signature-256", signPayload("secret", body))
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want 200", resp.Code)
	}
	if strings.TrimSpace(resp.Body.String()) != `{"status":"ok","event":"ping"}` {
		t.Fatalf("ServeHTTP body = %q, want ping ack JSON", resp.Body.String())
	}
	if called {
		t.Fatal("unexpected outbound GitHub call for ping event")
	}
	assertLogContains(t, logs.String(), `event="ping"`)
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

func TestChangedSymbolsFromPatchCountsOnlyAddedAndDeletedContent(t *testing.T) {
	patch := strings.Join([]string{
		"diff --git a/file.go b/file.go",
		"index 123..456 100644",
		"--- a/file.go",
		"+++ b/file.go",
		"@@ -1,3 +1,4 @@",
		" context line",
		"-old",
		"+new",
		"+",
		"-xy",
		"\\ No newline at end of file",
	}, "\n")

	if got := changedSymbolsFromPatch(patch); got != 8 {
		t.Fatalf("changedSymbolsFromPatch() = %d, want 8", got)
	}
}

func TestChangedSymbolsFromPatchCountsContentStartingWithTripleMarkers(t *testing.T) {
	patch := strings.Join([]string{
		"--- a/file.go",
		"+++ b/file.go",
		"@@ -1,2 +1,2 @@",
		"---bar",
		"+++foo",
	}, "\n")

	if got := changedSymbolsFromPatch(patch); got != 10 {
		t.Fatalf("changedSymbolsFromPatch() = %d, want 10", got)
	}
}

func TestChangedSymbolsFromPatchEmptyPatchCountsZero(t *testing.T) {
	if got := changedSymbolsFromPatch(""); got != 0 {
		t.Fatalf("changedSymbolsFromPatch(empty) = %d, want 0", got)
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

func assertLogNotContains(t *testing.T, logs, forbidden string) {
	t.Helper()
	if strings.Contains(logs, forbidden) {
		t.Fatalf("expected logs not to contain %q, got %s", forbidden, logs)
	}
}
