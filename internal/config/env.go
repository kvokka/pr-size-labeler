package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Env struct {
	ListenAddr       string
	WebhookSecret    string
	AppID            int64
	PrivateKeyPEM    string
	GitHubAPIBaseURL string
}

func LoadEnv() (Env, error) {
	listenAddr := strings.TrimSpace(os.Getenv("LISTEN_ADDR"))
	if listenAddr == "" {
		listenAddr = ":8080"
	}

	appIDRaw := strings.TrimSpace(os.Getenv("APP_ID"))
	if appIDRaw == "" {
		return Env{}, errors.New("APP_ID is required")
	}
	appID, err := strconv.ParseInt(appIDRaw, 10, 64)
	if err != nil {
		return Env{}, fmt.Errorf("parse APP_ID: %w", err)
	}

	privateKeyPEM := strings.TrimSpace(os.Getenv("PRIVATE_KEY"))
	if privateKeyPEM == "" {
		return Env{}, errors.New("PRIVATE_KEY is required")
	}
	privateKeyPEM = strings.ReplaceAll(privateKeyPEM, `\n`, "\n")

	webhookSecret := strings.TrimSpace(os.Getenv("WEBHOOK_SECRET"))
	if webhookSecret == "" {
		return Env{}, errors.New("WEBHOOK_SECRET is required")
	}

	apiBaseURL := strings.TrimSpace(os.Getenv("GITHUB_API_BASE_URL"))
	if apiBaseURL == "" {
		apiBaseURL = "https://api.github.com/"
	}

	return Env{
		ListenAddr:       listenAddr,
		WebhookSecret:    webhookSecret,
		AppID:            appID,
		PrivateKeyPEM:    privateKeyPEM,
		GitHubAPIBaseURL: apiBaseURL,
	}, nil
}
