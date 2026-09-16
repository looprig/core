package v1_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	sessionwire "github.com/looprig/core/sessionwire/v1"
)

// The v0.8.0 reply shape. A Host that predates the capability signal answers
// exactly this, so a v0.9.0 Factory must read it as "no advertised methods"
// rather than refuse it.
const v080VersionNegotiationResponse = `{"version":1}`

func TestVersionNegotiationResponseV080ShapeDecodesWithNoMethods(t *testing.T) {
	t.Parallel()

	var decoded sessionwire.VersionNegotiationResponse
	if err := json.Unmarshal([]byte(v080VersionNegotiationResponse), &decoded); err != nil {
		t.Fatalf("Unmarshal(v0.8.0 reply): %v", err)
	}
	if decoded.Version != sessionwire.CurrentWireVersion {
		t.Errorf("Version = %d, want %d", decoded.Version, sessionwire.CurrentWireVersion)
	}
	if decoded.HostLinkMethods() != nil {
		t.Errorf("HostLinkMethods() = %#v, want nil for an absent member", decoded.HostLinkMethods())
	}
	for _, method := range []string{
		sessionwire.HostLinkMethodAttach,
		sessionwire.HostLinkMethodBind,
		sessionwire.HostLinkMethodUnbind,
		sessionwire.HostLinkMethodDrain,
		sessionwire.HostLinkMethodDrainStatus,
		"",
	} {
		if decoded.Supports(method) {
			t.Errorf("Supports(%q) = true on a reply that advertised nothing", method)
		}
	}
	if extra := decoded.AdditionalFields(); len(extra) != 0 {
		t.Errorf("AdditionalFields() = %v, want none", extra)
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Equal(encoded, []byte(v080VersionNegotiationResponse)) {
		t.Errorf("re-encoded v0.8.0 reply = %s, want byte-identical %s", encoded, v080VersionNegotiationResponse)
	}
}

func TestVersionNegotiationResponseHostLinkMethodsRoundTrip(t *testing.T) {
	t.Parallel()

	const body = `{"hostlink_methods":["hostlink.attach","hostlink.bind","hostlink.unbind"],"version":1}`
	var decoded sessionwire.VersionNegotiationResponse
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := []string{sessionwire.HostLinkMethodAttach, sessionwire.HostLinkMethodBind, sessionwire.HostLinkMethodUnbind}
	if !slices.Equal(decoded.HostLinkMethods(), want) {
		t.Errorf("HostLinkMethods() = %v, want %v in wire order", decoded.HostLinkMethods(), want)
	}
	for _, method := range want {
		if !decoded.Supports(method) {
			t.Errorf("Supports(%q) = false, want true", method)
		}
	}
	for _, method := range []string{sessionwire.HostLinkMethodDrain, sessionwire.HostLinkMethodDrainStatus, "hostlink.attac", "hostlink.attach ", "", "attach"} {
		if decoded.Supports(method) {
			t.Errorf("Supports(%q) = true, want exact-match false", method)
		}
	}
	// The member is a declared contract member now, not a retained extension:
	// a caller must find it in HostLinkMethods, never in AdditionalFields.
	if extra := decoded.AdditionalFields(); len(extra) != 0 {
		t.Errorf("AdditionalFields() = %v, want hostlink_methods NOT captured as an extension", extra)
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Equal(encoded, []byte(body)) {
		t.Errorf("round trip = %s, want %s", encoded, body)
	}

	// The form a Host builds after NegotiateVersion encodes to the same bytes:
	// the Host adds the set to the negotiated response.
	negotiated, err := sessionwire.NegotiateVersion(sessionwire.VersionNegotiationRequest{SupportedVersions: []sessionwire.WireVersion{sessionwire.CurrentWireVersion}})
	if err != nil {
		t.Fatalf("NegotiateVersion: %v", err)
	}
	negotiated = negotiated.WithHostLinkMethods(want...)
	fromStruct, err := json.Marshal(negotiated)
	if err != nil {
		t.Fatalf("Marshal(negotiated): %v", err)
	}
	if !bytes.Equal(fromStruct, []byte(body)) {
		t.Errorf("Marshal(negotiated) = %s, want %s", fromStruct, body)
	}
}

func TestVersionNegotiationResponseOmitsEmptyHostLinkMethods(t *testing.T) {
	t.Parallel()

	for name, methods := range map[string][]string{"nil": nil, "empty": {}} {
		encoded, err := json.Marshal(sessionwire.VersionNegotiationResponse{Version: sessionwire.CurrentWireVersion}.WithHostLinkMethods(methods...))
		if err != nil {
			t.Fatalf("Marshal(%s): %v", name, err)
		}
		if !bytes.Equal(encoded, []byte(v080VersionNegotiationResponse)) {
			t.Errorf("Marshal(%s) = %s, want the member omitted: %s", name, encoded, v080VersionNegotiationResponse)
		}
	}

	// An explicit empty array on the wire is "no advertised methods" and
	// canonicalises to the omitted form.
	var decoded sessionwire.VersionNegotiationResponse
	if err := json.Unmarshal([]byte(`{"version":1,"hostlink_methods":[]}`), &decoded); err != nil {
		t.Fatalf("Unmarshal(empty array): %v", err)
	}
	if len(decoded.HostLinkMethods()) != 0 || decoded.Supports(sessionwire.HostLinkMethodAttach) {
		t.Errorf("empty array decoded as %#v", decoded.HostLinkMethods())
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Equal(encoded, []byte(v080VersionNegotiationResponse)) {
		t.Errorf("empty array re-encoded as %s, want %s", encoded, v080VersionNegotiationResponse)
	}
}

// A method a v0.9.0 Factory has no constant for is tolerated and retained, not
// refused: refusing would make the very first record a newer Host sends fail
// the connect of every older Factory, which is the forward-compatibility break
// the tolerant response decoder exists to prevent. Retaining it (rather than
// dropping it) keeps a negotiation proxy from silently stripping a capability
// on re-encode, and makes Supports the truthful "the peer advertised this".
func TestVersionNegotiationResponseToleratesUnknownHostLinkMethod(t *testing.T) {
	t.Parallel()

	const body = `{"hostlink_methods":["hostlink.attach","hostlink.future_v030"],"version":1}`
	var decoded sessionwire.VersionNegotiationResponse
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("a method name this version does not know was refused: %v", err)
	}
	if !decoded.Supports(sessionwire.HostLinkMethodAttach) {
		t.Error("Supports(attach) = false; a known method next to an unknown one was lost")
	}
	if !decoded.Supports("hostlink.future_v030") {
		t.Error("Supports(unknown) = false; the advertised name was dropped rather than retained")
	}
	if extra := decoded.AdditionalFields(); len(extra) != 0 {
		t.Errorf("AdditionalFields() = %v, want the unknown NAME retained in HostLinkMethods, not as an extension", extra)
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Equal(encoded, []byte(body)) {
		t.Errorf("re-encode = %s, want the unknown method preserved: %s", encoded, body)
	}
}

func TestVersionNegotiationResponseRejectsMalformedHostLinkMethods(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{"null", `{"version":1,"hostlink_methods":null}`},
		{"not an array", `{"version":1,"hostlink_methods":"hostlink.attach"}`},
		{"object", `{"version":1,"hostlink_methods":{"hostlink.attach":true}}`},
		{"non-string element", `{"version":1,"hostlink_methods":[1]}`},
		{"null element", `{"version":1,"hostlink_methods":[null]}`},
		{"empty name", `{"version":1,"hostlink_methods":[""]}`},
		{"duplicate name", `{"version":1,"hostlink_methods":["hostlink.attach","hostlink.attach"]}`},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var decoded sessionwire.VersionNegotiationResponse
			err := json.Unmarshal([]byte(tt.body), &decoded)
			if err == nil {
				t.Fatalf("decoder accepted %s as %#v", tt.body, decoded)
			}
			assertValidationError(t, err, sessionwire.RequestValidationCodeInvalidField, "hostlink_methods")
		})
	}
	var decoded sessionwire.VersionNegotiationResponse
	assertValidationError(t, json.Unmarshal([]byte(`{"version":1,"hostlink_methods":["a"],"hostlink_methods":["b"]}`), &decoded), sessionwire.RequestValidationCodeDuplicateField, "hostlink_methods")
	// A malformed UTF-8 name fails the whole-body strict scan before any member
	// is read, so it reports as invalid JSON like every other record.
	assertValidationError(t, json.Unmarshal([]byte("{\"version\":1,\"hostlink_methods\":[\"hostlink.\xff\"]}"), &decoded), sessionwire.RequestValidationCodeInvalidJSON, "")

	// Validate and MarshalJSON enforce the same set rule as the decoder, so a
	// Host cannot emit a reply a Factory would refuse.
	for name, methods := range map[string][]string{
		"duplicate": {sessionwire.HostLinkMethodAttach, sessionwire.HostLinkMethodAttach},
		"empty":     {""},
	} {
		response := sessionwire.VersionNegotiationResponse{Version: sessionwire.CurrentWireVersion}.WithHostLinkMethods(methods...)
		assertValidationError(t, response.Validate(), sessionwire.RequestValidationCodeInvalidField, "hostlink_methods")
		if _, err := json.Marshal(response); err == nil {
			t.Errorf("Marshal(%s) succeeded on a set the decoder refuses", name)
		}
	}
}

