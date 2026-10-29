package main

import (
	"crypto/elliptic"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/mail"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL       string
	ListenAddress     string
	PublicOrigin      string
	VAPIDPublicKey    string
	VAPIDPrivateKey   string
	VAPIDSubject      string
	EncryptionKey     []byte
	AuthServiceURL    string
	AuthServiceToken  string
	EventServiceToken string
	MaxEventAge       time.Duration
}

func LoadConfig() (Config, error) {
	var configErr error
	read := func(name string) string {
		if configErr != nil {
			return ""
		}
		value, err := configValue(name)
		if err != nil {
			configErr = err
			return ""
		}
		return value
	}
	c := Config{
		DatabaseURL:       read("DATABASE_URL"),
		ListenAddress:     env("PUSH_LISTEN_ADDRESS", ":8080"),
		PublicOrigin:      strings.TrimRight(os.Getenv("PUBLIC_ORIGIN"), "/"),
		VAPIDPublicKey:    read("PUSH_VAPID_PUBLIC_KEY"),
		VAPIDPrivateKey:   read("PUSH_VAPID_PRIVATE_KEY"),
		VAPIDSubject:      os.Getenv("PUSH_VAPID_SUBJECT"),
		AuthServiceURL:    strings.TrimRight(os.Getenv("AUTH_SERVICE_INTERNAL_URL"), "/"),
		AuthServiceToken:  read("PUSH_AUTH_SERVICE_TOKEN"),
		EventServiceToken: read("PUSH_EVENT_SERVICE_TOKEN"),
		MaxEventAge:       envDuration("PUSH_MAX_EVENT_AGE", 24*time.Hour),
	}
	if configErr != nil {
		return c, configErr
	}
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}
	if err := validateOrigin(c.PublicOrigin); err != nil {
		return c, fmt.Errorf("PUBLIC_ORIGIN: %w", err)
	}
	if c.VAPIDPublicKey == "" || c.VAPIDPrivateKey == "" || c.VAPIDSubject == "" {
		return c, errors.New("PUSH_VAPID_PUBLIC_KEY, PUSH_VAPID_PRIVATE_KEY, and PUSH_VAPID_SUBJECT are required")
	}
	if err := validateVAPIDKeyPair(c.VAPIDPublicKey, c.VAPIDPrivateKey); err != nil {
		return c, fmt.Errorf("VAPID key pair: %w", err)
	}
	subject, err := normalizeVAPIDSubject(c.VAPIDSubject)
	if err != nil {
		return c, fmt.Errorf("PUSH_VAPID_SUBJECT: %w", err)
	}
	c.VAPIDSubject = subject
	if c.AuthServiceToken == "" || c.EventServiceToken == "" {
		return c, errors.New("PUSH_AUTH_SERVICE_TOKEN and PUSH_EVENT_SERVICE_TOKEN are required")
	}
	parsedAuthURL, err := url.ParseRequestURI(c.AuthServiceURL)
	if err != nil || (parsedAuthURL.Scheme != "http" && parsedAuthURL.Scheme != "https") || parsedAuthURL.Host == "" || parsedAuthURL.User != nil {
		return c, errors.New("AUTH_SERVICE_INTERNAL_URL must be an absolute HTTP(S) URL without user information")
	}
	keyRaw := read("PUSH_ENCRYPTION_KEY")
	if configErr != nil {
		return c, configErr
	}
	key, err := base64.StdEncoding.DecodeString(keyRaw)
	if err != nil || len(key) != 32 {
		return c, errors.New("PUSH_ENCRYPTION_KEY must be base64-encoded 32-byte key material")
	}
	c.EncryptionKey = key
	return c, nil
}

func validateVAPIDKeyPair(publicEncoded, privateEncoded string) error {
	decode := func(value string) ([]byte, error) {
		if decoded, err := base64.RawURLEncoding.DecodeString(value); err == nil {
			return decoded, nil
		}
		return base64.URLEncoding.DecodeString(value)
	}
	public, err := decode(publicEncoded)
	if err != nil {
		return errors.New("public key is not base64url")
	}
	private, err := decode(privateEncoded)
	if err != nil || len(private) != 32 {
		return errors.New("private key must be a base64url P-256 scalar")
	}
	curve := elliptic.P256()
	d := new(big.Int).SetBytes(private)
	if d.Sign() == 0 || d.Cmp(curve.Params().N) >= 0 {
		return errors.New("private key scalar is outside the P-256 range")
	}
	publicX, publicY := elliptic.Unmarshal(curve, public)
	if publicX == nil || publicY == nil {
		return errors.New("public key is not an uncompressed P-256 point")
	}
	derivedX, derivedY := curve.ScalarBaseMult(private)
	if publicX.Cmp(derivedX) != 0 || publicY.Cmp(derivedY) != 0 {
		return errors.New("public and private keys do not match")
	}
	return nil
}

func normalizeVAPIDSubject(value string) (string, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(strings.TrimPrefix(value, "mailto:"), "MAILTO:")
	if strings.HasPrefix(strings.ToLower(value), "https:") {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
			return "", errors.New("must be an email address or absolute HTTPS URL")
		}
		return value, nil
	}
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || !strings.Contains(value, "@") {
		return "", errors.New("must be an email address or absolute HTTPS URL")
	}
	return value, nil
}

func validateOrigin(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("must be an absolute HTTP(S) origin without credentials, path, query, or fragment")
	}
	return nil
}

func validatePushEndpoint(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Opaque != "" || strings.Contains(value, "#") || len(value) > 4096 {
		return errors.New("endpoint must be a valid HTTPS URL without credentials or fragment")
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil && !isPublicPushIP(ip) {
		return errors.New("endpoint must use a publicly routable host")
	}
	return nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return fallback
	}
	return duration
}
