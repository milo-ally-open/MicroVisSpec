package main

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
)

type DeviceMessagePlane interface {
	Check(context.Context) error
	Start(context.Context, func(string, []byte)) error
	Publish(context.Context, string, []byte) error
	Close()
}

type PahoDeviceMessagePlane struct {
	config  Config
	client  mqtt.Client
	mu      sync.RWMutex
	handler func(string, []byte)
}

func NewPahoDeviceMessagePlane(config Config) (*PahoDeviceMessagePlane, error) {
	if config.BrokerServiceUser == "" || config.BrokerServicePass == "" {
		return nil, errors.New("device-service MQTT identity is required")
	}
	parsed, err := url.Parse(config.BrokerURL)
	if err != nil || parsed.Scheme != "ssl" || parsed.Hostname() == "" {
		return nil, errors.New("MQTT broker URL must be a valid ssl:// URL")
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
	if config.BrokerClientCertFile != "" || config.BrokerClientKeyFile != "" {
		if config.BrokerClientCertFile == "" || config.BrokerClientKeyFile == "" {
			return nil, errors.New("both MQTT client certificate and key must be configured")
		}
		cert, err := tls.LoadX509KeyPair(config.BrokerClientCertFile, config.BrokerClientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("load MQTT client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	plane := &PahoDeviceMessagePlane{config: config}
	options := mqtt.NewClientOptions().AddBroker(config.BrokerURL).SetClientID(config.BrokerServiceClient).
		SetUsername(config.BrokerServiceUser).SetPassword(config.BrokerServicePass).SetProtocolVersion(4).
		SetTLSConfig(tlsConfig).SetCleanSession(false).SetKeepAlive(30 * time.Second).
		SetConnectTimeout(5 * time.Second).SetAutoReconnect(true).SetConnectRetry(false)
	options.SetOnConnectHandler(plane.onConnect)
	options.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		slog.Warn("device_mqtt_connection_lost", "error", err.Error())
	})
	options.SetDefaultPublishHandler(plane.onMessage)
	plane.client = mqtt.NewClient(options)
	return plane, nil
}

func (m *PahoDeviceMessagePlane) Start(ctx context.Context, handler func(string, []byte)) error {
	m.mu.Lock()
	m.handler = handler
	m.mu.Unlock()
	connect := m.client.Connect()
	wait := 6 * time.Second
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < wait {
		wait = time.Until(deadline)
	}
	if wait <= 0 || !connect.WaitTimeout(wait) {
		return errors.New("device MQTT connection timed out")
	}
	if err := connect.Error(); err != nil {
		return fmt.Errorf("device MQTT connection failed: %w", err)
	}
	return nil
}

func (m *PahoDeviceMessagePlane) onConnect(client mqtt.Client) {
	for _, topic := range []string{"devices/+/state", "devices/+/command-acks"} {
		token := client.Subscribe(topic, 1, nil)
		if !token.WaitTimeout(5*time.Second) || token.Error() != nil {
			slog.Error("device_mqtt_subscribe_failed", "topic", topic, "error", token.Error())
		}
	}
	slog.Info("device_mqtt_subscribed", "topics", []string{"devices/+/state", "devices/+/command-acks"})
}

func (m *PahoDeviceMessagePlane) onMessage(_ mqtt.Client, message mqtt.Message) {
	m.mu.RLock()
	handler := m.handler
	m.mu.RUnlock()
	if handler == nil || message == nil || len(message.Payload()) == 0 || len(message.Payload()) > 64<<10 {
		return
	}
	handler(message.Topic(), append([]byte(nil), message.Payload()...))
}

func (m *PahoDeviceMessagePlane) Check(context.Context) error {
	if m.client == nil || !m.client.IsConnected() {
		return errors.New("device MQTT client is disconnected")
	}
	return nil
}

func (m *PahoDeviceMessagePlane) Publish(ctx context.Context, topic string, payload []byte) error {
	if err := m.Check(ctx); err != nil {
		return err
	}
	token := m.client.Publish(topic, 1, false, payload)
	wait := 5 * time.Second
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < wait {
		wait = time.Until(deadline)
	}
	if wait <= 0 || !token.WaitTimeout(wait) {
		return errors.New("device MQTT publish timed out")
	}
	return token.Error()
}

func (m *PahoDeviceMessagePlane) Close() {
	if m.client != nil && m.client.IsConnected() {
		m.client.Disconnect(250)
	}
}

func constantTimeEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func mqttJSON(payload []byte, destination any) error {
	if len(payload) > 64<<10 {
		return errors.New("MQTT payload exceeds 64 KiB")
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("MQTT payload must contain one JSON object")
	}
	return nil
}

func parseDeviceTopic(topic, suffix string) (uuid.UUID, error) {
	parts := strings.Split(topic, "/")
	if len(parts) != 3 || parts[0] != "devices" || parts[2] != suffix {
		return uuid.Nil, errors.New("unexpected MQTT topic")
	}
	id, err := uuid.Parse(parts[1])
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errors.New("invalid device id in MQTT topic")
	}
	return id, nil
}
