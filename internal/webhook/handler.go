package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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

var (
	errLabelsConfigMissing = errors.New("labels config missing")
	errLabelsConfigInvalid = errors.New("labels config invalid")
)

type Handler struct {
	webhookSecret string
	tokenProvider auth.TokenProvider
	newClient     func(token string) *githubapi.Client
	logger        *log.Logger
	logPrivate    bool
	now           func() time.Time
}

type pullRequestEvent struct {
	Number       int    `json:"number"`
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Repository struct {
		Name          string `json:"name"`
		DefaultBranch string `json:"default_branch"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	PullRequest struct {
		Number int  `json:"number"`
		Merged bool `json:"merged"`
		Labels []struct {
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

func NewHandler(webhookSecret string, tokenProvider auth.TokenProvider, newClient func(token string) *githubapi.Client, logPrivate bool) *Handler {
	return &Handler{
		webhookSecret: webhookSecret,
		tokenProvider: tokenProvider,
		newClient:     newClient,
		logger:        log.Default(),
		logPrivate:    logPrivate,
		now:           time.Now,
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
		if !allowedPullRequestAction(event.Action) {
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

func allowedPullRequestAction(action string) bool {
	switch action {
	case "opened", "reopened", "synchronize", "edited", "closed":
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

	if event.Action == "closed" {
		return h.handleMergedLabelsConfigChange(ctx, client, event)
	}

	target := event.target()
	labelsConfig, configRef, err := h.loadRepositoryLabelsConfig(ctx, client, target.Owner, target.Repo, event.defaultBranch())
	if err != nil {
		return h.handlePullRequestLabelsConfigError(target, configRef, err)
	}
	h.logPullRequestTargetStage(target, "labels_config", fmt.Sprintf("loaded %d label definition(s) from default branch %s", len(labelsConfig.Labels), configRef))
	return h.processPullRequest(ctx, client, target, labelsConfig.Labels)
}

func (h *Handler) handleMergedLabelsConfigChange(ctx context.Context, client *githubapi.Client, event pullRequestEvent) error {
	owner := event.Repository.Owner.Login
	repo := event.Repository.Name
	defaultBranch, err := h.resolveDefaultBranch(ctx, client, owner, repo, event.defaultBranch())
	if err != nil {
		h.logPullRequestFailure(event, "config_change", err)
		return err
	}
	if !event.PullRequest.Merged {
		h.logPullRequestStage(event, "config_change", "skipping merged-config relabel because pull request was not merged")
		return nil
	}
	if event.PullRequest.Base.Ref != defaultBranch {
		h.logPullRequestStage(event, "config_change", fmt.Sprintf("skipping merged-config relabel because base branch %s is not the default branch %s", event.PullRequest.Base.Ref, defaultBranch))
		return nil
	}

	h.logPullRequestStage(event, "config_change", "checking merged pull request for .github/labels.yml changes")
	files, err := client.ListPullRequestFiles(ctx, owner, repo, event.prNumber())
	if err != nil {
		h.logPullRequestFailure(event, "config_change", err)
		return err
	}
	if !pullRequestTouchesLabelsConfig(files) {
		h.logPullRequestStage(event, "config_change", "merged pull request did not change .github/labels.yml")
		return nil
	}

	labelsConfig, configRef, err := h.loadRepositoryLabelsConfig(ctx, client, owner, repo, defaultBranch)
	if err != nil {
		return h.handlePullRequestConfigChangeLabelsError(event, configRef, err)
	}
	if !labelsConfig.Backfill.Enabled {
		h.logPullRequestStage(event, "config_change", "backfill disabled in .github/labels.yml; skipping open pull request relabel")
		return nil
	}

	h.logPullRequestStage(event, "config_change", fmt.Sprintf("relabeling open pull requests from default branch %s lookback=%s", configRef, labelsConfig.Backfill.Lookback))
	return h.relabelOpenPullRequests(ctx, client, event.Installation.ID, owner, repo, event.Action, labelsConfig.Labels, labelsConfig.Backfill.Lookback)
}

func (h *Handler) handleConnectEvent(ctx context.Context, eventName string, event installationEvent) error {
	token, err := h.tokenProvider.Token(ctx, event.Installation.ID)
	if err != nil {
		return fmt.Errorf("create installation token: %w", err)
	}
	client := h.newClient(token)

	repositories := event.repositoriesForAction()
	h.logger.Printf("connect_relabel event=%q action=%q repositories=%d", eventName, event.Action, len(repositories))
	for _, repository := range repositories {
		owner, repo, err := repository.ownerAndRepo()
		if err != nil {
			return err
		}
		labelsConfig, configRef, err := h.loadRepositoryLabelsConfig(ctx, client, owner, repo, "")
		if err != nil {
			if h.logConnectLabelsConfigSkip(owner, repo, configRef, err) {
				continue
			}
			return err
		}
		if !labelsConfig.Backfill.Enabled {
			h.logger.Printf("connect_relabel repository=%s/%s default_branch=%s skipped backfill_enabled=false", owner, repo, configRef)
			continue
		}
		if err := h.relabelOpenPullRequests(ctx, client, event.Installation.ID, owner, repo, event.Action, labelsConfig.Labels, labelsConfig.Backfill.Lookback); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) relabelOpenPullRequests(ctx context.Context, client *githubapi.Client, installationID int64, owner, repo, action string, labelSet labels.Set, lookback time.Duration) error {
	cutoff := h.now().UTC().Add(-lookback)
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
			Action:         action,
			InstallationID: installationID,
			Owner:          owner,
			Repo:           repo,
			Number:         pullRequest.Number,
			BaseRef:        pullRequest.Base.Ref,
		}
		for _, label := range pullRequest.Labels {
			target.Labels = append(target.Labels, pullRequestLabel{Name: label.Name})
		}
		if err := h.processPullRequest(ctx, client, target, labelSet); err != nil {
			return err
		}
		processed++
	}
	h.logger.Printf("open_pr_relabel repository=%s/%s processed=%d listed=%d lookback=%s cutoff=%s", owner, repo, processed, len(pullRequests), lookback, cutoff.Format(time.RFC3339))
	return nil
}

func (h *Handler) processPullRequest(ctx context.Context, client *githubapi.Client, target pullRequestTarget, labelSet labels.Set) error {
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

	effectiveLines, effectiveSymbols := effectiveTotals(files, patterns)
	selected := labelSet.Select(effectiveLines, effectiveSymbols)
	h.logPullRequestTargetStage(target, "selection", fmt.Sprintf("effective_lines=%d effective_symbols=%d selected_label=%s", effectiveLines, effectiveSymbols, selected.Name))

	h.logPullRequestTargetStage(target, "label_check", fmt.Sprintf("verifying label %s exists", selected.Name))
	if err := h.verifyLabelExists(ctx, client, target.Owner, target.Repo, selected.Name); err != nil {
		h.logPullRequestTargetFailure(target, "label_check", err)
		return err
	}

	h.logPullRequestTargetStage(target, "labels_cleanup", "removing previously configured size labels")
	if err := h.removeExistingLabels(ctx, client, target.Owner, target.Repo, target.Number, target.Labels, labelSet, selected.Name); err != nil {
		h.logPullRequestTargetFailure(target, "labels_cleanup", err)
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

func (e pullRequestEvent) defaultBranch() string {
	if defaultBranch := strings.TrimSpace(e.Repository.DefaultBranch); defaultBranch != "" {
		return defaultBranch
	}
	return strings.TrimSpace(e.PullRequest.Base.Ref)
}

func (e pullRequestEvent) target() pullRequestTarget {
	target := pullRequestTarget{
		Action:         e.Action,
		InstallationID: e.Installation.ID,
		Owner:          e.Repository.Owner.Login,
		Repo:           e.Repository.Name,
		Number:         e.prNumber(),
		BaseRef:        e.PullRequest.Base.Ref,
	}
	for _, label := range e.PullRequest.Labels {
		target.Labels = append(target.Labels, pullRequestLabel{Name: label.Name})
	}
	return target
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

func (h *Handler) resolveDefaultBranch(ctx context.Context, client *githubapi.Client, owner, repo, known string) (string, error) {
	if branch := strings.TrimSpace(known); branch != "" {
		return branch, nil
	}
	repository, err := client.GetRepository(ctx, owner, repo)
	if err != nil {
		return "", err
	}
	branch := strings.TrimSpace(repository.DefaultBranch)
	if branch == "" {
		return "", fmt.Errorf("repository %s/%s has empty default branch", owner, repo)
	}
	return branch, nil
}

func (h *Handler) loadRepositoryLabelsConfig(ctx context.Context, client *githubapi.Client, owner, repo, knownDefaultBranch string) (config.LabelsConfig, string, error) {
	defaultBranch, err := h.resolveDefaultBranch(ctx, client, owner, repo, knownDefaultBranch)
	if err != nil {
		return config.LabelsConfig{}, defaultBranch, err
	}

	labelsContent, err := client.GetRepositoryContent(ctx, owner, repo, ".github/labels.yml", defaultBranch)
	if err != nil {
		if err == githubapi.ErrNotFound {
			return config.LabelsConfig{}, defaultBranch, errLabelsConfigMissing
		}
		return config.LabelsConfig{}, defaultBranch, err
	}

	labelsConfig, err := config.LoadLabelsConfig(labelsContent)
	if err != nil {
		return config.LabelsConfig{}, defaultBranch, fmt.Errorf("%w: %v", errLabelsConfigInvalid, err)
	}
	return labelsConfig, defaultBranch, nil
}

func pullRequestTouchesLabelsConfig(files []githubapi.PullRequestFile) bool {
	for _, file := range files {
		if file.Filename == ".github/labels.yml" {
			return true
		}
	}
	return false
}

func (h *Handler) handlePullRequestLabelsConfigError(target pullRequestTarget, defaultBranch string, err error) error {
	if h.logPullRequestLabelsConfigSkip(target, defaultBranch, err) {
		return nil
	}
	h.logPullRequestTargetFailure(target, "labels_config", err)
	return err
}

func (h *Handler) handlePullRequestConfigChangeLabelsError(event pullRequestEvent, defaultBranch string, err error) error {
	if errors.Is(err, errLabelsConfigMissing) {
		h.logPullRequestStage(event, "labels_config", fmt.Sprintf("skipping: .github/labels.yml is missing from default branch %s", safeBranch(defaultBranch)))
		return nil
	}
	if errors.Is(err, errLabelsConfigInvalid) {
		h.logPullRequestStage(event, "labels_config", fmt.Sprintf("skipping: invalid .github/labels.yml on default branch %s (%v)", safeBranch(defaultBranch), err))
		return nil
	}
	h.logPullRequestFailure(event, "labels_config", err)
	return err
}

func (h *Handler) logPullRequestLabelsConfigSkip(target pullRequestTarget, defaultBranch string, err error) bool {
	if errors.Is(err, errLabelsConfigMissing) {
		h.logPullRequestTargetStage(target, "labels_config", fmt.Sprintf("skipping: .github/labels.yml is missing from default branch %s", safeBranch(defaultBranch)))
		return true
	}
	if errors.Is(err, errLabelsConfigInvalid) {
		h.logPullRequestTargetStage(target, "labels_config", fmt.Sprintf("skipping: invalid .github/labels.yml on default branch %s (%v)", safeBranch(defaultBranch), err))
		return true
	}
	return false
}

func (h *Handler) logConnectLabelsConfigSkip(owner, repo, defaultBranch string, err error) bool {
	if errors.Is(err, errLabelsConfigMissing) {
		h.logger.Printf("connect_relabel repository=%s/%s default_branch=%s skipped labels_config_missing=true", owner, repo, safeBranch(defaultBranch))
		return true
	}
	if errors.Is(err, errLabelsConfigInvalid) {
		h.logger.Printf("connect_relabel repository=%s/%s default_branch=%s skipped labels_config_invalid=true error=%v", owner, repo, safeBranch(defaultBranch), err)
		return true
	}
	return false
}

func safeBranch(branch string) string {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "unknown"
	}
	return branch
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

func (h *Handler) verifyLabelExists(ctx context.Context, client *githubapi.Client, owner, repo, name string) error {
	resp, err := client.GetLabel(ctx, owner, repo, name)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("selected label %q does not exist in %s/%s", name, owner, repo)
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
