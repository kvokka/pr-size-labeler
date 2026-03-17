package githubapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListPullRequestFilesPaginates(t *testing.T) {
	var baseURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.RequestURI() {
		case "/repos/acme/widgets/pulls/7/files?per_page=100&page=1":
			w.Header().Add("Link", `<`+baseURL+`/repos/acme/widgets/pulls/7/files?per_page=100&page=2>; rel="next"`)
			writeJSON(w, []PullRequestFile{{Filename: "page-one.go", Additions: 1, Deletions: 1, Patch: "@@ -1 +1 @@\n-old\n+new\n"}})
		case "/repos/acme/widgets/pulls/7/files?per_page=100&page=2":
			writeJSON(w, []PullRequestFile{{Filename: "page-two.go", Additions: 2, Deletions: 2, Patch: "@@ -1 +1 @@\n-a\n+ab\n"}})
		default:
			t.Fatalf("unexpected request %s", r.URL.RequestURI())
		}
	}))
	defer server.Close()
	baseURL = server.URL

	client := NewClient(server.URL+"/", "test-token", server.Client())
	files, err := client.ListPullRequestFiles(context.Background(), "acme", "widgets", 7)
	if err != nil {
		t.Fatalf("ListPullRequestFiles returned error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[1].Filename != "page-two.go" {
		t.Fatalf("second filename = %q, want page-two.go", files[1].Filename)
	}
	if files[0].Patch == "" || files[1].Patch == "" {
		t.Fatalf("expected patch text to be populated for all files")
	}
}

func TestListOpenPullRequestsPaginates(t *testing.T) {
	var baseURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.RequestURI() {
		case "/repos/acme/widgets/pulls?state=open&sort=created&direction=desc&per_page=100&page=1":
			w.Header().Add("Link", `<`+baseURL+`/repos/acme/widgets/pulls?state=open&sort=created&direction=desc&per_page=100&page=2>; rel="next"`)
			writeJSON(w, []PullRequest{{Number: 9, State: "open", CreatedAt: "2026-03-16T10:00:00Z"}})
		case "/repos/acme/widgets/pulls?state=open&sort=created&direction=desc&per_page=100&page=2":
			writeJSON(w, []PullRequest{{Number: 8, State: "open", CreatedAt: "2026-03-15T10:00:00Z"}})
		default:
			t.Fatalf("unexpected request %s", r.URL.RequestURI())
		}
	}))
	defer server.Close()
	baseURL = server.URL

	client := NewClient(server.URL+"/", "test-token", server.Client())
	pullRequests, err := client.ListOpenPullRequests(context.Background(), "acme", "widgets")
	if err != nil {
		t.Fatalf("ListOpenPullRequests returned error: %v", err)
	}
	if len(pullRequests) != 2 {
		t.Fatalf("expected 2 pull requests, got %d", len(pullRequests))
	}
	if pullRequests[0].Number != 9 || pullRequests[1].Number != 8 {
		t.Fatalf("unexpected pull requests: %+v", pullRequests)
	}
}

func TestGetRepositoryReturnsDefaultBranch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/repos/acme/widgets" {
			t.Fatalf("unexpected request %s", r.URL.RequestURI())
		}
		writeJSON(w, map[string]any{"default_branch": "main"})
	}))
	defer server.Close()

	client := NewClient(server.URL+"/", "test-token", server.Client())
	repository, err := client.GetRepository(context.Background(), "acme", "widgets")
	if err != nil {
		t.Fatalf("GetRepository returned error: %v", err)
	}
	if repository.DefaultBranch != "main" {
		t.Fatalf("default branch = %q, want main", repository.DefaultBranch)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(err)
	}
}
