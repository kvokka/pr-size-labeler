package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"pr-size-labeler/internal/auth"
	"pr-size-labeler/internal/config"
	"pr-size-labeler/internal/githubapi"
	"pr-size-labeler/internal/webhook"
)

func main() {
	env, err := config.LoadEnv()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("startup config app_id=%d listen_addr=%s github_api_base_url=%s %s", env.AppID, env.ListenAddr, env.GitHubAPIBaseURL, privateKeyDiagnosticSummary(env.PrivateKeyPEM))
	outboundClient := &http.Client{Timeout: 30 * time.Second}

	tokenProvider, err := auth.NewAppTokenProvider(env.AppID, env.PrivateKeyPEM, env.GitHubAPIBaseURL, outboundClient)
	if err != nil {
		log.Printf("token provider initialization failed: %v; %s", err, privateKeyDiagnosticSummary(env.PrivateKeyPEM))
		log.Fatal(err)
	}

	handler := webhook.NewHandler(
		env.WebhookSecret,
		tokenProvider,
		func(token string) *githubapi.Client {
			return githubapi.NewClient(env.GitHubAPIBaseURL, token, outboundClient)
		},
	)

	server := &http.Server{
		Addr:    env.ListenAddr,
		Handler: handler,
	}

	log.Printf("pr-size-labeler listening on %s", env.ListenAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}

	_ = os.Stdout.Sync()
	_ = os.Stderr.Sync()
}

func privateKeyDiagnosticSummary(privateKey string) string {
	return fmt.Sprintf(
		"private_key[len=%d prefix=%q begin_marker=%t end_marker=%t newline_count=%d contains_escaped_newline=%t contains_carriage_return=%t]",
		len(privateKey),
		safePrefix(privateKey, 4),
		strings.Contains(privateKey, "-----BEGIN"),
		strings.Contains(privateKey, "-----END"),
		strings.Count(privateKey, "\n"),
		strings.Contains(privateKey, `\n`) || strings.Contains(privateKey, `\r`),
		strings.Contains(privateKey, "\r"),
	)
}

func safePrefix(value string, count int) string {
	runes := []rune(value)
	if len(runes) < count {
		count = len(runes)
	}
	if count <= 0 {
		return ""
	}
	return string(runes[:count])
}