// TestVersionNegotiationResponseSchemaAgreesWithDecoder holds the published
// schema's member and required sets to what the decoder enforces, so a
// generated consumer's view of the record cannot drift from Core's.
func TestVersionNegotiationResponseSchemaAgreesWithDecoder(t *testing.T) {
	t.Parallel()

	rawSchema, err := os.ReadFile(filepath.Join(v1SchemaDir, "version_negotiation_response.schema.json"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var schema struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(rawSchema, &schema); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	// The capability-carrying fixture is the one with every member present.
	fixture := readV1JSONObject(t, filepath.Join(v1FixtureDir, "version_negotiation_response_hostlink_methods.json"))

	var declared, present []string
	for name := range schema.Properties {
		declared = append(declared, name)
	}
	for name := range fixture {
		present = append(present, name)
	}
	slices.Sort(declared)
	slices.Sort(present)
	if want := []string{"hostlink_methods", "version"}; !slices.Equal(declared, want) {
		t.Fatalf("schema properties = %v, want %v", declared, want)
	}
	if !slices.Equal(declared, present) {
		t.Fatalf("schema properties %v != fixture members %v; the check below needs every member present", declared, present)
	}
	if want := []string{"version"}; !slices.Equal(schema.Required, want) {
		t.Errorf("schema required = %v, want %v: hostlink_methods must stay optional for v0.8.0 Hosts", schema.Required, want)
	}

	for _, name := range declared {
		members := make(map[string]json.RawMessage, len(fixture))
		for k, v := range fixture {
			if k != name {
				members[k] = v
			}
		}
		body, err := json.Marshal(members)
		if err != nil {
			t.Fatalf("marshal body without %s: %v", name, err)
		}
		var decoded sessionwire.VersionNegotiationResponse
		decodeErr := json.Unmarshal(body, &decoded)
		if slices.Contains(schema.Required, name) {
			if decodeErr == nil {
				t.Errorf("schema requires %q but the decoder accepts a body without it", name)
				continue
			}
			assertValidationError(t, decodeErr, sessionwire.RequestValidationCodeMissingField, name)
		} else if decodeErr != nil {
			t.Errorf("schema makes %q optional but the decoder refuses a body without it: %v", name, decodeErr)
		}
	}

	// The schema must NOT close the method names into an enum: the decoder
	// tolerates a name it does not know, and a generated consumer that trusted
	// a closed enum would refuse a newer Host's reply.
	var items struct {
		Items map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(schema.Properties["hostlink_methods"], &items); err != nil {
		t.Fatalf("decode hostlink_methods schema: %v", err)
	}
	if _, closed := items.Items["enum"]; closed {
		t.Error("schema closes hostlink_methods items with an enum; the decoder is deliberately open to future method names")
	}
	if _, closed := items.Items["const"]; closed {
		t.Error("schema closes hostlink_methods items with a const")
	}
}

// The connect framing. Both records travel BARE; the wrapped shape Factory
// v0.1.1 invented ({"version_negotiation":{...}}) is refused on both sides
// with the stable missing-field code, so neither module can drift back to it
// and still pass Core's own tests.
func TestHostLinkConnectFramingIsBare(t *testing.T) {
	t.Parallel()

	request := sessionwire.VersionNegotiationRequest{SupportedVersions: []sessionwire.WireVersion{sessionwire.CurrentWireVersion}}
	requestData, err := sessionwire.EncodeHostLinkConnectRequest(request)
	if err != nil {
		t.Fatalf("EncodeHostLinkConnectRequest: %v", err)
	}
	if want := `{"supported_versions":[1]}`; string(requestData) != want {
		t.Errorf("connect Data = %s, want bare %s", requestData, want)
	}
	decodedRequest, err := sessionwire.DecodeHostLinkConnectRequest(requestData)
	if err != nil {
		t.Fatalf("DecodeHostLinkConnectRequest: %v", err)
	}
	if !slices.Equal(decodedRequest.SupportedVersions, request.SupportedVersions) {
		t.Errorf("decoded request = %#v, want %#v", decodedRequest, request)
	}

	reply := sessionwire.VersionNegotiationResponse{Version: sessionwire.CurrentWireVersion}.WithHostLinkMethods(sessionwire.HostLinkMethodAttach)
	replyData, err := sessionwire.EncodeHostLinkConnectReply(reply)
	if err != nil {
		t.Fatalf("EncodeHostLinkConnectReply: %v", err)
	}
	if want := `{"hostlink_methods":["hostlink.attach"],"version":1}`; string(replyData) != want {
		t.Errorf("reply Data = %s, want bare %s", replyData, want)
	}
	decodedReply, err := sessionwire.DecodeHostLinkConnectReply(replyData)
	if err != nil {
		t.Fatalf("DecodeHostLinkConnectReply: %v", err)
	}
	if decodedReply.Version != sessionwire.CurrentWireVersion || !decodedReply.Supports(sessionwire.HostLinkMethodAttach) {
		t.Errorf("decoded reply = %#v", decodedReply)
	}

	// Host v0.1.0's actual reply is the bare v0.8.0 shape: accepted, no methods.
	hostV010, err := sessionwire.DecodeHostLinkConnectReply([]byte(v080VersionNegotiationResponse))
	if err != nil {
		t.Fatalf("DecodeHostLinkConnectReply(Host v0.1.0 reply): %v", err)
	}
	if hostV010.Supports(sessionwire.HostLinkMethodAttach) {
		t.Error("a Host that advertised nothing was read as supporting attach")
	}

	// The wrapped shapes are refused with the stable code and the member name
	// the wrapper hid, not accepted as version 0 or read as an extension.
	_, err = sessionwire.DecodeHostLinkConnectRequest([]byte(`{"version_negotiation":{"supported_versions":[1]}}`))
	assertValidationError(t, err, sessionwire.RequestValidationCodeUnknownField, "")
	_, err = sessionwire.DecodeHostLinkConnectReply([]byte(`{"version_negotiation":{"version":1}}`))
	assertValidationError(t, err, sessionwire.RequestValidationCodeMissingField, "version")

	// The encoders refuse what the decoders would refuse.
	if _, err := sessionwire.EncodeHostLinkConnectRequest(sessionwire.VersionNegotiationRequest{}); err == nil {
		t.Error("EncodeHostLinkConnectRequest accepted an empty offer")
	}
	if _, err := sessionwire.EncodeHostLinkConnectReply(sessionwire.VersionNegotiationResponse{}); err == nil {
		t.Error("EncodeHostLinkConnectReply accepted version 0")
	}
	if _, err := sessionwire.DecodeHostLinkConnectReply([]byte(`{"version":0}`)); err == nil {
		t.Error("DecodeHostLinkConnectReply accepted version 0")
	}
}

// TestVersionNegotiationResponseStaysComparable pins the property apidiff
// checks: v0.8.0's VersionNegotiationResponse was comparable, and adding the
// method set must not make `==` stop compiling for a consumer. Two replies that
// advertise nothing still compare equal, exactly as at v0.8.0.
func TestVersionNegotiationResponseStaysComparable(t *testing.T) {
	t.Parallel()

	var a, b sessionwire.VersionNegotiationResponse
	for _, target := range []*sessionwire.VersionNegotiationResponse{&a, &b} {
		if err := json.Unmarshal([]byte(v080VersionNegotiationResponse), target); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
	}
	if a != b {
		t.Errorf("two decodes of %s are unequal: %#v vs %#v", v080VersionNegotiationResponse, a, b)
	}
	if a == a.WithHostLinkMethods(sessionwire.HostLinkMethodAttach) {
		t.Error("a reply with an advertised method compares equal to one without")
	}
}

// The accessor and the setter both copy: a caller mutating what it passed in
// or what it got back cannot change what the reply advertises.
func TestVersionNegotiationResponseMethodSetIsDefensivelyCopied(t *testing.T) {
	t.Parallel()

	input := []string{sessionwire.HostLinkMethodAttach}
	reply := sessionwire.VersionNegotiationResponse{Version: sessionwire.CurrentWireVersion}.WithHostLinkMethods(input...)
	input[0] = sessionwire.HostLinkMethodDrain
	if !reply.Supports(sessionwire.HostLinkMethodAttach) || reply.Supports(sessionwire.HostLinkMethodDrain) {
		t.Errorf("mutating the setter's input changed the reply: %v", reply.HostLinkMethods())
	}
	returned := reply.HostLinkMethods()
	returned[0] = sessionwire.HostLinkMethodDrain
	if !reply.Supports(sessionwire.HostLinkMethodAttach) || reply.Supports(sessionwire.HostLinkMethodDrain) {
		t.Errorf("mutating the accessor's result changed the reply: %v", reply.HostLinkMethods())
	}
}
