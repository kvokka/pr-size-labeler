package main

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"

	"pr-size-labeler/internal/config"
)

func TestPrivateKeyDiagnosticSummaryIsSafe(t *testing.T) {
	privateKey := "-----BEGIN RSA PRIVATE KEY-----\nsecret-line-1\nsecret-line-2\n-----END RSA PRIVATE KEY-----"
	summary := privateKeyDiagnosticSummary(privateKey)

	for _, want := range []string{`prefix="----"`, "begin_marker=true", "end_marker=true", "newline_count=3", "contains_escaped_newline=false", "contains_carriage_return=false"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q: %s", want, summary)
		}
	}
	for _, forbidden := range []string{"secret-line-1", "secret-line-2", "RSA PRIVATE KEY-----\nsecret"} {
		if strings.Contains(summary, forbidden) {
			t.Fatalf("summary leaked private key content %q: %s", forbidden, summary)
		}
	}
}

func TestLogStartupConfigDefaultRedactsDetails(t *testing.T) {
	var logs bytes.Buffer
	original := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(original)

	logStartupConfig(config.Env{AppID: 123, ListenAddr: ":8080", GitHubAPIBaseURL: "https://api.github.com/", PrivateKeyPEM: "secret", LogPrivateDetails: false})

	if !strings.Contains(logs.String(), "startup config private_details=false") {
		t.Fatalf("expected redacted startup log, got %q", logs.String())
	}
	if !strings.Contains(logs.String(), "connect_open_prs_backfill enabled=false explicit_event_subscriptions_required=false extra_permissions_required=false normal_pull_request_labeling_without_backfill=true") {
		t.Fatalf("expected connect backfill disabled log, got %q", logs.String())
	}
	for _, forbidden := range []string{"app_id=123", ":8080", "https://api.github.com/", "private_key["} {
		if strings.Contains(logs.String(), forbidden) {
			t.Fatalf("startup log leaked %q: %s", forbidden, logs.String())
		}
	}
}

func TestLogStartupConfigCanIncludeDetails(t *testing.T) {
	var logs bytes.Buffer
	original := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(original)

	logStartupConfig(config.Env{AppID: 123, ListenAddr: ":8080", GitHubAPIBaseURL: "https://api.github.com/", PrivateKeyPEM: "secret", LogPrivateDetails: true})

	for _, want := range []string{"startup config app_id=123", "listen_addr=:8080", "github_api_base_url=https://api.github.com/", `private_key[len=6 prefix="secr"`} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("startup log missing %q: %s", want, logs.String())
		}
	}
}

func TestLogStartupConfigExplainsOptionalBackfillEvents(t *testing.T) {
	var logs bytes.Buffer
	original := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(original)

	logStartupConfig(config.Env{ConnectOpenPRsBackfillEnabled: true, ConnectOpenPRsBackfillLookback: 365 * 24 * time.Hour, LogPrivateDetails: false})

	for _, want := range []string{
		"connect_open_prs_backfill enabled=true",
		"github_app_ui_does_not_currently_expose_installation_checkboxes=true",
		"installation_target_ui_event_is_different=true",
		"extra_permissions_required=false",
		"normal_pull_request_labeling_without_backfill=true",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("startup log missing %q: %s", want, logs.String())
		}
	}
}
