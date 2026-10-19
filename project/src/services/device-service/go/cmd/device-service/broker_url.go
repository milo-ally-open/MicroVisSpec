package main

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

var errInvalidMQTTBrokerURL = errors.New("invalid MQTT broker URL")

func validateMQTTBrokerURL(value string) error {
	if strings.TrimSpace(value) != value || value == "" {
		return errInvalidMQTTBrokerURL
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "ssl" || parsed.Hostname() == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errInvalidMQTTBrokerURL
	}
	if port := parsed.Port(); port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return errInvalidMQTTBrokerURL
		}
	}
	return nil
}
