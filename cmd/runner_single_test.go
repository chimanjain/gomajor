package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chimanjain/gomajor/checker"
)

func TestRunCheckerTable(t *testing.T) {
	tests := []struct {
		name         string
		goModContent string
		checkAll     bool
		maxProbe     int
		jsonOutput   bool
		httpHandler  func(rw http.ResponseWriter, req *http.Request)
		wantErr      bool
	}{
		{
			name: "NoDirectDeps",
			goModContent: `module example.com/test

go 1.21

require github.com/google/uuid v1.6.0 // indirect
`,
			checkAll: false,
			maxProbe: 0,
		},
		{
			name:         "EmptyMod",
			goModContent: "module example.com/empty\n\ngo 1.21\n",
			checkAll:     false,
			maxProbe:     0,
		},
		{
			name: "WithUpdatesMock",
			goModContent: `module example.com/test

go 1.21

require github.com/foo/bar v1.0.0
`,
			checkAll: false,
			maxProbe: 2,
			httpHandler: func(rw http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/github.com/foo/bar/v2/@latest" {
					_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v2.0.0"})
				} else {
					rw.WriteHeader(http.StatusNotFound)
				}
			},
		},
		{
			name: "AllDeps",
			goModContent: `module example.com/test

go 1.21

require (
	github.com/foo/bar v1.0.0
	github.com/foo/baz v1.0.0 // indirect
)
`,
			checkAll: true,
			maxProbe: 1,
			httpHandler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(http.StatusNotFound)
			},
		},
		{
			name: "Json",
			goModContent: `module example.com/test
require github.com/foo/bar v1.0.0
`,
			checkAll:   false,
			maxProbe:   2,
			jsonOutput: true,
			httpHandler: func(rw http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/github.com/foo/bar/v2/@latest" {
					_ = json.NewEncoder(rw).Encode(map[string]string{"Version": "v2.0.0"})
				} else {
					rw.WriteHeader(http.StatusNotFound)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			p := writeModFile(t, dir, tt.goModContent)

			var client *checker.Client
			if tt.httpHandler != nil {
				server := httptest.NewServer(http.HandlerFunc(tt.httpHandler))
				defer server.Close()
				client = &checker.Client{
					HTTPClient: server.Client(),
					ProxyBase:  server.URL,
				}
			} else {
				client = checker.DefaultClient()
			}

			testConfig := &Config{
				ModFilePath: p,
				CheckAll:    tt.checkAll,
				MaxProbe:    tt.maxProbe,
				JsonOutput:  tt.jsonOutput,
				Client:      client,
			}

			err := runCheckerWithConfig(testConfig, true, false, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("runCheckerWithConfig returned error: %v, wantErr: %v", err, tt.wantErr)
			}
		})
	}
}
