package auth

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type TokenProvider interface {
	Token(ctx context.Context, installationID int64) (string, error)
}

type StaticTokenProvider string

func (s StaticTokenProvider) Token(context.Context, int64) (string, error) {
	return string(s), nil
}

type AppTokenProvider struct {
	appID      int64
	privateKey *rsa.PrivateKey
	baseURL    *url.URL
	client     *http.Client
}

func NewAppTokenProvider(appID int64, privateKeyPEM, baseURL string) (*AppTokenProvider, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("decode private key PEM")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		parsed, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if parseErr != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		rsaKey, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key must be RSA")
		}
		key = rsaKey
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse GitHub API base URL: %w", err)
	}
	if !strings.HasSuffix(parsedURL.Path, "/") {
		parsedURL.Path += "/"
	}
	return &AppTokenProvider{appID: appID, privateKey: key, baseURL: parsedURL, client: http.DefaultClient}, nil
}

func (p *AppTokenProvider) Token(ctx context.Context, installationID int64) (string, error) {
	token, err := p.appJWT()
	if err != nil {
		return "", err
	}
	rel := fmt.Sprintf("app/installations/%d/access_tokens", installationID)
	endpoint := p.baseURL.ResolveReference(&url.URL{Path: rel})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("create installation token: unexpected status %d", resp.StatusCode)
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	return payload.Token, nil
}

func (p *AppTokenProvider) appJWT() (string, error) {
	now := time.Now().UTC()
	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now.Add(-1 * time.Minute)),
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),
		Issuer:    fmt.Sprintf("%d", p.appID),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(p.privateKey)
}
