package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const offlineExamplesCommand = "GOWORK=off GOCACHE=/tmp/looprig-core-docs-gocache go test ./examples/..."

type examplesManifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	Repository    string `json:"repository"`
	ProofSources  []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Path string `json:"path"`
	} `json:"proofSources"`
	Examples []struct {
		ID             string            `json:"id"`
		Ecosystem      string            `json:"ecosystem"`
		Owner          string            `json:"owner"`
		SourcePath     string            `json:"sourcePath"`
		Availability   string            `json:"availability"`
		Versions       map[string]string `json:"versions"`
		OfflineCommand string            `json:"offlineCommand"`
		Assertion      string            `json:"assertion"`
		WorkflowPath   string            `json:"workflowPath"`
		JobID          string            `json:"jobId"`
		Cleanup        string            `json:"cleanup"`
		LiveGate       any               `json:"liveGate"`
		ProofIDs       []string          `json:"proofIds"`
	} `json:"examples"`
}

func TestRunnableExamplesExist(t *testing.T) {
	t.Parallel()

	paths := []string{
		"examples/content/example_test.go",
		"examples/streaming/example_test.go",
		"examples/usage/example_test.go",
		"examples/logging/example_test.go",
		"examples/uuid/example_test.go",
	}
	for _, path := range paths {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			if _, err := os.Stat(filepath.Join("..", path)); err != nil {
				t.Fatalf("runnable example %q: %v", path, err)
			}
		})
	}
}

func TestDocsExamplesArtifacts(t *testing.T) {
	t.Parallel()

	manifest, err := os.ReadFile(filepath.Join("..", "testdata/docs/examples.json"))
	if err != nil {
		t.Fatalf("read examples manifest: %v", err)
	}
	var decoded examplesManifest
	if err := json.Unmarshal(manifest, &decoded); err != nil {
		t.Fatalf("decode examples manifest: %v", err)
	}
	if decoded.SchemaVersion != 1 || decoded.Repository != "core" {
		t.Fatalf("manifest identity = schema %d repository %q", decoded.SchemaVersion, decoded.Repository)
	}
	proofs := make(map[string]bool, len(decoded.ProofSources))
	for _, proof := range decoded.ProofSources {
		if proof.ID == "" || proof.Type == "" || proof.Path == "" {
			t.Errorf("incomplete proof source: %#v", proof)
		}
		proofs[proof.ID] = true
	}
	if len(decoded.Examples) != 5 {
		t.Fatalf("manifest examples = %d, want 5", len(decoded.Examples))
	}
	seen := make(map[string]bool, len(decoded.Examples))
	for _, example := range decoded.Examples {
		if seen[example.ID] {
			t.Errorf("duplicate example ID %q", example.ID)
		}
		seen[example.ID] = true
		if example.Ecosystem != "go" || example.Owner != "core" || example.Availability != "source-workspace" {
			t.Errorf("example %q classification is incorrect", example.ID)
		}
		if example.Versions["github.com/looprig/core"] != "source-workspace" || len(example.Versions) != 1 {
			t.Errorf("example %q versions = %#v", example.ID, example.Versions)
		}
		if example.OfflineCommand != offlineExamplesCommand {
			t.Errorf("example %q offlineCommand = %q", example.ID, example.OfflineCommand)
		}
		if example.SourcePath == "" || example.Assertion == "" || example.WorkflowPath != ".github/workflows/docs-examples.yml" || example.JobID != "docs-examples" || example.Cleanup == "" || example.LiveGate != nil {
			t.Errorf("example %q has incomplete execution metadata", example.ID)
		}
		for _, proofID := range example.ProofIDs {
			if !proofs[proofID] {
				t.Errorf("example %q references unknown proof %q", example.ID, proofID)
			}
		}
		if len(example.ProofIDs) < 2 {
			t.Errorf("example %q proofIds = %v, want source and test proofs", example.ID, example.ProofIDs)
		}
	}

	workflow, err := os.ReadFile(filepath.Join("..", ".github/workflows/docs-examples.yml"))
	if err != nil {
		t.Fatalf("read docs examples workflow: %v", err)
	}
	for _, literal := range []string{
		"docs-examples:",
		offlineExamplesCommand,
		"GOWORK=off GOCACHE=/tmp/looprig-core-docs-gocache make check",
		"GOWORK=off GOCACHE=/tmp/looprig-core-docs-gocache go test -race ./...",
	} {
		if !strings.Contains(string(workflow), literal) {
			t.Errorf("workflow does not contain %q", literal)
		}
	}
}
