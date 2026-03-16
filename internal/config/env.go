package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"
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

	privateKeyPEM := normalizePrivateKeyPEM(os.Getenv("PRIVATE_KEY"))
	if privateKeyPEM == "" {
		return Env{}, errors.New("PRIVATE_KEY is required")
	}

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

func normalizePrivateKeyPEM(value string) string {
	normalized := strings.TrimSpace(value)
	if len(normalized) >= 2 {
		if normalized[0] == '"' && normalized[len(normalized)-1] == '"' {
			if unquoted, err := strconv.Unquote(normalized); err == nil {
				normalized = unquoted
			} else {
				normalized = normalized[1 : len(normalized)-1]
			}
		} else if normalized[0] == '\'' && normalized[len(normalized)-1] == '\'' {
			normalized = normalized[1 : len(normalized)-1]
		}
	}
	replacer := strings.NewReplacer(`\r\n`, "\n", `\n`, "\n", `\r`, "\n")
	normalized = replacer.Replace(normalized)
	normalized = strings.ReplaceAll(normalized, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	normalized = reshapeSingleLinePEM(normalized)
	return strings.TrimSpace(normalized)
}

func reshapeSingleLinePEM(value string) string {
	if strings.Contains(value, "\n") {
		return value
	}
	if !strings.HasPrefix(value, "-----BEGIN ") {
		return value
	}
	footerStart := strings.LastIndex(value, "-----END ")
	if footerStart <= 0 {
		return value
	}
	header, ok := pemBoundary(value, 0, "-----BEGIN ")
	if !ok || len(header) >= footerStart {
		return value
	}
	footer, ok := pemBoundary(value, footerStart, "-----END ")
	if !ok || footerStart+len(footer) != len(value) {
		return value
	}
	body := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value[len(header):footerStart])
	if body == "" {
		return value
	}
	chunks := make([]string, 0, (len(body)+63)/64)
	for len(body) > 64 {
		chunks = append(chunks, body[:64])
		body = body[64:]
	}
	chunks = append(chunks, body)
	return strings.Join([]string{header, strings.Join(chunks, "\n"), footer}, "\n")
}

func pemBoundary(value string, start int, prefix string) (string, bool) {
	if !strings.HasPrefix(value[start:], prefix) {
		return "", false
	}
	rest := value[start+len(prefix):]
	idx := strings.Index(rest, "-----")
	if idx == -1 {
		return "", false
	}
	return value[start : start+len(prefix)+idx+5], true
}
