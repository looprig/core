package v1_test

import (
	"encoding/json"
	"strings"
	"testing"

	sessionwire "github.com/looprig/core/sessionwire/v1"
)

func TestV1PrincipalAndMetadataSchemasAgreeWithDecoders(t *testing.T) {
	t.Parallel()
	const inputHead = `{"version":1,"command_id":"c","session_id":"s","blocks":[{"type":"text","text":"x"}]`
	input := func(body string) error { var r sessionwire.InputRequest; return json.Unmarshal([]byte(body), &r) }
	interrupt := func(body string) error { var r sessionwire.InterruptRequest; return json.Unmarshal([]byte(body), &r) }
	tests := []struct {
		name, stem, body string
		decode           func(string) error
	}{
		{"principal unknown member", "input_request_principal", inputHead + `,"principal":{"tenant":"t","subject":"u","kind":"actor","name":"Alex"}}`, input},
		{"principal unknown kind", "input_request_principal", inputHead + `,"principal":{"tenant":"t","subject":"u","kind":"robot"}}`, input},
		{"principal missing subject", "input_request_principal", inputHead + `,"principal":{"tenant":"t","kind":"actor"}}`, input},
		{"principal null", "input_request_principal", inputHead + `,"principal":null}`, input},
		{"metadata non-string value", "input_request_metadata", inputHead + `,"metadata":{"space":1}}`, input},
		{"metadata null", "input_request_metadata", inputHead + `,"metadata":null}`, input},
		{"metadata on interrupt", "interrupt_request_principal", `{"version":1,"command_id":"c","session_id":"s","metadata":{"a":"b"},"principal":{"tenant":"t","subject":"u","kind":"actor"}}`, interrupt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			schema := readV1SchemaDocument(t, v1SchemaDir+"/"+tt.stem+".schema.json")
			decoder := json.NewDecoder(strings.NewReader(tt.body))
			decoder.UseNumber()
			var instance any
			if err := decoder.Decode(&instance); err != nil {
				t.Fatal(err)
			}
			problems, err := validateV1Instance(schema, instance)
			if err != nil {
				t.Fatal(err)
			}
			if len(problems) == 0 {
				t.Error("schema accepted body Core refuses")
			}
			if tt.decode(tt.body) == nil {
				t.Error("Core decoder accepted body")
			}
		})
	}
}
