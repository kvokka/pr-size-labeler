package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
		switch r.URL.RequestURI() {
		case "/repos/acme/widgets/contents/.github/labels.yml?ref=main":
			writeJSON(w, repositoryContentResponse(labelsConfigYAML(false, "", "S:\n  comment: small enough\n")))
		case "/repos/acme/widgets/pulls/7/files?per_page=100&page=1":
			writeJSON(w, []map[string]any{
				{"filename": "generated/bundle.js", "additions": 200, "deletions": 50, "patch": "@@ -1 +1 @@\n-const old = 1;\n+const newer = 2;\n"},
				{"filename": "internal/service.go", "additions": 20, "deletions": 4, "patch": "@@ -1,2 +1,2 @@\n-old\n+new\n-abc\n+xy\n context\n"},
			})
		case "/repos/acme/widgets/contents/.gitattributes?ref=main":
			writeJSON(w, repositoryContentResponse("generated/* linguist-generated=true\n"))
		case "/repos/acme/widgets/labels/size%2FS":
			w.WriteHeader(http.StatusOK)
		case "/repos/acme/widgets/issues/7/labels/size%2FM":
			if r.Method != http.MethodDelete {
				t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
			}
			w.WriteHeader(http.StatusOK)
		case "/repos/acme/widgets/issues/7/labels":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
			}
			w.WriteHeader(http.StatusOK)
		case "/repos/acme/widgets/issues/7/comments?per_page=100&page=1":
			writeJSON(w, []map[string]any{{"body": "older comment"}})
		case "/repos/acme/widgets/issues/7/comments":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
			}
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	handler := newTestHandler(server, false)
	var logs bytes.Buffer
	handler.logger = log.New(&logs, "", 0)

	payload := pullRequestPayload("opened", "main", "main", false, 7, []string{"size/M", "bug"})
	resp := serveWebhook(handler, "pull_request", payload)

	if resp.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	assertContainsRequest(t, recorded, http.MethodDelete, "/repos/acme/widgets/issues/7/labels/size%2FM", "")
	assertContainsRequest(t, recorded, http.MethodPost, "/repos/acme/widgets/issues/7/labels", `{"labels":["size/S"]}`)
	assertContainsRequest(t, recorded, http.MethodPost, "/repos/acme/widgets/issues/7/comments", `{"body":"small enough"}`)
	assertLogContains(t, logs.String(), `pull_request stage=selection action="opened" repo=acme/widgets pr_number=7 effective_lines=24 effective_symbols=11 selected_label=size/S`)
	assertLogContains(t, logs.String(), `pull_request stage=done action="opened" repo=acme/widgets pr_number=7 completed pull request processing with label size/S`)
	assertLogContains(t, logs.String(), `incoming request method=POST path=/ event="pull_request"`)
	assertLogNotContains(t, logs.String(), `installation_id=42`)
}

func TestPullRequestOpenedDoesNothingWhenLabelsConfigMissing(t *testing.T) {
	recorded := []requestRecord{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		recorded = append(recorded, requestRecord{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body)})
		if r.URL.RequestURI() != "/repos/acme/widgets/contents/.github/labels.yml?ref=main" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	handler := newTestHandler(server, false)
	resp := serveWebhook(handler, "pull_request", pullRequestPayload("opened", "main", "main", false, 7, nil))

	if resp.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	assertNoRequestPath(t, recorded, "/repos/acme/widgets/pulls/7/files?per_page=100&page=1")
	assertNoMutationRequests(t, recorded)
}

func TestPullRequestOpenedSkipsInvalidLabelsConfig(t *testing.T) {
	recorded := []requestRecord{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		recorded = append(recorded, requestRecord{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body)})
		if r.URL.RequestURI() != "/repos/acme/widgets/contents/.github/labels.yml?ref=main" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		writeJSON(w, repositoryContentResponse("backfill:\n  enabled: true\n  lookback: 1y\n"))
	}))
	defer server.Close()

	handler := newTestHandler(server, false)
	resp := serveWebhook(handler, "pull_request", pullRequestPayload("opened", "main", "main", false, 7, nil))

	if resp.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	assertNoRequestPath(t, recorded, "/repos/acme/widgets/pulls/7/files?per_page=100&page=1")
	assertNoMutationRequests(t, recorded)
}

