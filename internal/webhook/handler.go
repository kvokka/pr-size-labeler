package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/netip"
	"strings"

	"pr-size/internal/auth"
	"pr-size/internal/config"
	"pr-size/internal/generated"
	"pr-size/internal/githubapi"
	"pr-size/internal/labels"
)

type Handler struct {
	webhookSecret string
	tokenProvider auth.TokenProvider
	newClient     func(token string) *githubapi.Client
	logger        *log.Logger
}

type pullRequestEvent struct {
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	PullRequest struct {
		Number    int `json:"number"`
		Additions int `json:"additions"`
		Deletions int `json:"deletions"`
		Labels    []struct {
			Name string `json:"name"`
		} `json:"labels"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	} `json:"pull_request"`
}

func NewHandler(webhookSecret string, tokenProvider auth.TokenProvider, newClient func(token string) *githubapi.Client) *Handler {
	return &Handler{webhookSecret: webhookSecret, tokenProvider: tokenProvider, newClient: newClient, logger: log.Default()}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.logRequest(r)
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	if !h.validSignature(body, r.Header.Get("X-Hub-Signature-256")) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	if r.Header.Get("X-GitHub-Event") != "pull_request" {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	var event pullRequestEvent
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if !allowedAction(event.Action) {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	if err := h.handlePullRequest(r.Context(), event); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func allowedAction(action string) bool {
	switch action {
	case "opened", "reopened", "synchronize", "edited":
		return true
	default:
		return false
	}
}

func (h *Handler) handlePullRequest(ctx context.Context, event pullRequestEvent) error {
	token, err := h.tokenProvider.Token(ctx, event.Installation.ID)
	if err != nil {
		return fmt.Errorf("create installation token: %w", err)
	}
	client := h.newClient(token)
	owner := event.Repository.Owner.Login
	repo := event.Repository.Name
	ref := event.PullRequest.Base.Ref

	files, err := client.ListPullRequestFiles(ctx, owner, repo, event.PullRequest.Number)
	if err != nil {
		return err
	}

	gitattributesContent, err := client.GetRepositoryContent(ctx, owner, repo, ".gitattributes", ref)
	if err != nil && err != githubapi.ErrNotFound {
		return err
	}
	patterns := generated.ParseGitattributes(gitattributesContent)

	labelsContent, err := client.GetRepositoryContent(ctx, owner, repo, ".github/labels.yml", ref)
	if err != nil && err != githubapi.ErrNotFound {
		return err
	}
	labelSet, err := config.LoadLabelSet(labelsContent)
	if err != nil {
		return err
	}

	effectiveTotal := event.PullRequest.Additions + event.PullRequest.Deletions
	for _, file := range files {
		if generated.Match(file.Filename, patterns) {
			effectiveTotal -= file.Additions + file.Deletions
		}
	}
	if effectiveTotal < 0 {
		effectiveTotal = 0
	}

	selected := labelSet.Select(effectiveTotal)
	if err := h.removeExistingLabels(ctx, client, owner, repo, event, labelSet, selected.Name); err != nil {
		return err
	}
	if err := h.ensureLabelExists(ctx, client, owner, repo, selected); err != nil {
		return err
	}
	if err := client.AddIssueLabels(ctx, owner, repo, event.PullRequest.Number, []string{selected.Name}); err != nil {
		return err
	}
	if strings.TrimSpace(selected.Comment) != "" {
		if err := h.ensureComment(ctx, client, owner, repo, event.PullRequest.Number, selected.Comment); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) removeExistingLabels(ctx context.Context, client *githubapi.Client, owner, repo string, event pullRequestEvent, labelSet labels.Set, keep string) error {
	knownLabels := labelSet.Names()
	for _, existing := range event.PullRequest.Labels {
		if _, ok := knownLabels[existing.Name]; !ok {
			continue
		}
		if existing.Name == keep {
			continue
		}
		if err := client.RemoveIssueLabel(ctx, owner, repo, event.PullRequest.Number, existing.Name); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) ensureLabelExists(ctx context.Context, client *githubapi.Client, owner, repo string, selected labels.Definition) error {
	resp, err := client.GetLabel(ctx, owner, repo, selected.Name)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return client.CreateLabel(ctx, owner, repo, selected.Name, selected.Color)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("get label: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (h *Handler) ensureComment(ctx context.Context, client *githubapi.Client, owner, repo string, number int, body string) error {
	comments, err := client.ListIssueComments(ctx, owner, repo, number)
	if err != nil {
		return err
	}
	for _, comment := range comments {
		if comment.Body == body {
			return nil
		}
	}
	return client.CreateIssueComment(ctx, owner, repo, number, body)
}

func (h *Handler) validSignature(body []byte, signature string) bool {
	if signature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(h.webhookSecret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func (h *Handler) logRequest(r *http.Request) {
	if h.logger == nil {
		return
	}
	h.logger.Printf(
		"incoming request method=%s path=%s event=%q source=%q remote_addr=%q forwarded_for=%q real_ip=%q user_agent=%q",
		r.Method,
		r.URL.Path,
		r.Header.Get("X-GitHub-Event"),
		requestSource(r),
		r.RemoteAddr,
		r.Header.Get("X-Forwarded-For"),
		r.Header.Get("X-Real-IP"),
		r.UserAgent(),
	)
}

func requestSource(r *http.Request) string {
	for _, candidate := range []string{firstForwardedFor(r.Header.Get("X-Forwarded-For")), strings.TrimSpace(r.Header.Get("X-Real-IP")), remoteIP(r.RemoteAddr)} {
		if candidate != "" {
			return candidate
		}
	}
	return "unknown"
}

func firstForwardedFor(value string) string {
	if value == "" {
		return ""
	}
	parts := strings.Split(value, ",")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

func remoteIP(remoteAddr string) string {
	if remoteAddr == "" {
		return ""
	}
	addr, err := netip.ParseAddrPort(remoteAddr)
	if err == nil {
		return addr.Addr().String()
	}
	return strings.TrimSpace(remoteAddr)
}
