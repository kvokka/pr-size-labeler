package githubapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type Client struct {
	baseURL    *url.URL
	token      string
	httpClient *http.Client
}

type PullRequestFile struct {
	Filename  string `json:"filename"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch"`
}

type PullRequest struct {
	Number    int    `json:"number"`
	State     string `json:"state"`
	CreatedAt string `json:"created_at"`
	Labels    []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

type Repository struct {
	DefaultBranch string `json:"default_branch"`
}

type IssueComment struct {
	Body string `json:"body"`
}

func NewClient(baseURL, token string, httpClient *http.Client) *Client {
	parsedURL, _ := url.Parse(baseURL)
	if !strings.HasSuffix(parsedURL.Path, "/") {
		parsedURL.Path += "/"
	}
	return &Client{baseURL: parsedURL, token: token, httpClient: httpClient}
}

func (c *Client) ListPullRequestFiles(ctx context.Context, owner, repo string, number int) ([]PullRequestFile, error) {
	allFiles := []PullRequestFile{}
	for page := 1; ; page++ {
		var files []PullRequestFile
		endpoint := fmt.Sprintf("repos/%s/%s/pulls/%d/files?per_page=100&page=%d", owner, repo, number, page)
		resp, err := c.do(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			return nil, ErrNotFound
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return nil, fmt.Errorf("GET %s: unexpected status %d", endpoint, resp.StatusCode)
		}
		if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
			resp.Body.Close()
			return nil, err
		}
		hasNext := hasNextPage(resp.Header.Values("Link"), page)
		resp.Body.Close()
		allFiles = append(allFiles, files...)
		if !hasNext {
			break
		}
	}
	return allFiles, nil
}

func (c *Client) ListOpenPullRequests(ctx context.Context, owner, repo string) ([]PullRequest, error) {
	allPullRequests := []PullRequest{}
	for page := 1; ; page++ {
		var pullRequests []PullRequest
		endpoint := fmt.Sprintf("repos/%s/%s/pulls?state=open&sort=created&direction=desc&per_page=100&page=%d", owner, repo, page)
		resp, err := c.do(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			return nil, ErrNotFound
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return nil, fmt.Errorf("GET %s: unexpected status %d", endpoint, resp.StatusCode)
		}
		if err := json.NewDecoder(resp.Body).Decode(&pullRequests); err != nil {
			resp.Body.Close()
			return nil, err
		}
		hasNext := hasNextPage(resp.Header.Values("Link"), page)
		resp.Body.Close()
		allPullRequests = append(allPullRequests, pullRequests...)
		if !hasNext {
			break
		}
	}
	return allPullRequests, nil
}

func (c *Client) GetRepositoryContent(ctx context.Context, owner, repo, filePath, ref string) (string, error) {
	endpoint := fmt.Sprintf("repos/%s/%s/contents/%s", owner, repo, filePath)
	if ref != "" {
		endpoint += "?ref=" + url.QueryEscape(ref)
	}
	var content struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := c.getJSON(ctx, endpoint, &content); err != nil {
		return "", err
	}
	if content.Encoding != "base64" {
		return "", fmt.Errorf("unsupported encoding %q", content.Encoding)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(content.Content, "\n", ""))
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func (c *Client) GetLabel(ctx context.Context, owner, repo, name string) (*http.Response, error) {
	return c.do(ctx, http.MethodGet, fmt.Sprintf("repos/%s/%s/labels/%s", owner, repo, url.PathEscape(name)), nil)
}

func (c *Client) GetRepository(ctx context.Context, owner, repo string) (Repository, error) {
	var repository Repository
	if err := c.getJSON(ctx, fmt.Sprintf("repos/%s/%s", owner, repo), &repository); err != nil {
		return Repository{}, err
	}
	return repository, nil
}

func (c *Client) AddIssueLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	body := map[string][]string{"labels": labels}
	resp, err := c.do(ctx, http.MethodPost, fmt.Sprintf("repos/%s/%s/issues/%d/labels", owner, repo, number), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("add labels: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) RemoveIssueLabel(ctx context.Context, owner, repo string, number int, name string) error {
	resp, err := c.do(ctx, http.MethodDelete, fmt.Sprintf("repos/%s/%s/issues/%d/labels/%s", owner, repo, number, url.PathEscape(name)), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("remove label: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) ListIssueComments(ctx context.Context, owner, repo string, number int) ([]IssueComment, error) {
	allComments := []IssueComment{}
	for page := 1; ; page++ {
		var comments []IssueComment
		endpoint := fmt.Sprintf("repos/%s/%s/issues/%d/comments?per_page=100&page=%d", owner, repo, number, page)
		resp, err := c.do(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			return nil, ErrNotFound
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return nil, fmt.Errorf("GET %s: unexpected status %d", endpoint, resp.StatusCode)
		}
		if err := json.NewDecoder(resp.Body).Decode(&comments); err != nil {
			resp.Body.Close()
			return nil, err
		}
		hasNext := hasNextPage(resp.Header.Values("Link"), page)
		resp.Body.Close()
		allComments = append(allComments, comments...)
		if !hasNext {
			break
		}
	}
	return allComments, nil
}

func hasNextPage(linkHeaders []string, currentPage int) bool {
	for _, header := range linkHeaders {
		for _, part := range strings.Split(header, ",") {
			part = strings.TrimSpace(part)
			if !strings.Contains(part, `rel="next"`) {
				continue
			}
			start := strings.Index(part, "<")
			end := strings.Index(part, ">")
			if start == -1 || end == -1 || end <= start+1 {
				return true
			}
			nextURL, err := url.Parse(part[start+1 : end])
			if err != nil {
				return true
			}
			nextPage, err := strconv.Atoi(nextURL.Query().Get("page"))
			if err != nil {
				return true
			}
			return nextPage > currentPage
		}
	}
	return false
}

func (c *Client) CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) error {
	resp, err := c.do(ctx, http.MethodPost, fmt.Sprintf("repos/%s/%s/issues/%d/comments", owner, repo, number), map[string]string{"body": body})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("create comment: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) getJSON(ctx context.Context, endpoint string, dest any) error {
	resp, err := c.do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GET %s: unexpected status %d", endpoint, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}

var ErrNotFound = fmt.Errorf("not found")

func (c *Client) do(ctx context.Context, method, endpoint string, body any) (*http.Response, error) {
	requestURL := endpoint
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		requestURL = strings.TrimRight(c.baseURL.String(), "/") + "/" + strings.TrimLeft(endpoint, "/")
	}
	resolved, err := url.Parse(requestURL)
	if err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, resolved.String(), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

func nextPageURL(linkHeaders []string) string {
	for _, header := range linkHeaders {
		for _, part := range strings.Split(header, ",") {
			part = strings.TrimSpace(part)
			if !strings.Contains(part, `rel="next"`) {
				continue
			}
			start := strings.Index(part, "<")
			end := strings.Index(part, ">")
			if start == -1 || end == -1 || end <= start+1 {
				return ""
			}
			return part[start+1 : end]
		}
	}
	return ""
}
