package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMediaMTXPathConfigurationUsesOnDemandSource(t *testing.T) {
	var added map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v3/config/paths/get/stream-id":
			http.NotFound(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/v3/config/paths/add/stream-id":
			if user, password, ok := r.BasicAuth(); !ok || user != "control-user" || password != "control-password" {
				t.Errorf("MediaMTX control credentials were not sent")
			}
			if err := json.NewDecoder(r.Body).Decode(&added); err != nil {
				t.Errorf("decode path configuration: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected MediaMTX API request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := &MediaMTXClient{baseURL: server.URL, user: "control-user", pass: "control-password", client: server.Client()}
	if err := client.ApplyPath(context.Background(), "stream-id", "rtsp://camera-user:secret@10.0.0.8/stream"); err != nil {
		t.Fatal(err)
	}
	if added["source"] != "rtsp://camera-user:secret@10.0.0.8/stream" || added["sourceOnDemand"] != true || added["sourceOnDemandCloseAfter"] != "10s" {
		t.Fatalf("unexpected MediaMTX path configuration: %v", added)
	}
}
