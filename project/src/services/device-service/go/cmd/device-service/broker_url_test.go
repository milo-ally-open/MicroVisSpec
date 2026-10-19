package main

import "testing"

func TestValidateMQTTBrokerURL(t *testing.T) {
	for _, value := range []string{
		"ssl://mosquitto:8883",
		"ssl://192.168.0.1:19088",
		"ssl://[2001:db8::1]:8883",
	} {
		if err := validateMQTTBrokerURL(value); err != nil {
			t.Errorf("validateMQTTBrokerURL(%q): %v", value, err)
		}
	}
	for _, value := range []string{
		"",
		"ssl://",
		"tcp://mosquitto:1883",
		"ssl://user:password@mosquitto:8883",
		"ssl://mosquitto:8883/path",
		"ssl://mosquitto:0",
		"ssl://mosquitto:65536",
		" ssl://mosquitto:8883",
	} {
		if err := validateMQTTBrokerURL(value); err == nil {
			t.Errorf("validateMQTTBrokerURL(%q) unexpectedly succeeded", value)
		}
	}
}
