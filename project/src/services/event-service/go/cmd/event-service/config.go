package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	DatabaseURL          string
	ListenAddress        string
	EvidenceDir          string
	BrokerURL            string
	BrokerUsername       string
	BrokerPassword       string
	BrokerClientID       string
	BrokerCAFile         string
	BrokerClientCertFile string
	BrokerClientKeyFile  string
	DeviceServiceURL     string
	DeviceServiceToken   string
	AuthServiceURL       string
	PushServiceURL       string
	PushServiceToken     string
	CursorSigningKey     []byte
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
		DatabaseURL:          read("DATABASE_URL"),
		ListenAddress:        env("EVENT_LISTEN_ADDRESS", ":8080"),
		EvidenceDir:          env("EVENT_EVIDENCE_DIR", "/var/lib/yunshao/evidence"),
		BrokerURL:            env("MQTT_BROKER_URL", "ssl://mosquitto:8883"),
		BrokerUsername:       read("MQTT_EVENT_SERVICE_USERNAME"),
		BrokerPassword:       read("MQTT_EVENT_SERVICE_PASSWORD"),
		BrokerClientID:       env("MQTT_EVENT_SERVICE_CLIENT_ID", "yunshao-event-service"),
		BrokerCAFile:         os.Getenv("MQTT_CA_FILE"),
		BrokerClientCertFile: os.Getenv("MQTT_CLIENT_CERT_FILE"),
		BrokerClientKeyFile:  os.Getenv("MQTT_CLIENT_KEY_FILE"),
		DeviceServiceURL:     strings.TrimRight(os.Getenv("DEVICE_SERVICE_INTERNAL_URL"), "/"),
		DeviceServiceToken:   read("DEVICE_SERVICE_INTERNAL_TOKEN"),
		AuthServiceURL:       strings.TrimRight(os.Getenv("AUTH_SERVICE_INTERNAL_URL"), "/"),
		PushServiceURL:       strings.TrimRight(os.Getenv("PUSH_SERVICE_INTERNAL_URL"), "/"),
		PushServiceToken:     read("PUSH_SERVICE_INTERNAL_TOKEN"),
	}
	if configErr != nil {
		return c, configErr
	}
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}
	if c.BrokerUsername == "" || c.BrokerPassword == "" {
		return c, errors.New("MQTT_EVENT_SERVICE_USERNAME and MQTT_EVENT_SERVICE_PASSWORD are required")
	}
	parsed, err := url.Parse(c.BrokerURL)
	if err != nil || parsed.Scheme != "ssl" || parsed.Hostname() == "" {
		return c, errors.New("MQTT_BROKER_URL must be an ssl:// URL with a host")
	}
	if c.DeviceServiceToken == "" {
		return c, errors.New("DEVICE_SERVICE_INTERNAL_TOKEN is required")
	}
	if err := validateServiceURL("DEVICE_SERVICE_INTERNAL_URL", c.DeviceServiceURL); err != nil {
		return c, err
	}
	if err := validateServiceURL("AUTH_SERVICE_INTERNAL_URL", c.AuthServiceURL); err != nil {
		return c, err
	}
	if c.PushServiceToken == "" {
		return c, errors.New("PUSH_SERVICE_INTERNAL_TOKEN is required")
	}
	if err := validateServiceURL("PUSH_SERVICE_INTERNAL_URL", c.PushServiceURL); err != nil {
		return c, err
	}
	cursorKeyRaw := read("EVENT_CURSOR_SIGNING_KEY")
	if configErr != nil {
		return c, configErr
	}
	cursorKey, err := base64.StdEncoding.DecodeString(cursorKeyRaw)
	if err != nil || len(cursorKey) < 32 {
		return c, errors.New("EVENT_CURSOR_SIGNING_KEY must be base64-encoded with at least 32 bytes")
	}
	c.CursorSigningKey = cursorKey
	if (c.BrokerClientCertFile == "") != (c.BrokerClientKeyFile == "") {
		return c, errors.New("MQTT client certificate and key must be configured together")
	}
	if strings.TrimSpace(c.EvidenceDir) == "" || !filepath.IsAbs(c.EvidenceDir) {
		return c, errors.New("EVENT_EVIDENCE_DIR must be an absolute path")
	}
	return c, nil
}

func validateServiceURL(name, value string) error {
	parsed, err := url.ParseRequestURI(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("%s must be an absolute HTTP(S) URL without user information", name)
	}
	return nil
}

func mqttTLSConfig(config Config) (*tls.Config, error) {
	parsed, err := url.Parse(config.BrokerURL)
	if err != nil || parsed.Scheme != "ssl" || parsed.Hostname() == "" {
		return nil, errors.New("MQTT broker URL must use ssl:// with a hostname")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: parsed.Hostname()}
	if config.BrokerCAFile != "" {
		pem, err := os.ReadFile(config.BrokerCAFile)
		if err != nil {
			return nil, fmt.Errorf("read MQTT CA file: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("MQTT CA file contains no certificates")
		}
		tlsConfig.RootCAs = roots
	}
	if config.BrokerClientCertFile != "" {
		cert, err := tls.LoadX509KeyPair(config.BrokerClientCertFile, config.BrokerClientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("load MQTT client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	return tlsConfig, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
