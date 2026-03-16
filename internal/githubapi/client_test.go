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
			writeJSON(w, []PullRequestFile{{Filename: "page-one.go", Additions: 1, Deletions: 1}})
		case "/repos/acme/widgets/pulls/7/files?per_page=100&page=2":
			writeJSON(w, []PullRequestFile{{Filename: "page-two.go", Additions: 2, Deletions: 2}})
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
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(err)
	}
}
