package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	DatabaseURL              string
	ListenAddress            string
	InternalDeviceToken      string
	InternalAuthToken        string
	InternalNginxToken       string
	DeviceServiceURL         string
	DeviceServiceToken       string
	MediaMTXAPIURL           string
	MediaMTXUsername         string
	MediaMTXPassword         string
	MediaSourceEncryptionKey []byte
	PublicMediaBaseURL       string
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
		DatabaseURL:         read("DATABASE_URL"),
		ListenAddress:       env("MEDIA_LISTEN_ADDRESS", ":8080"),
		InternalDeviceToken: read("MEDIA_DEVICE_SERVICE_TOKEN"),
		InternalAuthToken:   read("MEDIA_AUTH_SERVICE_TOKEN"),
		InternalNginxToken:  read("MEDIA_NGINX_INTERNAL_TOKEN"),
		DeviceServiceURL:    strings.TrimRight(os.Getenv("DEVICE_SERVICE_INTERNAL_URL"), "/"),
		DeviceServiceToken:  read("DEVICE_SERVICE_INTERNAL_TOKEN"),
		MediaMTXAPIURL:      strings.TrimRight(os.Getenv("MEDIAMTX_API_URL"), "/"),
		MediaMTXUsername:    read("MEDIAMTX_API_USERNAME"),
		MediaMTXPassword:    read("MEDIAMTX_API_PASSWORD"),
		PublicMediaBaseURL:  strings.TrimRight(os.Getenv("MEDIA_PUBLIC_BASE_URL"), "/"),
	}
	if configErr != nil {
		return c, configErr
	}
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}
	if c.InternalDeviceToken == "" || c.InternalAuthToken == "" || c.InternalNginxToken == "" {
		return c, errors.New("MEDIA_DEVICE_SERVICE_TOKEN, MEDIA_AUTH_SERVICE_TOKEN, and MEDIA_NGINX_INTERNAL_TOKEN are required")
	}
	if c.DeviceServiceToken == "" {
		return c, errors.New("DEVICE_SERVICE_INTERNAL_TOKEN is required")
	}
	if err := validateHTTPURL("DEVICE_SERVICE_INTERNAL_URL", c.DeviceServiceURL); err != nil {
		return c, err
	}
	if err := validateHTTPURL("MEDIAMTX_API_URL", c.MediaMTXAPIURL); err != nil {
		return c, err
	}
	if c.MediaMTXUsername == "" || c.MediaMTXPassword == "" {
		return c, errors.New("MEDIAMTX_API_USERNAME and MEDIAMTX_API_PASSWORD are required")
	}
	keyRaw := read("MEDIA_SOURCE_ENCRYPTION_KEY")
	if configErr != nil {
		return c, configErr
	}
	key, err := base64.StdEncoding.DecodeString(keyRaw)
	if err != nil || len(key) != 32 {
		return c, errors.New("MEDIA_SOURCE_ENCRYPTION_KEY must be base64-encoded 32-byte key material")
	}
	c.MediaSourceEncryptionKey = key
	if c.PublicMediaBaseURL != "" {
		if err := validateHTTPURL("MEDIA_PUBLIC_BASE_URL", c.PublicMediaBaseURL); err != nil {
			return c, err
		}
	}
	return c, nil
}

func validateHTTPURL(name, value string) error {
	parsed, err := url.ParseRequestURI(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("%s must be an absolute HTTP(S) URL without user information", name)
	}
	return nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
