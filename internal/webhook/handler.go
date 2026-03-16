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
	"time"
	"unicode/utf8"

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
	logPrivate    bool
	now           func() time.Time
	backfill      connectOpenPRsBackfillConfig
}

type connectOpenPRsBackfillConfig struct {
	enabled  bool
	lookback time.Duration
}

type pullRequestEvent struct {
	Number       int    `json:"number"`
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

type pullRequestLabel struct {
	Name string `json:"name"`
}

type pullRequestTarget struct {
	Action         string
	InstallationID int64
	Owner          string
	Repo           string
	Number         int
	BaseRef        string
	Labels         []pullRequestLabel
}

type installationRepository struct {
	FullName string `json:"full_name"`
	Name     string `json:"name"`
	Owner    struct {
		Login string `json:"login"`
	} `json:"owner"`
}

type installationEvent struct {
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Repositories      []installationRepository `json:"repositories"`
	RepositoriesAdded []installationRepository `json:"repositories_added"`
}

func NewHandler(webhookSecret string, tokenProvider auth.TokenProvider, newClient func(token string) *githubapi.Client, logPrivate bool, connectOpenPRsBackfillEnabled bool, connectOpenPRsBackfillLookback time.Duration) *Handler {
	return &Handler{
		webhookSecret: webhookSecret,
		tokenProvider: tokenProvider,
		newClient:     newClient,
		logger:        log.Default(),
		logPrivate:    logPrivate,
		now:           time.Now,
		backfill: connectOpenPRsBackfillConfig{
			enabled:  connectOpenPRsBackfillEnabled,
			lookback: connectOpenPRsBackfillLookback,
		},
	}
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
	eventName := r.Header.Get("X-GitHub-Event")
	if eventName == "ping" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","event":"ping"}`))
		return
	}

	switch eventName {
	case "pull_request":
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
		return
	case "installation", "installation_repositories":
		var event installationEvent
		if err := json.Unmarshal(body, &event); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		if !allowedInstallationAction(eventName, event.Action) {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if err := h.handleConnectEvent(r.Context(), eventName, event); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	default:
		w.WriteHeader(http.StatusAccepted)
		return
	}
}

func allowedAction(action string) bool {
	switch action {
	case "opened", "reopened", "synchronize", "edited":
		return true
	default:
		return false
	}
}

func allowedInstallationAction(eventName, action string) bool {
	switch eventName {
	case "installation":
		return action == "created"
	case "installation_repositories":
		return action == "added"
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
	target := pullRequestTarget{
		Action:         event.Action,
		InstallationID: event.Installation.ID,
		Owner:          event.Repository.Owner.Login,
		Repo:           event.Repository.Name,
		Number:         event.prNumber(),
		BaseRef:        event.PullRequest.Base.Ref,
	}
	for _, label := range event.PullRequest.Labels {
		target.Labels = append(target.Labels, pullRequestLabel{Name: label.Name})
	}
	return h.processPullRequest(ctx, client, target)
}

func (h *Handler) handleConnectEvent(ctx context.Context, eventName string, event installationEvent) error {
	if !h.backfill.enabled {
		h.logger.Printf("connect_open_prs_backfill enabled=false event=%q action=%q", eventName, event.Action)
		return nil
	}
	token, err := h.tokenProvider.Token(ctx, event.Installation.ID)
	if err != nil {
		return fmt.Errorf("create installation token: %w", err)
	}
	client := h.newClient(token)
	cutoff := h.now().UTC().Add(-h.backfill.lookback)
	repositories := event.repositoriesForAction()
	h.logger.Printf("connect_open_prs_backfill enabled=true event=%q action=%q repositories=%d lookback=%s cutoff=%s", eventName, event.Action, len(repositories), h.backfill.lookback, cutoff.Format(time.RFC3339))
	for _, repository := range repositories {
		owner, repo, err := repository.ownerAndRepo()
		if err != nil {
			return err
		}
		pullRequests, err := client.ListOpenPullRequests(ctx, owner, repo)
		if err != nil {
			return fmt.Errorf("list open pull requests for %s/%s: %w", owner, repo, err)
		}
		processed := 0
		for _, pullRequest := range pullRequests {
			createdAt, err := time.Parse(time.RFC3339, pullRequest.CreatedAt)
			if err != nil {
				return fmt.Errorf("parse pull request created_at for %s/%s#%d: %w", owner, repo, pullRequest.Number, err)
			}
			if createdAt.UTC().Before(cutoff) {
				break
			}
			target := pullRequestTarget{
				Action:         event.Action,
				InstallationID: event.Installation.ID,
				Owner:          owner,
				Repo:           repo,
				Number:         pullRequest.Number,
				BaseRef:        pullRequest.Base.Ref,
			}
			for _, label := range pullRequest.Labels {
				target.Labels = append(target.Labels, pullRequestLabel{Name: label.Name})
			}
			if err := h.processPullRequest(ctx, client, target); err != nil {
				return err
			}
			processed++
		}
		h.logger.Printf("connect_open_prs_backfill repository=%s/%s processed=%d listed=%d", owner, repo, processed, len(pullRequests))
	}
	return nil
}

func (h *Handler) processPullRequest(ctx context.Context, client *githubapi.Client, target pullRequestTarget) error {
	h.logPullRequestTargetStage(target, "files", "fetching pull request files")
	files, err := client.ListPullRequestFiles(ctx, target.Owner, target.Repo, target.Number)
	if err != nil {
		h.logPullRequestTargetFailure(target, "files", err)
		return err
	}
	h.logPullRequestTargetStage(target, "files", fmt.Sprintf("fetched %d pull request files", len(files)))

	h.logPullRequestTargetStage(target, "gitattributes", "loading .gitattributes from base branch")
	gitattributesContent, err := client.GetRepositoryContent(ctx, target.Owner, target.Repo, ".gitattributes", target.BaseRef)
	if err != nil && err != githubapi.ErrNotFound {
		h.logPullRequestTargetFailure(target, "gitattributes", err)
		return err
	}
	patterns := generated.ParseGitattributes(gitattributesContent)
	h.logPullRequestTargetStage(target, "gitattributes", fmt.Sprintf("loaded %d generated-file pattern(s)", len(patterns)))

	h.logPullRequestTargetStage(target, "labels_config", "loading .github/labels.yml from base branch")
	labelsContent, err := client.GetRepositoryContent(ctx, target.Owner, target.Repo, ".github/labels.yml", target.BaseRef)
	if err != nil && err != githubapi.ErrNotFound {
		h.logPullRequestTargetFailure(target, "labels_config", err)
		return err
	}
	labelSet, err := config.LoadLabelSet(labelsContent)
	if err != nil {
		h.logPullRequestTargetFailure(target, "labels_config", err)
		return err
	}
	h.logPullRequestTargetStage(target, "labels_config", fmt.Sprintf("loaded %d label definition(s)", len(labelSet)))

	effectiveLines, effectiveSymbols := effectiveTotals(files, patterns)
	selected := labelSet.Select(effectiveLines, effectiveSymbols)
	h.logPullRequestTargetStage(target, "selection", fmt.Sprintf("effective_lines=%d effective_symbols=%d selected_label=%s", effectiveLines, effectiveSymbols, selected.Name))
	h.logPullRequestTargetStage(target, "labels_cleanup", "removing previously configured size labels")
	if err := h.removeExistingLabels(ctx, client, target.Owner, target.Repo, target.Number, target.Labels, labelSet, selected.Name); err != nil {
		h.logPullRequestTargetFailure(target, "labels_cleanup", err)
		return err
	}
	h.logPullRequestTargetStage(target, "label_ensure", fmt.Sprintf("ensuring label %s exists", selected.Name))
	if err := h.ensureLabelExists(ctx, client, target.Owner, target.Repo, selected); err != nil {
		h.logPullRequestTargetFailure(target, "label_ensure", err)
		return err
	}
	h.logPullRequestTargetStage(target, "label_apply", fmt.Sprintf("applying label %s", selected.Name))
	if err := client.AddIssueLabels(ctx, target.Owner, target.Repo, target.Number, []string{selected.Name}); err != nil {
		h.logPullRequestTargetFailure(target, "label_apply", err)
		return err
	}
	if strings.TrimSpace(selected.Comment) != "" {
		h.logPullRequestTargetStage(target, "comment", "ensuring configured comment")
		if err := h.ensureComment(ctx, client, target.Owner, target.Repo, target.Number, selected.Comment); err != nil {
			h.logPullRequestTargetFailure(target, "comment", err)
			return err
		}
	}
	h.logPullRequestTargetStage(target, "done", fmt.Sprintf("completed pull request processing with label %s", selected.Name))
	return nil
}

func effectiveTotals(files []githubapi.PullRequestFile, patterns []string) (int, int) {
	effectiveLines := 0
	effectiveSymbols := 0
	for _, file := range files {
		if generated.Match(file.Filename, patterns) {
			continue
		}
		effectiveLines += file.Additions + file.Deletions
		effectiveSymbols += changedSymbolsFromPatch(file.Patch)
	}
	if effectiveLines < 0 {
		effectiveLines = 0
	}
	if effectiveSymbols < 0 {
		effectiveSymbols = 0
	}
	return effectiveLines, effectiveSymbols
}

func changedSymbolsFromPatch(patch string) int {
	total := 0
	for _, line := range strings.Split(patch, "\n") {
		if line == "" {
			continue
		}
		if line == "+++" || line == "---" || strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "--- ") {
			continue
		}
		if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
			total += utf8.RuneCountInString(line[1:])
		}
	}
	return total
}

