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

	"pr-size-labeler/internal/auth"
	"pr-size-labeler/internal/config"
	"pr-size-labeler/internal/generated"
	"pr-size-labeler/internal/githubapi"
	"pr-size-labeler/internal/labels"
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
	if r.Header.Get("X-GitHub-Event") == "ping" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","event":"ping"}`))
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
	h.logPullRequestStage(event, "start", "accepted pull_request event")
	h.logPullRequestStage(event, "token", "requesting installation token")
	token, err := h.tokenProvider.Token(ctx, event.Installation.ID)
	if err != nil {
		h.logPullRequestFailure(event, "token", err)
		return fmt.Errorf("create installation token: %w", err)
	}
	h.logPullRequestStage(event, "token", "installation token acquired")
	client := h.newClient(token)
	owner := event.Repository.Owner.Login
	repo := event.Repository.Name
	ref := event.PullRequest.Base.Ref

	h.logPullRequestStage(event, "files", "fetching pull request files")
	files, err := client.ListPullRequestFiles(ctx, owner, repo, event.PullRequest.Number)
	if err != nil {
		h.logPullRequestFailure(event, "files", err)
		return err
	}
	h.logPullRequestStage(event, "files", fmt.Sprintf("fetched %d pull request files", len(files)))

	h.logPullRequestStage(event, "gitattributes", "loading .gitattributes from base branch")
	gitattributesContent, err := client.GetRepositoryContent(ctx, owner, repo, ".gitattributes", ref)
	if err != nil && err != githubapi.ErrNotFound {
		h.logPullRequestFailure(event, "gitattributes", err)
		return err
	}
	patterns := generated.ParseGitattributes(gitattributesContent)
	h.logPullRequestStage(event, "gitattributes", fmt.Sprintf("loaded %d generated-file pattern(s)", len(patterns)))

	h.logPullRequestStage(event, "labels_config", "loading .github/labels.yml from base branch")
	labelsContent, err := client.GetRepositoryContent(ctx, owner, repo, ".github/labels.yml", ref)
	if err != nil && err != githubapi.ErrNotFound {
		h.logPullRequestFailure(event, "labels_config", err)
		return err
	}
	labelSet, err := config.LoadLabelSet(labelsContent)
	if err != nil {
		h.logPullRequestFailure(event, "labels_config", err)
		return err
	}
	h.logPullRequestStage(event, "labels_config", fmt.Sprintf("loaded %d label definition(s)", len(labelSet)))

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
	h.logPullRequestStage(event, "selection", fmt.Sprintf("effective_total=%d selected_label=%s", effectiveTotal, selected.Name))
	h.logPullRequestStage(event, "labels_cleanup", "removing previously configured size labels")
	if err := h.removeExistingLabels(ctx, client, owner, repo, event, labelSet, selected.Name); err != nil {
		h.logPullRequestFailure(event, "labels_cleanup", err)
		return err
	}
	h.logPullRequestStage(event, "label_ensure", fmt.Sprintf("ensuring label %s exists", selected.Name))
	if err := h.ensureLabelExists(ctx, client, owner, repo, selected); err != nil {
		h.logPullRequestFailure(event, "label_ensure", err)
		return err
	}
	h.logPullRequestStage(event, "label_apply", fmt.Sprintf("applying label %s", selected.Name))
	if err := client.AddIssueLabels(ctx, owner, repo, event.PullRequest.Number, []string{selected.Name}); err != nil {
		h.logPullRequestFailure(event, "label_apply", err)
		return err
	}
	if strings.TrimSpace(selected.Comment) != "" {
		h.logPullRequestStage(event, "comment", "ensuring configured comment")
		if err := h.ensureComment(ctx, client, owner, repo, event.PullRequest.Number, selected.Comment); err != nil {
			h.logPullRequestFailure(event, "comment", err)
			return err
		}
	}
	h.logPullRequestStage(event, "done", fmt.Sprintf("completed pull request processing with label %s", selected.Name))
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

func (h *Handler) logPullRequestStage(event pullRequestEvent, stage, message string) {
	if h.logger == nil {
		return
	}
	h.logger.Printf(
		"pull_request stage=%s action=%q installation_id=%d repo=%s/%s pr_number=%d %s",
		stage,
		event.Action,
		event.Installation.ID,
		event.Repository.Owner.Login,
		event.Repository.Name,
		event.PullRequest.Number,
		message,
	)
}

func (h *Handler) logPullRequestFailure(event pullRequestEvent, stage string, err error) {
	if h.logger == nil {
		return
	}
	h.logger.Printf(
		"pull_request stage=%s action=%q installation_id=%d repo=%s/%s pr_number=%d error=%v",
		stage,
		event.Action,
		event.Installation.ID,
		event.Repository.Owner.Login,
		event.Repository.Name,
		event.PullRequest.Number,
		err,
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
