package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type PahoEventMessagePlane struct {
	client  mqtt.Client
	mu      sync.RWMutex
	handler func(string, []byte)
}

func NewPahoEventMessagePlane(config Config) (*PahoEventMessagePlane, error) {
	if config.BrokerUsername == "" || config.BrokerPassword == "" {
		return nil, errors.New("event-service MQTT identity is required")
	}
	tlsConfig, err := mqttTLSConfig(config)
	if err != nil {
		return nil, err
	}
	plane := &PahoEventMessagePlane{}
	options := mqtt.NewClientOptions().AddBroker(config.BrokerURL).SetClientID(config.BrokerClientID).
		SetUsername(config.BrokerUsername).SetPassword(config.BrokerPassword).SetProtocolVersion(4).
		SetTLSConfig(tlsConfig).SetCleanSession(false).SetKeepAlive(30 * time.Second).
		SetConnectTimeout(5 * time.Second).SetAutoReconnect(true).SetConnectRetry(false)
	options.SetOnConnectHandler(plane.onConnect)
	options.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		slog.Warn("event_mqtt_connection_lost", "error", err.Error())
	})
	options.SetDefaultPublishHandler(plane.onMessage)
	plane.client = mqtt.NewClient(options)
	return plane, nil
}

func (m *PahoEventMessagePlane) Start(ctx context.Context, handler func(string, []byte)) error {
	m.mu.Lock()
	m.handler = handler
	m.mu.Unlock()
	connect := m.client.Connect()
	wait := 6 * time.Second
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < wait {
		wait = time.Until(deadline)
	}
	if wait <= 0 || !connect.WaitTimeout(wait) {
		return errors.New("event MQTT connection timed out")
	}
	if err := connect.Error(); err != nil {
		return fmt.Errorf("event MQTT connection failed: %w", err)
	}
	return nil
}

func (m *PahoEventMessagePlane) onConnect(client mqtt.Client) {
	token := client.Subscribe("devices/+/events", 1, nil)
	if !token.WaitTimeout(5*time.Second) || token.Error() != nil {
		slog.Error("event_mqtt_subscribe_failed", "topic", "devices/+/events", "error", token.Error())
		return
	}
	slog.Info("event_mqtt_subscribed", "topic", "devices/+/events", "qos", 1)
}

func (m *PahoEventMessagePlane) onMessage(_ mqtt.Client, message mqtt.Message) {
	if message == nil || len(message.Payload()) == 0 || len(message.Payload()) > 64<<10 {
		return
	}
	m.mu.RLock()
	handler := m.handler
	m.mu.RUnlock()
	if handler != nil {
		handler(message.Topic(), append([]byte(nil), message.Payload()...))
	}
}

func (m *PahoEventMessagePlane) Publish(ctx context.Context, topic string, payload []byte) error {
	if err := m.Check(ctx); err != nil {
		return err
	}
	token := m.client.Publish(topic, 1, false, payload)
	wait := 5 * time.Second
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < wait {
		wait = time.Until(deadline)
	}
	if wait <= 0 || !token.WaitTimeout(wait) {
		return errors.New("event MQTT publish timed out")
	}
	return token.Error()
}

func (m *PahoEventMessagePlane) Check(context.Context) error {
	if m.client == nil || !m.client.IsConnected() {
		return errors.New("event MQTT client is disconnected")
	}
	return nil
}

func (m *PahoEventMessagePlane) Close() {
	if m.client != nil && m.client.IsConnected() {
		m.client.Disconnect(250)
	}
}