func TestPullRequestLogsFailingStage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.RequestURI() {
		case "/repos/acme/widgets/contents/.github/labels.yml?ref=main":
			writeJSON(w, repositoryContentResponse(labelsConfigYAML(false, "", "")))
		case "/repos/acme/widgets/pulls/7/files?per_page=100&page=1":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	handler := newTestHandler(server, false)
	var logs bytes.Buffer
	handler.logger = log.New(&logs, "", 0)

	resp := serveWebhook(handler, "pull_request", pullRequestPayload("opened", "main", "main", false, 7, nil))

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
		switch r.URL.RequestURI() {
		case "/repos/acme/widgets/contents/.github/labels.yml?ref=main":
			writeJSON(w, repositoryContentResponse(labelsConfigYAML(false, "", "")))
		case "/repos/acme/widgets/pulls/7/files?per_page=100&page=1":
			writeJSON(w, []map[string]any{{"filename": "internal/symbol_heavy.go", "additions": 1, "deletions": 0, "patch": largePatch}})
		case "/repos/acme/widgets/contents/.gitattributes?ref=main":
			w.WriteHeader(http.StatusNotFound)
		case "/repos/acme/widgets/labels/size%2FL":
			w.WriteHeader(http.StatusOK)
		case "/repos/acme/widgets/issues/7/labels":
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	handler := newTestHandler(server, false)
	resp := serveWebhook(handler, "pull_request", pullRequestPayload("opened", "main", "main", false, 7, nil))

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
		switch r.URL.RequestURI() {
		case "/repos/acme/widgets/contents/.github/labels.yml?ref=main":
			writeJSON(w, repositoryContentResponse(labelsConfigYAML(false, "", "")))
		case "/repos/acme/widgets/pulls/7/files?per_page=100&page=1":
			writeJSON(w, []map[string]any{
				{"filename": "generated/huge.js", "additions": 1, "deletions": 0, "patch": "@@ -1 +1 @@\n+" + strings.Repeat("x", 10000) + "\n"},
				{"filename": "internal/small.go", "additions": 1, "deletions": 0, "patch": "@@ -0,0 +1 @@\n+abcd\n"},
			})
		case "/repos/acme/widgets/contents/.gitattributes?ref=main":
			writeJSON(w, repositoryContentResponse("generated/* linguist-generated=true\n"))
		case "/repos/acme/widgets/labels/size%2FXS":
			w.WriteHeader(http.StatusOK)
		case "/repos/acme/widgets/issues/7/labels":
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	handler := newTestHandler(server, false)
	var logs bytes.Buffer
	handler.logger = log.New(&logs, "", 0)

	resp := serveWebhook(handler, "pull_request", pullRequestPayload("opened", "main", "main", false, 7, nil))

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
		switch r.URL.RequestURI() {
		case "/repos/acme/widgets/contents/.github/labels.yml?ref=main":
			writeJSON(w, repositoryContentResponse(labelsConfigYAML(false, "", "L:\n  symbols: 250\n")))
		case "/repos/acme/widgets/pulls/7/files?per_page=100&page=1":
			writeJSON(w, []map[string]any{{"filename": "internal/symbol_custom.go", "additions": 1, "deletions": 0, "patch": "@@ -0,0 +1 @@\n+" + strings.Repeat("x", 250) + "\n"}})
		case "/repos/acme/widgets/contents/.gitattributes?ref=main":
			w.WriteHeader(http.StatusNotFound)
		case "/repos/acme/widgets/labels/size%2FL":
			w.WriteHeader(http.StatusOK)
		case "/repos/acme/widgets/issues/7/labels":
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	handler := newTestHandler(server, false)
	resp := serveWebhook(handler, "pull_request", pullRequestPayload("opened", "main", "main", false, 7, nil))

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
		switch r.URL.RequestURI() {
		case "/repos/acme/widgets/contents/.github/labels.yml?ref=main":
			writeJSON(w, repositoryContentResponse(labelsConfigYAML(false, "", "")))
		case "/repos/acme/widgets/pulls/9/files?per_page=100&page=1":
			writeJSON(w, []map[string]any{{"filename": "internal/service.go", "additions": 1, "deletions": 0, "patch": "@@ -0,0 +1 @@\n+ok\n"}})
		case "/repos/acme/widgets/contents/.gitattributes?ref=main":
			w.WriteHeader(http.StatusNotFound)
		case "/repos/acme/widgets/labels/size%2FXS":
			w.WriteHeader(http.StatusOK)
		case "/repos/acme/widgets/issues/9/labels":
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	handler := newTestHandler(server, false)
	var logs bytes.Buffer
	handler.logger = log.New(&logs, "", 0)

	payload := pullRequestPayload("opened", "main", "main", false, 1, nil)
	payload["number"] = 9
	resp := serveWebhook(handler, "pull_request", payload)

	if resp.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	assertContainsRequest(t, recorded, http.MethodPost, "/repos/acme/widgets/issues/9/labels", `{"labels":["size/XS"]}`)
	assertLogContains(t, logs.String(), `repo=acme/widgets pr_number=9`)
	assertLogNotContains(t, logs.String(), `pr_number=1`)
}

func TestPullRequestOpenedFailsWhenSelectedRepositoryLabelDoesNotExist(t *testing.T) {
	recorded := []requestRecord{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		recorded = append(recorded, requestRecord{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body)})
		switch r.URL.RequestURI() {
		case "/repos/acme/widgets/contents/.github/labels.yml?ref=main":
			writeJSON(w, repositoryContentResponse(labelsConfigYAML(false, "", "")))
		case "/repos/acme/widgets/pulls/7/files?per_page=100&page=1":
			writeJSON(w, []map[string]any{{"filename": "internal/service.go", "additions": 1, "deletions": 0, "patch": "@@ -0,0 +1 @@\n+ok\n"}})
		case "/repos/acme/widgets/contents/.gitattributes?ref=main":
			w.WriteHeader(http.StatusNotFound)
		case "/repos/acme/widgets/labels/size%2FXS":
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	handler := newTestHandler(server, false)
	resp := serveWebhook(handler, "pull_request", pullRequestPayload("opened", "main", "main", false, 7, []string{"size/M"}))

	if resp.Code != http.StatusBadGateway {
		t.Fatalf("ServeHTTP status = %d, want 502; body=%s", resp.Code, resp.Body.String())
	}
	assertNoRequestPath(t, recorded, "/repos/acme/widgets/issues/7/labels/size%2FM")
	assertNoRequestPath(t, recorded, "/repos/acme/widgets/issues/7/labels")
}

func TestInstallationCreatedBackfillsRecentOpenPullRequestsOnlyWhenEnabledInConfig(t *testing.T) {
	now := time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC)
	recorded := []requestRecord{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		recorded = append(recorded, requestRecord{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body)})
		switch r.URL.RequestURI() {
		case "/repos/acme/widgets":
			writeJSON(w, map[string]any{"default_branch": "main"})
		case "/repos/acme/widgets/contents/.github/labels.yml?ref=main":
			writeJSON(w, repositoryContentResponse(labelsConfigYAML(true, "720h", "")))
		case "/repos/acme/widgets/pulls?state=open&sort=created&direction=desc&per_page=100&page=1":
			writeJSON(w, []map[string]any{
				{"number": 7, "state": "open", "created_at": now.Add(-10 * 24 * time.Hour).Format(time.RFC3339), "labels": []map[string]any{{"name": "size/M"}}, "base": map[string]any{"ref": "main"}},
				{"number": 6, "state": "open", "created_at": now.Add(-40 * 24 * time.Hour).Format(time.RFC3339), "labels": []map[string]any{}, "base": map[string]any{"ref": "main"}},
			})
		case "/repos/acme/widgets/pulls/7/files?per_page=100&page=1":
			writeJSON(w, []map[string]any{{"filename": "internal/service.go", "additions": 1, "deletions": 0, "patch": "@@ -0,0 +1 @@\n+ok\n"}})
		case "/repos/acme/widgets/contents/.gitattributes?ref=main":
			w.WriteHeader(http.StatusNotFound)
		case "/repos/acme/widgets/labels/size%2FXS":
			w.WriteHeader(http.StatusOK)
		case "/repos/acme/widgets/issues/7/labels/size%2FM":
			if r.Method != http.MethodDelete {
				t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
			}
			w.WriteHeader(http.StatusOK)
		case "/repos/acme/widgets/issues/7/labels":
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	handler := newTestHandler(server, false)
	handler.now = func() time.Time { return now }

	payload := map[string]any{
		"action":       "created",
		"installation": map[string]any{"id": 42},
		"repositories": []map[string]any{{"full_name": "acme/widgets", "name": "widgets"}},
	}
	resp := serveWebhook(handler, "installation", payload)

	if resp.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	assertContainsRequest(t, recorded, http.MethodPost, "/repos/acme/widgets/issues/7/labels", `{"labels":["size/XS"]}`)
	assertNoRequestPath(t, recorded, "/repos/acme/widgets/pulls/6/files?per_page=100&page=1")
}

func TestInstallationRepositoriesAddedBackfillsWhenEnabledInConfig(t *testing.T) {
	recorded := []requestRecord{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		recorded = append(recorded, requestRecord{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body)})
		switch r.URL.RequestURI() {
		case "/repos/acme/widgets":
			writeJSON(w, map[string]any{"default_branch": "main"})
		case "/repos/acme/widgets/contents/.github/labels.yml?ref=main":
			writeJSON(w, repositoryContentResponse(labelsConfigYAML(true, "168h", "")))
		case "/repos/acme/widgets/pulls?state=open&sort=created&direction=desc&per_page=100&page=1":
			writeJSON(w, []map[string]any{{"number": 7, "state": "open", "created_at": "2026-03-16T10:00:00Z", "labels": []map[string]any{}, "base": map[string]any{"ref": "main"}}})
		case "/repos/acme/widgets/pulls/7/files?per_page=100&page=1":
			writeJSON(w, []map[string]any{{"filename": "internal/service.go", "additions": 1, "deletions": 0, "patch": "@@ -0,0 +1 @@\n+ok\n"}})
		case "/repos/acme/widgets/contents/.gitattributes?ref=main":
			w.WriteHeader(http.StatusNotFound)
		case "/repos/acme/widgets/labels/size%2FXS":
			w.WriteHeader(http.StatusOK)
		case "/repos/acme/widgets/issues/7/labels":
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	handler := newTestHandler(server, false)
	resp := serveWebhook(handler, "installation_repositories", map[string]any{
		"action":             "added",
		"installation":       map[string]any{"id": 42},
		"repositories_added": []map[string]any{{"full_name": "acme/widgets", "name": "widgets"}},
	})

	if resp.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	assertContainsRequest(t, recorded, http.MethodPost, "/repos/acme/widgets/issues/7/labels", `{"labels":["size/XS"]}`)
}

func TestInstallationCreatedDoesNothingWhenBackfillDisabledInConfig(t *testing.T) {
	recorded := []requestRecord{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		recorded = append(recorded, requestRecord{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body)})
		switch r.URL.RequestURI() {
		case "/repos/acme/widgets":
			writeJSON(w, map[string]any{"default_branch": "main"})
		case "/repos/acme/widgets/contents/.github/labels.yml?ref=main":
			writeJSON(w, repositoryContentResponse(labelsConfigYAML(false, "", "")))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	handler := newTestHandler(server, false)
	resp := serveWebhook(handler, "installation", map[string]any{
		"action":       "created",
		"installation": map[string]any{"id": 42},
		"repositories": []map[string]any{{"full_name": "acme/widgets", "name": "widgets"}},
	})

	if resp.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	assertNoRequestPath(t, recorded, "/repos/acme/widgets/pulls?state=open&sort=created&direction=desc&per_page=100&page=1")
	assertNoMutationRequests(t, recorded)
}

func TestInstallationCreatedDoesNothingWhenLabelsConfigMissing(t *testing.T) {
	recorded := []requestRecord{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		recorded = append(recorded, requestRecord{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body)})
		switch r.URL.RequestURI() {
		case "/repos/acme/widgets":
			writeJSON(w, map[string]any{"default_branch": "main"})
		case "/repos/acme/widgets/contents/.github/labels.yml?ref=main":
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	handler := newTestHandler(server, false)
	resp := serveWebhook(handler, "installation", map[string]any{
		"action":       "created",
		"installation": map[string]any{"id": 42},
		"repositories": []map[string]any{{"full_name": "acme/widgets", "name": "widgets"}},
	})

	if resp.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	assertNoRequestPath(t, recorded, "/repos/acme/widgets/pulls?state=open&sort=created&direction=desc&per_page=100&page=1")
	assertNoMutationRequests(t, recorded)
}

func TestMergedDefaultBranchLabelsYMLChangeRelabelsInWindowOpenPullRequests(t *testing.T) {
	now := time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC)
	recorded := []requestRecord{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		recorded = append(recorded, requestRecord{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body)})
		switch r.URL.RequestURI() {
		case "/repos/acme/widgets/pulls/12/files?per_page=100&page=1":
			writeJSON(w, []map[string]any{{"filename": ".github/labels.yml", "additions": 4, "deletions": 1, "patch": "@@ -1 +1 @@\n-old\n+new\n"}})
		case "/repos/acme/widgets/contents/.github/labels.yml?ref=main":
			writeJSON(w, repositoryContentResponse(labelsConfigYAML(true, "720h", "")))
		case "/repos/acme/widgets/pulls?state=open&sort=created&direction=desc&per_page=100&page=1":
			writeJSON(w, []map[string]any{
				{"number": 7, "state": "open", "created_at": now.Add(-5 * 24 * time.Hour).Format(time.RFC3339), "labels": []map[string]any{}, "base": map[string]any{"ref": "main"}},
				{"number": 6, "state": "open", "created_at": now.Add(-50 * 24 * time.Hour).Format(time.RFC3339), "labels": []map[string]any{}, "base": map[string]any{"ref": "main"}},
			})
		case "/repos/acme/widgets/pulls/7/files?per_page=100&page=1":
			writeJSON(w, []map[string]any{{"filename": "internal/service.go", "additions": 1, "deletions": 0, "patch": "@@ -0,0 +1 @@\n+ok\n"}})
		case "/repos/acme/widgets/contents/.gitattributes?ref=main":
			w.WriteHeader(http.StatusNotFound)
		case "/repos/acme/widgets/labels/size%2FXS":
			w.WriteHeader(http.StatusOK)
		case "/repos/acme/widgets/issues/7/labels":
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	handler := newTestHandler(server, false)
	handler.now = func() time.Time { return now }

	resp := serveWebhook(handler, "pull_request", pullRequestPayload("closed", "main", "main", true, 12, nil))

	if resp.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	assertContainsRequest(t, recorded, http.MethodPost, "/repos/acme/widgets/issues/7/labels", `{"labels":["size/XS"]}`)
	assertNoRequestPath(t, recorded, "/repos/acme/widgets/pulls/6/files?per_page=100&page=1")
}

func TestMergedLabelsYMLChangeDoesNothingWhenPullRequestNotMerged(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	handler := newTestHandler(server, false)
	resp := serveWebhook(handler, "pull_request", pullRequestPayload("closed", "main", "main", false, 12, nil))

	if resp.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	if called {
		t.Fatal("unexpected GitHub API call for unmerged closed pull request")
	}
}

func TestMergedLabelsYMLChangeDoesNothingOutsideDefaultBranch(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	handler := newTestHandler(server, false)
	resp := serveWebhook(handler, "pull_request", pullRequestPayload("closed", "main", "release", true, 12, nil))

	if resp.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	if called {
		t.Fatal("unexpected GitHub API call for non-default-branch merge")
	}
}

func TestMergedLabelsYMLChangeDoesNothingWhenPRDidNotChangeLabelsConfig(t *testing.T) {
	recorded := []requestRecord{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		recorded = append(recorded, requestRecord{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body)})
		if r.URL.RequestURI() != "/repos/acme/widgets/pulls/12/files?per_page=100&page=1" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		writeJSON(w, []map[string]any{{"filename": "README.md", "additions": 1, "deletions": 0, "patch": "@@ -1 +1 @@\n-old\n+new\n"}})
	}))
	defer server.Close()

	handler := newTestHandler(server, false)
	resp := serveWebhook(handler, "pull_request", pullRequestPayload("closed", "main", "main", true, 12, nil))

	if resp.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	assertNoRequestPath(t, recorded, "/repos/acme/widgets/pulls?state=open&sort=created&direction=desc&per_page=100&page=1")
}

func TestMergedLabelsYMLChangeDoesNothingWhenBackfillDisabled(t *testing.T) {
	recorded := []requestRecord{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		recorded = append(recorded, requestRecord{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body)})
		switch r.URL.RequestURI() {
		case "/repos/acme/widgets/pulls/12/files?per_page=100&page=1":
			writeJSON(w, []map[string]any{{"filename": ".github/labels.yml", "additions": 4, "deletions": 1, "patch": "@@ -1 +1 @@\n-old\n+new\n"}})
		case "/repos/acme/widgets/contents/.github/labels.yml?ref=main":
			writeJSON(w, repositoryContentResponse(labelsConfigYAML(false, "", "")))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	handler := newTestHandler(server, false)
	resp := serveWebhook(handler, "pull_request", pullRequestPayload("closed", "main", "main", true, 12, nil))

	if resp.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	assertNoRequestPath(t, recorded, "/repos/acme/widgets/pulls?state=open&sort=created&direction=desc&per_page=100&page=1")
	assertNoMutationRequests(t, recorded)
}

func TestMergedLabelsYMLChangeDoesNothingWhenLabelsConfigMissing(t *testing.T) {
	recorded := []requestRecord{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		recorded = append(recorded, requestRecord{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body)})
		switch r.URL.RequestURI() {
		case "/repos/acme/widgets/pulls/12/files?per_page=100&page=1":
			writeJSON(w, []map[string]any{{"filename": ".github/labels.yml", "additions": 4, "deletions": 1, "patch": "@@ -1 +1 @@\n-old\n+new\n"}})
		case "/repos/acme/widgets/contents/.github/labels.yml?ref=main":
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	handler := newTestHandler(server, false)
	resp := serveWebhook(handler, "pull_request", pullRequestPayload("closed", "main", "main", true, 12, nil))

	if resp.Code != http.StatusOK {
		t.Fatalf("ServeHTTP status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	assertNoRequestPath(t, recorded, "/repos/acme/widgets/pulls?state=open&sort=created&direction=desc&per_page=100&page=1")
	assertNoMutationRequests(t, recorded)
}

func TestPrivateRequestLoggingCanBeEnabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	handler := newTestHandler(server, true)
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

	handler := newTestHandler(server, false)
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

	handler := newTestHandler(server, false)
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

func newTestHandler(server *httptest.Server, logPrivate bool) *Handler {
	return NewHandler(
		"secret",
		auth.StaticTokenProvider("test-token"),
		func(token string) *githubapi.Client {
			return githubapi.NewClient(server.URL+"/", token, server.Client())
		},
		logPrivate,
	)
}

func serveWebhook(handler *Handler, eventName string, payload any) *httptest.ResponseRecorder {
	body, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", eventName)
	req.Header.Set("X-Hub-Signature-256", signPayload("secret", body))
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

func pullRequestPayload(action, defaultBranch, baseRef string, merged bool, number int, labelNames []string) map[string]any {
	labels := make([]map[string]any, 0, len(labelNames))
	for _, name := range labelNames {
		labels = append(labels, map[string]any{"name": name})
	}
	return map[string]any{
		"action":       action,
		"installation": map[string]any{"id": 42},
		"repository":   map[string]any{"name": "widgets", "default_branch": defaultBranch, "owner": map[string]any{"login": "acme"}},
		"pull_request": map[string]any{
			"number": number,
			"merged": merged,
			"labels": labels,
			"base":   map[string]any{"ref": baseRef},
		},
	}
}

func labelsConfigYAML(enabled bool, lookback, labelOverrides string) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "backfill:\n  enabled: %t\n", enabled)
	if strings.TrimSpace(lookback) != "" {
		fmt.Fprintf(&builder, "  lookback: %s\n", lookback)
	}
	if strings.TrimSpace(labelOverrides) == "" {
		builder.WriteString("labels: {}\n")
		return builder.String()
	}
	builder.WriteString("labels:\n")
	builder.WriteString(indentBlock(labelOverrides, "  "))
	if !strings.HasSuffix(labelOverrides, "\n") {
		builder.WriteString("\n")
	}
	return builder.String()
}

func indentBlock(value, prefix string) string {
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func repositoryContentResponse(content string) map[string]any {
	return map[string]any{
		"encoding": "base64",
		"content":  base64.StdEncoding.EncodeToString([]byte(content)),
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

func assertNoRequestPath(t *testing.T, recorded []requestRecord, requestPath string) {
	t.Helper()
	for _, record := range recorded {
		if record.Path == requestPath {
			t.Fatalf("unexpected request %s %s", record.Method, requestPath)
		}
	}
}

func assertNoMutationRequests(t *testing.T, recorded []requestRecord) {
	t.Helper()
	for _, record := range recorded {
		if record.Method == http.MethodPost || record.Method == http.MethodDelete || record.Method == http.MethodPatch || record.Method == http.MethodPut {
			t.Fatalf("unexpected mutation request %s %s", record.Method, record.Path)
		}
	}
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
