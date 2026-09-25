package v1_test

import (
	"bytes"
	"encoding/json"
	"testing"

	sessionwire "github.com/looprig/core/sessionwire/v1"
)

// FuzzInputRequestMessageMembers checks that accepted input requests validate
// and their canonical encoding is a fixed point.
func FuzzInputRequestMessageMembers(f *testing.F) {
	for _, seed := range []string{
		`{"version":1,"command_id":"c","session_id":"s","blocks":[{"type":"text","text":"x"}]}`,
		`{"version":1,"command_id":"c","session_id":"s","blocks":[{"type":"text","text":"x"}],"metadata":{"space":"family & friends"},"principal":{"tenant":"t","subject":"u","kind":"actor"}}`,
		`{"version":1,"command_id":"c","session_id":"s","blocks":[{"type":"text","text":"x"}],"metadata":{"a":"\u2028"}}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var first sessionwire.InputRequest
		if err := json.Unmarshal(data, &first); err != nil {
			return
		}
		if err := first.Validate(); err != nil {
			t.Fatalf("decoded request fails Validate: %v", err)
		}
		encoded, err := json.Marshal(first)
		if err != nil {
			t.Fatalf("marshal decoded request: %v", err)
		}
		var second sessionwire.InputRequest
		if err := json.Unmarshal(encoded, &second); err != nil {
			t.Fatalf("re-decode %s: %v", encoded, err)
		}
		again, err := json.Marshal(second)
		if err != nil {
			t.Fatalf("re-marshal: %v", err)
		}
		if !bytes.Equal(encoded, again) {
			t.Fatalf("encoding is not a fixed point\nfirst: %s\nsecond: %s", encoded, again)
		}
	})
}
