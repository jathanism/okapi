package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression: previously `main` only dispatched to makeOpenApiFromSource when
// the source started with "https://". Anything else (http://, file://, raw
// bytes via OKAPI_OPENAPI_SOURCE, etc.) silently fell through, leaving api
// nil and producing a confusing nil-pointer panic in api.Endpoints().
//
// resolveSpecSource must accept any non-empty source verbatim and let the
// downstream spec loader decide how to interpret it.
func TestResolveSpecSource(t *testing.T) {
	tests := []struct {
		name       string
		sourceFlag string
		hostFlag   string
		envSource  string
		want       string
		wantErr    bool
	}{
		{
			name:       "http source flag is accepted (regression)",
			sourceFlag: "http://localhost:8080/openapi.json",
			want:       "http://localhost:8080/openapi.json",
		},
		{
			name:       "https source flag is accepted",
			sourceFlag: "https://api.example.com/openapi.json",
			want:       "https://api.example.com/openapi.json",
		},
		{
			name:       "file source flag is accepted",
			sourceFlag: "file:///tmp/openapi.yaml",
			want:       "file:///tmp/openapi.yaml",
		},
		{
			name:       "embed sentinel passes through",
			sourceFlag: "embed",
			want:       "embed",
		},
		{
			name:       "source flag wins over env and host",
			sourceFlag: "http://flag/openapi.json",
			hostFlag:   "host.example.com",
			envSource:  "http://env/openapi.json",
			want:       "http://flag/openapi.json",
		},
		{
			name:      "env source is used when flag is empty (regression: http via env)",
			envSource: "http://localhost:8080/openapi.json",
			want:      "http://localhost:8080/openapi.json",
		},
		{
			name:     "host is synthesized into https URL when nothing else is set",
			hostFlag: "api.example.com",
			want:     "https://api.example.com/api/schema/",
		},
		{
			name:    "no inputs returns an error",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveSpecSource(tt.sourceFlag, tt.hostFlag, tt.envSource)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got source %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// Regression: writeEmbeddedSchema previously called os.OpenFile directly on
// <schemaDir>/openapi.yaml. With O_CREATE that only creates the file, not the
// parent directory, so a clean checkout (no ./cli/schema dir) fataled with
// "no such file or directory".
func TestWriteEmbeddedSchemaCreatesMissingDir(t *testing.T) {
	root := t.TempDir()
	// Two levels of missing parents: exercises MkdirAll, not just Mkdir.
	schemaDir := filepath.Join(root, "nested", "schema")
	data := []byte("openapi: 3.0.0\n")

	path, err := writeEmbeddedSchema(schemaDir, data)
	if err != nil {
		t.Fatalf("writeEmbeddedSchema returned error: %v", err)
	}

	wantPath := filepath.Join(schemaDir, "openapi.yaml")
	if path != wantPath {
		t.Fatalf("returned path %q, want %q", path, wantPath)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read written schema: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("schema contents = %q, want %q", got, data)
	}
}

// writeEmbeddedSchema must overwrite an existing file (gen runs repeatedly),
// not append to it.
func TestWriteEmbeddedSchemaTruncatesExisting(t *testing.T) {
	schemaDir := t.TempDir()
	path := filepath.Join(schemaDir, "openapi.yaml")
	if err := os.WriteFile(path, []byte("stale stale stale stale"), 0644); err != nil {
		t.Fatalf("seeding existing file failed: %v", err)
	}

	fresh := []byte("openapi: 3.1.0\n")
	if _, err := writeEmbeddedSchema(schemaDir, fresh); err != nil {
		t.Fatalf("writeEmbeddedSchema returned error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read written schema: %v", err)
	}
	if !strings.EqualFold(string(got), string(fresh)) {
		t.Fatalf("schema contents = %q, want %q (file was not truncated)", got, fresh)
	}
}
