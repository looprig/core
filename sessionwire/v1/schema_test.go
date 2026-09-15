package v1_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	v1SchemaDir = "schema"
	// v1FixtureDir contains reviewed wire records. testdata/command_id_vectors.json
	// intentionally remains outside it because it is algorithm test data, not a
	// transport record and therefore has no record schema.
	v1FixtureDir        = "testdata/fixtures"
	v1SchemaIDPrefix    = "https://looprig.dev/sessionwire/v1/"
	v1JSONSchemaDialect = "https://json-schema.org/draft/2020-12/schema"
)

// v1ContractSchema is intentionally the small JSON Schema view that the
// standard library can validate without adding a runtime schema dependency.
// Fixtures also round-trip through the authoritative Core record decoders in
// fixtures_test.go, which covers record-specific semantic constraints.
type v1ContractSchema struct {
	Dialect              string                     `json:"$schema"`
	ID                   string                     `json:"$id"`
	Type                 string                     `json:"type"`
	Required             []string                   `json:"required"`
	Properties           map[string]json.RawMessage `json:"properties"`
	AdditionalProperties json.RawMessage            `json:"additionalProperties"`
}

func TestV1SchemaFilesParseAndDeclareV1IDs(t *testing.T) {
	t.Parallel()

	files := v1SchemaFiles(t)
	for _, path := range files {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()

			schema := readV1Schema(t, path)
			stem := strings.TrimSuffix(filepath.Base(path), ".schema.json")
			if got, want := schema.Dialect, v1JSONSchemaDialect; got != want {
				t.Errorf("$schema = %q, want %q", got, want)
			}
			if got, want := schema.ID, v1SchemaIDPrefix+stem+".schema.json"; got != want {
				t.Errorf("$id = %q, want explicit V1 ID %q", got, want)
			}
			if !strings.Contains(schema.ID, "/v1/") {
				t.Errorf("$id %q is not versioned", schema.ID)
			}
			if schema.Type != "object" {
				t.Errorf("type = %q, want object", schema.Type)
			}
			if len(schema.Properties) == 0 {
				t.Error("schema has no declared top-level properties")
			}
			if len(schema.AdditionalProperties) == 0 {
				t.Error("schema does not declare additionalProperties policy")
			}
			// A required member that is not also a declared property makes the
			// schema unsatisfiable, so no fixture could ever validate against it.
			for _, name := range schema.Required {
				if _, ok := schema.Properties[name]; !ok {
					t.Errorf("required member %q is not a declared property", name)
				}
			}
		})
	}
}

func TestV1SchemaAndFixtureStemsMatch(t *testing.T) {
	t.Parallel()

	schemaStems := make(map[string]struct{})
	for _, path := range v1SchemaFiles(t) {
		schemaStems[strings.TrimSuffix(filepath.Base(path), ".schema.json")] = struct{}{}
	}
	fixtureStems := make(map[string]struct{})
	for _, path := range v1FixtureFiles(t) {
		fixtureStems[strings.TrimSuffix(filepath.Base(path), ".json")] = struct{}{}
	}

	for stem := range schemaStems {
		if _, ok := fixtureStems[stem]; !ok {
			t.Errorf("schema %q has no matching fixture", stem)
		}
	}
	for stem := range fixtureStems {
		if _, ok := schemaStems[stem]; !ok {
			t.Errorf("fixture %q has no matching schema", stem)
		}
	}
}

func TestV1FixturesMatchSchemaShapeAndExtensionPolicy(t *testing.T) {
	t.Parallel()

	strict := map[string]bool{
		"create_request":                  true,
		"input_request":                   true,
		"interrupt_request":               true,
		"restore_request":                 true,
		"gate_response_request":           true,
		"object_reference":                true,
		"object_metadata":                 true,
		"hostlink_attach_request":         true,
		"hostlink_bind_request":           true,
		"hostlink_unbind_request":         true,
		"hostlink_command_delivery":       true,
		"hostlink_capacity_report":        true,
		"hostlink_registry_observation":   true,
		"hostlink_drain_request":          true,
		"hostlink_drain_observation":      true,
		"hostlink_error_epoch_mismatch":   true,
		"hostlink_error_runtime_mismatch": true,
		"version_negotiation_request":     true,
	}
	// Open public response schemas intentionally permit additive members. Keep
	// the few golden examples explicit here so an accidental misspelling of a
	// known field cannot hide behind additionalProperties: true.
	extensions := map[string]map[string]struct{}{
		"agent_capability_summary":      {"future_agent_hint": {}},
		"department_capability_summary": {"future_department_hint": {}},
		"error_envelope":                {"request_id": {}},
	}

	for _, fixturePath := range v1FixtureFiles(t) {
		fixturePath := fixturePath
		stem := strings.TrimSuffix(filepath.Base(fixturePath), ".json")
		t.Run(stem, func(t *testing.T) {
			t.Parallel()

			fixture := readV1JSONObject(t, fixturePath)
			schema := readV1Schema(t, filepath.Join(v1SchemaDir, stem+".schema.json"))
			var additionalProperties bool
			if err := json.Unmarshal(schema.AdditionalProperties, &additionalProperties); err != nil {
				t.Fatalf("decode additionalProperties: %v", err)
			}
			for name := range fixture {
				if _, known := schema.Properties[name]; known {
					continue
				}
				if _, extension := extensions[stem][name]; extension && additionalProperties {
					continue
				}
				if additionalProperties {
					t.Errorf("fixture member %q is not a declared property or reviewed additive extension", name)
				} else {
					t.Errorf("fixture member %q is not declared by closed schema", name)
				}
			}
			for _, name := range schema.Required {
				if _, ok := fixture[name]; !ok {
					t.Errorf("schema requires member %q missing from fixture", name)
				}
			}
			if got, want := additionalProperties, !strict[stem]; got != want {
				t.Errorf("additionalProperties = %t, want %t for %s", got, want, stem)
			}
		})
	}
}

func v1SchemaFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(v1SchemaDir, "*.schema.json"))
	if err != nil {
		t.Fatalf("glob V1 schemas: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no V1 schemas found in %s", v1SchemaDir)
	}
	slices.Sort(files)
	return files
}

func v1FixtureFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(v1FixtureDir, "*.json"))
	if err != nil {
		t.Fatalf("glob V1 fixtures: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no V1 fixtures found in %s", v1FixtureDir)
	}
	slices.Sort(files)
	return files
}

func readV1Schema(t *testing.T, path string) v1ContractSchema {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read schema %s: %v", path, err)
	}
	var schema v1ContractSchema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("parse schema %s: %v", path, err)
	}
	return schema
}

func readV1JSONObject(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("parse fixture %s: %v", path, err)
	}
	if object == nil {
		t.Fatalf("fixture %s is not a JSON object", path)
	}
	return object
}
