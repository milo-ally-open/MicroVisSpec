package main

import (
	"strings"
	"testing"
)

func TestPasswordHashAndUsernameValidation(t *testing.T) {
	hash, err := hashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !verifyPassword(hash, "correct horse battery staple") {
		t.Fatal("expected password hash to verify")
	}
	if verifyPassword(hash, "incorrect password") {
		t.Fatal("unexpected password match")
	}
	for _, username := range []string{"ab", "with-dash", "has space", "nonascii-用户"} {
		if validUsername(username) {
			t.Errorf("validUsername(%q) = true", username)
		}
	}
	for _, username := range []string{"abc", "classroom_01", "A1_"} {
		if !validUsername(strings.ToLower(username)) {
			t.Errorf("validUsername(%q) = false", username)
		}
	}
}