func (e pullRequestEvent) prNumber() int {
	if e.Number > 0 {
		return e.Number
	}
	return e.PullRequest.Number
}

func (event installationEvent) repositoriesForAction() []installationRepository {
	if event.Action == "added" {
		return event.RepositoriesAdded
	}
	return event.Repositories
}

func (repository installationRepository) ownerAndRepo() (string, string, error) {
	owner := strings.TrimSpace(repository.Owner.Login)
	repo := strings.TrimSpace(repository.Name)
	if owner != "" && repo != "" {
		return owner, repo, nil
	}
	fullName := strings.TrimSpace(repository.FullName)
	if fullName != "" {
		fullNameOwner, fullNameRepo, ok := strings.Cut(fullName, "/")
		if ok {
			if owner == "" {
				owner = strings.TrimSpace(fullNameOwner)
			}
			if repo == "" {
				repo = strings.TrimSpace(fullNameRepo)
			}
		}
	}
	if owner != "" && repo != "" {
		return owner, repo, nil
	}
	if fullName != "" {
		return "", "", fmt.Errorf("resolve installation repository owner/repo from payload full_name=%q name=%q", fullName, repository.Name)
	}
	return "", "", fmt.Errorf("resolve installation repository owner/repo from payload name=%q", repository.Name)
}

