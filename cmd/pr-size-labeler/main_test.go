package main

import (
	"bytes"
	"log"
	"strings"
	"testing"

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
