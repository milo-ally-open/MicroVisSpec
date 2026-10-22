package main

import (
	"github.com/google/uuid"
	"net/url"
)

// MediaMTX HLS uses a cookie-check redirect and, when upstream cookies are
// stripped, a session UUID in child playlists and segments. These parameters
// never replace the browser session and owned viewer lease checked below.
func validPlaybackQuery(values url.Values) bool {
	for key, items := range values {
		if len(items) != 1 {
			return false
		}
		switch key {
		case "lease_id", "session":
			id, err := uuid.Parse(items[0])
			if err != nil || id == uuid.Nil {
				return false
			}
		case "cookieCheck":
			if items[0] != "1" {
				return false
			}
		default:
			return false
		}
	}
	return true
}
