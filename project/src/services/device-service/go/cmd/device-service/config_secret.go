package main

import (
	"fmt"
	"os"
	"strings"
)

func configValue(name string) (string, error) {
	if file := os.Getenv(name + "_FILE"); file != "" {
		contents, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("%s_FILE cannot be read", name)
		}
		value := strings.TrimRight(string(contents), "\r\n")
		if value == "" {
			return "", fmt.Errorf("%s_FILE is empty", name)
		}
		return value, nil
	}
	return os.Getenv(name), nil
}
