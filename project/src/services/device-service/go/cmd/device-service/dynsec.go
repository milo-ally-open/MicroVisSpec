package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
)

const dynsecRequestTopic = "$CONTROL/dynamic-security/v1"
const dynsecResponseTopic = "$CONTROL/dynamic-security/v1/response"

type DeviceBroker interface {
	Check(context.Context) error
	Provision(context.Context, uuid.UUID, string, string, string) error
	Disable(context.Context, uuid.UUID, string) error
}

type DynamicSecurityBroker struct {
	url       string
	username  string
	password  string
	clientID  string
	tlsConfig *tls.Config
	timeout   time.Duration
	mu        sync.Mutex
}

type dynsecResponse struct {
	Responses []struct {
		Command string          `json:"command"`
		Error   string          `json:"error"`
		Data    json.RawMessage `json:"data"`
	} `json:"responses"`
}

type dynsecCommandError struct {
	Command string
	Message string
}

func (e *dynsecCommandError) Error() string { return e.Command + ": " + e.Message }

func NewDynamicSecurityBroker(config Config) (*DynamicSecurityBroker, error) {
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
	return &DynamicSecurityBroker{url: config.BrokerURL, username: config.BrokerAdminUsername, password: config.BrokerAdminPassword, clientID: config.BrokerAdminClientID, tlsConfig: tlsConfig, timeout: 8 * time.Second}, nil
}

func (b *DynamicSecurityBroker) Check(ctx context.Context) error {
	_, err := b.command(ctx, map[string]any{"command": "getDetails"})
	return err
}

func (b *DynamicSecurityBroker) Provision(ctx context.Context, deviceID uuid.UUID, username, clientID, password string) error {
	role := deviceRoleName(deviceID)
	acls := deviceACLs(deviceID)
	if _, err := b.command(ctx, map[string]any{"command": "getRole", "rolename": role}); err != nil {
		if !isMissingDynsecObject(err, "role") {
			return err
		}
		if _, err := b.command(ctx, map[string]any{"command": "createRole", "rolename": role, "textname": "Yunshao edge device", "acls": acls}); err != nil {
			return err
		}
	} else {
		if _, err := b.command(ctx, map[string]any{"command": "modifyRole", "rolename": role, "acls": acls}); err != nil {
			return err
		}
	}
	roles := []map[string]any{{"rolename": role, "priority": 100}}
	if _, err := b.command(ctx, map[string]any{"command": "getClient", "username": username}); err != nil {
		if !isMissingDynsecObject(err, "client") {
			return err
		}
		_, err = b.command(ctx, map[string]any{"command": "createClient", "username": username, "password": password, "clientid": clientID, "textname": "Yunshao edge device " + deviceID.String(), "roles": roles})
		return err
	}
	if _, err := b.command(ctx, map[string]any{"command": "disableClient", "username": username}); err != nil && !strings.Contains(strings.ToLower(err.Error()), "disabled") {
		return err
	}
	if _, err := b.command(ctx, map[string]any{"command": "modifyClient", "username": username, "password": password, "clientid": clientID, "roles": roles}); err != nil {
		return err
	}
	_, err := b.command(ctx, map[string]any{"command": "enableClient", "username": username})
	return err
}

func (b *DynamicSecurityBroker) Disable(ctx context.Context, _ uuid.UUID, username string) error {
	_, err := b.command(ctx, map[string]any{"command": "disableClient", "username": username})
	if isMissingDynsecObject(err, "client") {
		return nil
	}
	return err
}

func isMissingDynsecObject(err error, object string) bool {
	var commandErr *dynsecCommandError
	if !errors.As(err, &commandErr) {
		return false
	}
	message := strings.ToLower(commandErr.Message)
	return strings.Contains(message, "unknown "+object) || strings.Contains(message, object+" not found") || strings.Contains(message, "no such "+object)
}

func deviceRoleName(deviceID uuid.UUID) string {
	return "yunshao-role-" + strings.ReplaceAll(deviceID.String(), "-", "")
}

func deviceACLs(deviceID uuid.UUID) []map[string]any {
	base := "devices/" + deviceID.String() + "/"
	acls := []map[string]any{}
	for _, topic := range []string{"state", "events", "command-acks"} {
		acls = append(acls, map[string]any{"acltype": "publishClientSend", "topic": base + topic, "priority": 100, "allow": true})
	}
	for _, topic := range []string{"commands", "event-acks"} {
		acls = append(acls,
			map[string]any{"acltype": "subscribeLiteral", "topic": base + topic, "priority": 100, "allow": true},
			map[string]any{"acltype": "publishClientReceive", "topic": base + topic, "priority": 100, "allow": true},
		)
	}
	return acls
}

func (b *DynamicSecurityBroker) command(ctx context.Context, command map[string]any) (json.RawMessage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	deadline, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()

	options := mqtt.NewClientOptions().AddBroker(b.url).SetClientID(b.clientID).SetUsername(b.username).SetPassword(b.password).
		SetProtocolVersion(4).SetTLSConfig(b.tlsConfig).SetConnectTimeout(3 * time.Second).SetKeepAlive(15 * time.Second)
	client := mqtt.NewClient(options)
	connect := client.Connect()
	if !connect.WaitTimeout(4 * time.Second) {
		client.Disconnect(100)
		return nil, errors.New("MQTT admin connection timed out")
	}
	if err := connect.Error(); err != nil {
		client.Disconnect(100)
		return nil, fmt.Errorf("MQTT admin connection failed: %w", err)
	}
	defer client.Disconnect(100)

	responseChannel := make(chan dynsecResponse, 1)
	subscribe := client.Subscribe(dynsecResponseTopic, 1, func(_ mqtt.Client, message mqtt.Message) {
		var response dynsecResponse
		if json.Unmarshal(message.Payload(), &response) == nil {
			select {
			case responseChannel <- response:
			default:
			}
		}
	})
	if !subscribe.WaitTimeout(3*time.Second) || subscribe.Error() != nil {
		return nil, errors.New("MQTT admin response subscription failed")
	}
	payload, err := json.Marshal(map[string]any{"commands": []any{command}})
	if err != nil {
		return nil, err
	}
	publish := client.Publish(dynsecRequestTopic, 1, false, payload)
	if !publish.WaitTimeout(3*time.Second) || publish.Error() != nil {
		return nil, errors.New("MQTT dynamic security request failed")
	}
	select {
	case response := <-responseChannel:
		if len(response.Responses) != 1 {
			return nil, errors.New("MQTT dynamic security returned an invalid response")
		}
		item := response.Responses[0]
		if item.Error != "" {
			return nil, &dynsecCommandError{Command: item.Command, Message: item.Error}
		}
		return item.Data, nil
	case <-deadline.Done():
		return nil, errors.New("MQTT dynamic security response timed out")
	}
}
