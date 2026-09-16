package v1

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
)

// v080VersionNegotiationResponseMembers is the member list this record declared
// at core v0.8.0 (hostlink.go:23, `versionNegotiationResponseMembers =
// []string{"version"}`). The v0.8.0 UnmarshalJSON was exactly
// decodeContractFields + decodeHostLinkVersion + captureExtensions over this
// list, so driving those same unexported helpers with the old list reproduces
// the v0.8.0 code path without needing the old module in the build.
var v080VersionNegotiationResponseMembers = []string{"version"}

func TestVersionNegotiationResponseMemberListNamesHostLinkMethods(t *testing.T) {
	t.Parallel()

	// The member list is a set (the marshaller sorts), so compare it as one.
	got := slices.Clone(versionNegotiationResponseMembers)
	slices.Sort(got)
	want := []string{"hostlink_methods", "version"}
	if !slices.Equal(got, want) {
		t.Fatalf("versionNegotiationResponseMembers = %v, want %v", versionNegotiationResponseMembers, want)
	}
}

// TestV080DecoderRetainsHostLinkMethodsAsExtension is the compatibility proof
// in the other direction: a v0.8.0 Factory that receives a v0.9.0 Host's reply
// still decodes it (the response decoder is tolerant), keeps the new member in
// AdditionalFields, and re-emits it unchanged if it proxies the reply.
func TestV080DecoderRetainsHostLinkMethodsAsExtension(t *testing.T) {
	t.Parallel()

	const body = `{"hostlink_methods":["hostlink.attach"],"version":1}`

	fields, err := decodeContractFields([]byte(body), v080VersionNegotiationResponseMembers...)
	if err != nil {
		t.Fatalf("v0.8.0 decodeContractFields refused the v0.9.0 reply: %v", err)
	}
	version, err := decodeHostLinkVersion(fields)
	if err != nil {
		t.Fatalf("v0.8.0 decodeHostLinkVersion: %v", err)
	}
	if version != CurrentWireVersion {
		t.Errorf("v0.8.0 decoder read version %d, want %d", version, CurrentWireVersion)
	}
	old := VersionNegotiationResponse{
		Version:    version,
		extensions: captureExtensions(fields, v080VersionNegotiationResponseMembers...),
	}
	extra := old.AdditionalFields()
	raw, ok := extra["hostlink_methods"]
	if !ok {
		t.Fatalf("v0.8.0 decoder did not retain hostlink_methods as an extension: %v", extra)
	}
	if want := `["hostlink.attach"]`; !bytes.Equal(raw, []byte(want)) {
		t.Errorf("retained extension = %s, want %s", raw, want)
	}
	// A v0.8.0 proxy re-emits the reply with the capability intact, so a v0.9.0
	// Factory behind a v0.8.0 proxy still sees it.
	reencoded, err := marshalResponseFields(v080VersionNegotiationResponseMembers, map[string]json.RawMessage{"version": json.RawMessage(`1`)}, old.extensions)
	if err != nil {
		t.Fatalf("v0.8.0 marshalResponseFields: %v", err)
	}
	if !bytes.Equal(reencoded, []byte(body)) {
		t.Errorf("v0.8.0 re-encode = %s, want %s", reencoded, body)
	}

	// The v0.9.0 decoder, by contrast, owns the member and captures nothing.
	var current VersionNegotiationResponse
	if err := json.Unmarshal([]byte(body), &current); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if current.extensions.fields != nil {
		t.Errorf("v0.9.0 decoder captured %v as extensions; hostlink_methods must be excluded from capture", current.AdditionalFields())
	}
	if !current.Supports(HostLinkMethodAttach) {
		t.Error("v0.9.0 decoder did not surface the advertised method")
	}
}
