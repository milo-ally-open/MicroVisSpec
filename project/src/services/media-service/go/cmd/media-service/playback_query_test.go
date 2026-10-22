package main

import (
	"net/url"
	"testing"
)

func TestValidPlaybackQuery(t *testing.T) {
	const lease = "123e4567-e89b-12d3-a456-426614174001"
	const session = "123e4567-e89b-12d3-a456-426614174002"
	for _, test := range []struct {
		query   string
		allowed bool
	}{
		{"", true},
		{"lease_id=" + lease, true},
		{"lease_id=" + lease + "&cookieCheck=1", true},
		{"session=" + session, true},
		{"lease_id=" + lease + "&session=" + session, true},
		{"lease_id=" + lease + "&cookieCheck=1&session=" + session, true},
		{"lease_id=" + lease + "&lease_id=" + lease, false},
		{"session=" + session + "&session=" + session, false},
		{"cookieCheck=0", false},
		{"cookieCheck=1&cookieCheck=1", false},
		{"session=not-a-session", false},
		{"session=00000000-0000-0000-0000-000000000000", false},
		{"other=value", false},
	} {
		t.Run(test.query, func(t *testing.T) {
			values, err := url.ParseQuery(test.query)
			if err != nil {
				t.Fatal(err)
			}
			if got := validPlaybackQuery(values); got != test.allowed {
				t.Fatalf("allowed = %v, want %v", got, test.allowed)
			}
		})
	}
}
