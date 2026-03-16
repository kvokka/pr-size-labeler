package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
	"testing"

	"pr-size-labeler/internal/auth"
)

func TestLoadEnvNormalizesPrivateKeyFormats(t *testing.T) {
	validPEM := testRSAPrivateKeyPEM(t)
	tests := []struct {
		name string
		raw  string
	}{
		{name: "raw pem", raw: validPEM},
		{name: "escaped newlines", raw: strings.ReplaceAll(validPEM, "\n", `\n`)},
		{name: "quoted escaped newlines", raw: fmt.Sprintf("%q", strings.ReplaceAll(validPEM, "\n", `\n`))},
		{name: "quoted crlf pem", raw: "\"" + strings.ReplaceAll(validPEM, "\n", "\r\n") + "\""},
		{name: "flattened pem", raw: strings.ReplaceAll(validPEM, "\n", "")},
		{name: "surrounding whitespace", raw: "  \n" + validPEM + "\n  "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APP_ID", "123")
			t.Setenv("WEBHOOK_SECRET", "secret")
			t.Setenv("PRIVATE_KEY", tt.raw)

			env, err := LoadEnv()
			if err != nil {
				t.Fatalf("LoadEnv returned error: %v", err)
			}
			if strings.Contains(env.PrivateKeyPEM, `\n`) {
				t.Fatalf("normalized key still contains escaped newline: %q", env.PrivateKeyPEM)
			}
			if _, err := auth.NewAppTokenProvider(env.AppID, env.PrivateKeyPEM, "https://api.github.com/", nil); err != nil {
				t.Fatalf("NewAppTokenProvider returned error after normalization: %v", err)
			}
		})
	}
}

func testRSAPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
	return string(pem.EncodeToMemory(block))
}