func (h *Handler) removeExistingLabels(ctx context.Context, client *githubapi.Client, owner, repo string, number int, existingLabels []pullRequestLabel, labelSet labels.Set, keep string) error {
	knownLabels := labelSet.Names()
	for _, existing := range existingLabels {
		if _, ok := knownLabels[existing.Name]; !ok {
			continue
		}
		if existing.Name == keep {
			continue
		}
		if err := client.RemoveIssueLabel(ctx, owner, repo, number, existing.Name); err != nil {
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
	if !h.logPrivate {
		h.logger.Printf(
			"incoming request method=%s path=%s event=%q",
			r.Method,
			r.URL.Path,
			r.Header.Get("X-GitHub-Event"),
		)
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
	h.logger.Printf("pull_request stage=%s action=%q %s %s", stage, event.Action, h.pullRequestLogContext(event), message)
}

func (h *Handler) logPullRequestFailure(event pullRequestEvent, stage string, err error) {
	if h.logger == nil {
		return
	}
	h.logger.Printf("pull_request stage=%s action=%q %s error=%v", stage, event.Action, h.pullRequestLogContext(event), err)
}

func (h *Handler) logPullRequestTargetStage(target pullRequestTarget, stage, message string) {
	if h.logger == nil {
		return
	}
	h.logger.Printf("pull_request stage=%s action=%q %s %s", stage, target.Action, h.pullRequestTargetLogContext(target), message)
}

func (h *Handler) logPullRequestTargetFailure(target pullRequestTarget, stage string, err error) {
	if h.logger == nil {
		return
	}
	h.logger.Printf("pull_request stage=%s action=%q %s error=%v", stage, target.Action, h.pullRequestTargetLogContext(target), err)
}

func (h *Handler) pullRequestLogContext(event pullRequestEvent) string {
	context := fmt.Sprintf("repo=%s/%s pr_number=%d", event.Repository.Owner.Login, event.Repository.Name, event.prNumber())
	if h.logPrivate {
		return fmt.Sprintf("installation_id=%d %s", event.Installation.ID, context)
	}
	return context
}

func (h *Handler) pullRequestTargetLogContext(target pullRequestTarget) string {
	context := fmt.Sprintf("repo=%s/%s pr_number=%d", target.Owner, target.Repo, target.Number)
	if h.logPrivate {
		return fmt.Sprintf("installation_id=%d %s", target.InstallationID, context)
	}
	return context
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
