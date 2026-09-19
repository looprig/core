package v1_test

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sessionwire "github.com/looprig/core/sessionwire/v1"
)

// hostLinkEndpointGoldens is testdata/hostlink_endpoint_goldens.json. Every
// accepted endpoint in it is the base plus the exact path that host v0.2.1's
// real router (links.handler() mounted as Service.Routes() mounts it) was
// dialled with and resolved to the same tenant; every refusal records what that
// router answered. The table was produced by running Host, not by applying the
// rule under test, so it is an oracle for the derivation rather than a copy of
// it.
type hostLinkEndpointGoldens struct {
	Provenance string `json:"provenance"`
	Base       string `json:"base"`
	Rows       []struct {
		ID               string `json:"id"`
		TenantHex        string `json:"tenant_hex"`
		RouterStatus     int    `json:"router_status"`
		RouterSameTenant bool   `json:"router_same_tenant"`
		Endpoint         string `json:"endpoint"`
		Refusal          string `json:"refusal"`
		EndpointBytes    int    `json:"endpoint_bytes"`
	} `json:"rows"`
}

func loadHostLinkEndpointGoldens(t *testing.T) hostLinkEndpointGoldens {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "hostlink_endpoint_goldens.json"))
	if err != nil {
		t.Fatalf("read goldens: %v", err)
	}
	var goldens hostLinkEndpointGoldens
	if err := json.Unmarshal(data, &goldens); err != nil {
		t.Fatalf("decode goldens: %v", err)
	}
	return goldens
}

func goldenTenant(t *testing.T, tenantHex string) sessionwire.TenantID {
	t.Helper()
	raw, err := hex.DecodeString(tenantHex)
	if err != nil {
		t.Fatalf("decode tenant hex %q: %v", tenantHex, err)
	}
	return sessionwire.TenantID(raw)
}

func TestHostLinkEndpointMatchesHostRouterGoldens(t *testing.T) {
	t.Parallel()

	goldens := loadHostLinkEndpointGoldens(t)
	counts := map[string]int{}
	for _, row := range goldens.Rows {
		t.Run(row.ID, func(t *testing.T) {
			tenant := goldenTenant(t, row.TenantHex)
			got, err := sessionwire.HostLinkEndpoint(sessionwire.InternalEndpoint(goldens.Base), tenant)
			if row.Refusal == "" {
				if err != nil {
					t.Fatalf("HostLinkEndpoint refused a tenant Host routes (router %d): %v", row.RouterStatus, err)
				}
				if string(got) != row.Endpoint {
					t.Fatalf("HostLinkEndpoint = %q, want Host-routed %q", got, row.Endpoint)
				}
				if err := got.Validate(); err != nil {
					t.Fatalf("derived endpoint fails InternalEndpoint.Validate: %v", err)
				}
				return
			}
			assertHostLinkEndpointRefusal(t, got, err, sessionwire.HostLinkEndpointReason(row.Refusal))
		})
		reason := row.Refusal
		if reason == "" {
			reason = "accepted"
		}
		counts[reason]++
	}

	// The table must keep exercising every outcome; a trimmed golden must not
	// quietly turn this into a loop over the easy rows.
	for reason, minimum := range map[string]int{
		"accepted":          40,
		"unroutable_tenant": 9,
		"too_long":          3,
		"invalid_tenant":    2,
	} {
		if counts[reason] < minimum {
			t.Errorf("goldens carry %d %s rows, want at least %d", counts[reason], reason, minimum)
		}
	}
}

// TestHostLinkEndpointGoldensCoverTheControllerProbeList pins the probe list
// the D2.1 controller gates ran against the same router, so a regenerated
// golden cannot drop the tenants that found the hazards.
func TestHostLinkEndpointGoldensCoverTheControllerProbeList(t *testing.T) {
	t.Parallel()

	goldens := loadHostLinkEndpointGoldens(t)
	have := map[string]string{}
	for _, row := range goldens.Rows {
		have[row.ID] = row.Refusal
	}
	want := map[string]string{
		"dot": "unroutable_tenant", "dotdot": "unroutable_tenant", "slash": "unroutable_tenant",
		"slash-suffix": "unroutable_tenant", "slash-mid": "unroutable_tenant",
		"lit-%2F": "", "lit-a%2Fb": "", "lit-%2e": "", "lit-%2e%2e": "", "lit-%2E%2E": "",
		"percent": "", "lit-%zz": "", "question": "", "hash": "", "space": "", "e-acute": "",
		"cjk": "", "emoji": "", "kelvin": "", "division-slash": "", "fullwidth-dots": "",
		"nul": "", "tab": "", "lf": "", "cr": "", "del": "", "nel": "", "crlf-inject": "",
		"backslash-mid": "", "dotdot-backslash": "", "dotdotdot": "", "semicolon": "", "plus": "",
		"userinfo-like": "", "ipv6-like": "",
		"ascii-223": "", "ascii-224": "too_long", "eacute37-plus1": "", "eacute37-plus2": "too_long",
		"ascii-256-core-max": "too_long", "ascii-257-core-invalid": "invalid_tenant", "invalid-utf8": "invalid_tenant",
	}
	for id, refusal := range want {
		got, ok := have[id]
		if !ok {
			t.Errorf("goldens lack probe tenant %q", id)
			continue
		}
		if got != refusal {
			t.Errorf("golden %q refusal = %q, want %q", id, got, refusal)
		}
	}
}

func assertHostLinkEndpointRefusal(t *testing.T, got sessionwire.InternalEndpoint, err error, want sessionwire.HostLinkEndpointReason) {
	t.Helper()
	if err == nil {
		t.Fatalf("HostLinkEndpoint = %q, want refusal %s", got, want)
	}
	if got != "" {
		t.Errorf("refused HostLinkEndpoint returned %q, want empty", got)
	}
	var endpointErr *sessionwire.HostLinkEndpointError
	if !errors.As(err, &endpointErr) {
		t.Fatalf("error %T %v is not a *HostLinkEndpointError", err, err)
	}
	if endpointErr.Reason != want {
		t.Fatalf("Reason = %q, want %q (err: %v)", endpointErr.Reason, want, err)
	}
}

func TestHostLinkEndpointBase(t *testing.T) {
	t.Parallel()

	const tenant = sessionwire.TenantID("tenant-a")
	accepted := []struct {
		name string
		base string
		want string
	}{
		{"bare authority", "ws://host.internal:7443", "ws://host.internal:7443/hostlink/tenant-a"},
		{"one trailing slash", "ws://host.internal:7443/", "ws://host.internal:7443/hostlink/tenant-a"},
		{"wss", "wss://host.internal", "wss://host.internal/hostlink/tenant-a"},
		{"ipv6 literal", "ws://[::1]:7443", "ws://[::1]:7443/hostlink/tenant-a"},
		{"scheme spelled verbatim", "WS://host.internal:7443", "WS://host.internal:7443/hostlink/tenant-a"},
	}
	for _, tt := range accepted {
		t.Run("accepts "+tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := sessionwire.HostLinkEndpoint(sessionwire.InternalEndpoint(tt.base), tenant)
			if err != nil {
				t.Fatalf("HostLinkEndpoint(%q) refused: %v", tt.base, err)
			}
			if string(got) != tt.want {
				t.Fatalf("HostLinkEndpoint(%q) = %q, want %q", tt.base, got, tt.want)
			}
		})
	}

	refused := []struct {
		name string
		base string
		want sessionwire.HostLinkEndpointReason
	}{
		{"empty", "", sessionwire.HostLinkEndpointReasonInvalidBase},
		{"http scheme", "http://host.internal:7443", sessionwire.HostLinkEndpointReasonInvalidBase},
		{"query", "ws://host.internal:7443?token=x", sessionwire.HostLinkEndpointReasonInvalidBase},
		{"userinfo", "ws://u:p@host.internal:7443", sessionwire.HostLinkEndpointReasonInvalidBase},
		{"no host", "ws:///hostlink/tenant-a", sessionwire.HostLinkEndpointReasonInvalidBase},
		{"v0.2.1 per-tenant endpoint", "ws://host.internal:7443/hostlink/tenant-a", sessionwire.HostLinkEndpointReasonBaseNamesTenant},
		{"hostlink prefix only", "ws://host.internal:7443/hostlink/", sessionwire.HostLinkEndpointReasonBaseNamesTenant},
		{"hostlink without slash", "ws://host.internal:7443/hostlink", sessionwire.HostLinkEndpointReasonBaseNamesTenant},
		{"hostlink tenant with trailing slash", "ws://host.internal:7443/hostlink/tenant-a/", sessionwire.HostLinkEndpointReasonBaseNamesTenant},
		{"escaped hostlink segment", "ws://host.internal:7443/%68ostlink/tenant-a", sessionwire.HostLinkEndpointReasonBaseNamesTenant},
		{"other path", "ws://host.internal:7443/prefix", sessionwire.HostLinkEndpointReasonBaseNotBare},
		{"path under a prefix", "ws://host.internal:7443/prefix/hostlink/tenant-a", sessionwire.HostLinkEndpointReasonBaseNotBare},
		{"two trailing slashes", "ws://host.internal:7443//", sessionwire.HostLinkEndpointReasonBaseNotBare},
		{"empty fragment marker", "ws://host.internal:7443#", sessionwire.HostLinkEndpointReasonBaseNotBare},
		{"slash then empty fragment marker", "ws://host.internal:7443/#", sessionwire.HostLinkEndpointReasonBaseNotBare},
	}
	for _, tt := range refused {
		t.Run("refuses "+tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := sessionwire.HostLinkEndpoint(sessionwire.InternalEndpoint(tt.base), tenant)
			assertHostLinkEndpointRefusal(t, got, err, tt.want)
		})
	}
}

// TestHostLinkEndpointEmptyFragmentBaseIsAValidEndpoint is the control for the one base
// spelling InternalEndpoint.Validate accepts but string concatenation would
// break: net/url drops an EMPTY fragment, so "ws://h#" validates, and appending
// the path to it would put the whole path inside the fragment.
func TestHostLinkEndpointEmptyFragmentBaseIsAValidEndpoint(t *testing.T) {
	t.Parallel()

	if err := sessionwire.InternalEndpoint("ws://host.internal:7443#").Validate(); err != nil {
		t.Fatalf("control: InternalEndpoint.Validate refuses an empty fragment marker (%v); the refusal test is then not needed", err)
	}
}

func TestHostLinkEndpointRefusalPrecedenceAndCauses(t *testing.T) {
	t.Parallel()

	t.Run("an invalid base is reported before an invalid tenant", func(t *testing.T) {
		t.Parallel()
		got, err := sessionwire.HostLinkEndpoint("", "")
		assertHostLinkEndpointRefusal(t, got, err, sessionwire.HostLinkEndpointReasonInvalidBase)
		var cause *sessionwire.RequestValidationError
		if !errors.As(err, &cause) || cause.Code != sessionwire.RequestValidationCodeMissingField || cause.Field != "internal_endpoint" {
			t.Fatalf("cause = %#v, want InternalEndpoint.Validate's missing internal_endpoint", cause)
		}
	})
	t.Run("a base naming a tenant is reported before an unroutable tenant", func(t *testing.T) {
		t.Parallel()
		got, err := sessionwire.HostLinkEndpoint("ws://h/hostlink/x", ".")
		assertHostLinkEndpointRefusal(t, got, err, sessionwire.HostLinkEndpointReasonBaseNamesTenant)
	})
	t.Run("an invalid tenant carries Core's identity code", func(t *testing.T) {
		t.Parallel()
		for tenant, code := range map[sessionwire.TenantID]sessionwire.IDValidationCode{
			"": sessionwire.IDValidationCodeEmpty,
			sessionwire.TenantID(strings.Repeat("a", 257)): sessionwire.IDValidationCodeTooLong,
			"\xff": sessionwire.IDValidationCodeInvalidUTF8,
		} {
			got, err := sessionwire.HostLinkEndpoint("ws://h", tenant)
			assertHostLinkEndpointRefusal(t, got, err, sessionwire.HostLinkEndpointReasonInvalidTenant)
			var cause *sessionwire.IDValidationError
			if !errors.As(err, &cause) || cause.Code != code {
				t.Fatalf("cause = %#v, want IDValidationError %s", cause, code)
			}
		}
	})
	t.Run("an invalid tenant is reported before an overlong endpoint", func(t *testing.T) {
		t.Parallel()
		got, err := sessionwire.HostLinkEndpoint("ws://h", sessionwire.TenantID(strings.Repeat("a", 257)))
		assertHostLinkEndpointRefusal(t, got, err, sessionwire.HostLinkEndpointReasonInvalidTenant)
	})
	t.Run("refusals without a lower cause unwrap to nil", func(t *testing.T) {
		t.Parallel()
		for _, call := range []struct {
			base   sessionwire.InternalEndpoint
			tenant sessionwire.TenantID
		}{
			{"ws://h/hostlink/x", "t"},
			{"ws://h/p", "t"},
			{"ws://h", ".."},
			{"ws://h", sessionwire.TenantID(strings.Repeat("a", 250))},
		} {
			_, err := sessionwire.HostLinkEndpoint(call.base, call.tenant)
			if errors.Unwrap(err) != nil {
				t.Errorf("HostLinkEndpoint(%q, …) unwraps to %v, want nil", call.base, errors.Unwrap(err))
			}
		}
	})
}

// TestHostLinkEndpointErrorNamesNoValue keeps tenant and endpoint values out of
// error text, as every other sessionwire/v1 error does: an identifier is a
// value, and a log line must not become a place tenants leak.
func TestHostLinkEndpointErrorNamesNoValue(t *testing.T) {
	t.Parallel()

	const marker = "zz-marker-zz"
	for _, call := range []struct {
		base   sessionwire.InternalEndpoint
		tenant sessionwire.TenantID
	}{
		{"ws://" + marker, marker + "/"},
		{"ws://" + marker + "/hostlink/" + marker, marker},
		{"ws://" + marker + "/" + marker, marker},
		{"ws://" + marker, sessionwire.TenantID(marker + strings.Repeat("a", 240))},
		{"http://" + marker, marker},
		{"ws://" + marker, sessionwire.TenantID(strings.Repeat(marker, 30))},
	} {
		_, err := sessionwire.HostLinkEndpoint(call.base, call.tenant)
		if err == nil {
			t.Fatalf("HostLinkEndpoint(%q, …) accepted; the probe needs a refusal", call.base)
		}
		if strings.Contains(err.Error(), marker) {
			t.Errorf("error text %q echoes a caller value", err.Error())
		}
	}
	for _, reason := range []sessionwire.HostLinkEndpointReason{
		sessionwire.HostLinkEndpointReasonInvalidBase,
		sessionwire.HostLinkEndpointReasonBaseNamesTenant,
		sessionwire.HostLinkEndpointReasonBaseNotBare,
		sessionwire.HostLinkEndpointReasonInvalidTenant,
		sessionwire.HostLinkEndpointReasonUnroutableTenant,
		sessionwire.HostLinkEndpointReasonTooLong,
	} {
		text := (&sessionwire.HostLinkEndpointError{Reason: reason}).Error()
		if text != "sessionwire/v1: cannot derive HostLink endpoint: "+string(reason) {
			t.Errorf("Error() = %q", text)
		}
	}
}

// TestHostLinkEndpointReasonStrings pins the reason vocabulary by value: a
// caller branches on these strings, so renaming one is a breaking change.
func TestHostLinkEndpointReasonStrings(t *testing.T) {
	t.Parallel()

	for got, want := range map[sessionwire.HostLinkEndpointReason]string{
		sessionwire.HostLinkEndpointReasonInvalidBase:      "invalid_base",
		sessionwire.HostLinkEndpointReasonBaseNamesTenant:  "base_names_tenant",
		sessionwire.HostLinkEndpointReasonBaseNotBare:      "base_not_bare",
		sessionwire.HostLinkEndpointReasonInvalidTenant:    "invalid_tenant",
		sessionwire.HostLinkEndpointReasonUnroutableTenant: "unroutable_tenant",
		sessionwire.HostLinkEndpointReasonTooLong:          "too_long",
	} {
		if string(got) != want {
			t.Errorf("reason %q, want %q", got, want)
		}
	}
	if sessionwire.HostLinkPathPrefix != "/hostlink/" {
		t.Errorf("HostLinkPathPrefix = %q, want host v0.2.1's %q", sessionwire.HostLinkPathPrefix, "/hostlink/")
	}
}

// TestHostLinkEndpointLengthBoundary pins the MaxIDBytes edge at a base length
// other than the goldens', so the limit is the whole endpoint's rather than a
// tenant-length constant that happens to match one base.
func TestHostLinkEndpointLengthBoundary(t *testing.T) {
	t.Parallel()

	const base = "wss://x" // 7 bytes + 10 for "/hostlink/" leaves 239 for the escaped tenant
	fits, err := sessionwire.HostLinkEndpoint(base, sessionwire.TenantID(strings.Repeat("a", 239)))
	if err != nil {
		t.Fatalf("a %d-byte endpoint was refused: %v", sessionwire.MaxIDBytes, err)
	}
	if len(fits) != sessionwire.MaxIDBytes {
		t.Fatalf("len = %d, want exactly %d", len(fits), sessionwire.MaxIDBytes)
	}
	got, err := sessionwire.HostLinkEndpoint(base, sessionwire.TenantID(strings.Repeat("a", 240)))
	assertHostLinkEndpointRefusal(t, got, err, sessionwire.HostLinkEndpointReasonTooLong)

	// The limit counts ESCAPED bytes: 79 × "é" is 158 tenant bytes but 474
	// endpoint bytes, and a tenant whose raw length fits is still refused.
	got, err = sessionwire.HostLinkEndpoint(base, sessionwire.TenantID(strings.Repeat("é", 79)))
	assertHostLinkEndpointRefusal(t, got, err, sessionwire.HostLinkEndpointReasonTooLong)
	// 39 × "é" escapes to 234 bytes: 7 + 10 + 234 = 251, accepted.
	if _, err := sessionwire.HostLinkEndpoint(base, sessionwire.TenantID(strings.Repeat("é", 39))); err != nil {
		t.Fatalf("a 251-byte multibyte endpoint was refused: %v", err)
	}
}

// hostV021Router is a VERBATIM copy of how host v0.2.1 routes a HostLink
// request: Service.Routes() mounts the links handler on an http.ServeMux at
// "/hostlink/" (host compose.go:482-483), and links.handler() takes the tenant
// from request.URL.Path with tenantFromPath (internal/compose/links.go:246-252)
// and validates it with Core's TenantID.Validate (links.go:96) before building
// a transport. The goldens are the authority; this copy lets the fuzz target
// ask the same router about arbitrary tenants. It answers 200 with the tenant
// it resolved as the body.
func hostV021Router() http.Handler {
	tenantFromPath := func(path string) (string, bool) {
		rest, found := strings.CutPrefix(path, "/hostlink/")
		if !found || rest == "" || strings.Contains(rest, "/") {
			return "", false
		}
		return rest, true
	}
	routes := http.NewServeMux()
	routes.Handle("/hostlink/", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		name, ok := tenantFromPath(request.URL.Path)
		if !ok {
			http.Error(writer, "HostLink requires a tenant path segment", http.StatusNotFound)
			return
		}
		if err := sessionwire.TenantID(name).Validate(); err != nil {
			http.Error(writer, "HostLink requires a valid tenant", http.StatusBadRequest)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(name))
	}))
	return routes
}

// routeThroughHostV021 sends endpoint's request-target through the router the
// way a dialler would: net/url parses the endpoint, RequestURI re-escapes the
// path, and http.ReadRequest (inside httptest.NewRequest) decodes it as a
// server does.
func routeThroughHostV021(router http.Handler, endpoint string) (int, string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return 0, "", err
	}
	request := httptest.NewRequest(http.MethodGet, parsed.RequestURI(), nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder.Code, recorder.Body.String(), nil
}

func TestHostV021RouterCopyAgreesWithGoldens(t *testing.T) {
	t.Parallel()

	goldens := loadHostLinkEndpointGoldens(t)
	router := hostV021Router()
	for _, row := range goldens.Rows {
		tenant := string(goldenTenant(t, row.TenantHex))
		code, resolved, err := routeThroughHostV021(router, goldens.Base+"/hostlink/"+url.PathEscape(tenant))
		if err != nil {
			t.Fatalf("%s: %v", row.ID, err)
		}
		if code != row.RouterStatus || (resolved == tenant) != row.RouterSameTenant {
			t.Errorf("%s: router copy answered %d same=%v, real host v0.2.1 answered %d same=%v",
				row.ID, code, resolved == tenant, row.RouterStatus, row.RouterSameTenant)
		}
	}
}

// FuzzHostLinkEndpointRoutesToItsTenant is the derivation's property over
// arbitrary tenants: an accepted endpoint validates, parses to exactly
// "/hostlink/" + tenant on the base's authority, and routes through Host's
// router to that same tenant; a tenant refused as unroutable is one that
// router does not resolve. Together they make the unroutable refusal exact
// rather than merely safe.
func FuzzHostLinkEndpointRoutesToItsTenant(f *testing.F) {
	goldens := hostLinkEndpointGoldensForFuzz(f)
	for _, row := range goldens.Rows {
		raw, err := hex.DecodeString(row.TenantHex)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(string(raw))
	}
	router := hostV021Router()
	const base = "ws://host.internal:7443"
	f.Fuzz(func(t *testing.T, tenant string) {
		got, err := sessionwire.HostLinkEndpoint(base, sessionwire.TenantID(tenant))
		if err != nil {
			var endpointErr *sessionwire.HostLinkEndpointError
			if !errors.As(err, &endpointErr) {
				t.Fatalf("untyped refusal %T: %v", err, err)
			}
			switch endpointErr.Reason {
			case sessionwire.HostLinkEndpointReasonInvalidTenant:
				if sessionwire.TenantID(tenant).Validate() == nil {
					t.Fatalf("refused a Core-valid tenant as invalid")
				}
			case sessionwire.HostLinkEndpointReasonUnroutableTenant:
				code, resolved, routeErr := routeThroughHostV021(router, base+"/hostlink/"+url.PathEscape(tenant))
				if routeErr == nil && code == http.StatusOK && resolved == tenant {
					t.Fatalf("refused as unroutable a tenant Host's router resolves")
				}
			case sessionwire.HostLinkEndpointReasonTooLong:
				if len(base)+len("/hostlink/")+len(url.PathEscape(tenant)) <= sessionwire.MaxIDBytes {
					t.Fatalf("refused as too long an endpoint that fits")
				}
			default:
				t.Fatalf("unexpected reason %q", endpointErr.Reason)
			}
			return
		}
		if err := got.Validate(); err != nil {
			t.Fatalf("derived endpoint fails Validate: %v", err)
		}
		parsed, err := url.Parse(string(got))
		if err != nil {
			t.Fatalf("derived endpoint does not parse: %v", err)
		}
		if parsed.Host != "host.internal:7443" || parsed.Path != "/hostlink/"+tenant {
			t.Fatalf("derived endpoint parses to host %q path %q", parsed.Host, parsed.Path)
		}
		code, resolved, err := routeThroughHostV021(router, string(got))
		if err != nil || code != http.StatusOK || resolved != tenant {
			t.Fatalf("Host's router answered %d resolving %q (err %v), want 200 and the tenant", code, resolved, err)
		}
	})
}

func hostLinkEndpointGoldensForFuzz(f *testing.F) hostLinkEndpointGoldens {
	f.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "hostlink_endpoint_goldens.json"))
	if err != nil {
		f.Fatalf("read goldens: %v", err)
	}
	var goldens hostLinkEndpointGoldens
	if err := json.Unmarshal(data, &goldens); err != nil {
		f.Fatalf("decode goldens: %v", err)
	}
	return goldens
}

func ExampleHostLinkEndpoint() {
	// A pooled Host advertises one BASE; each tenant's link is derived from it.
	base := sessionwire.InternalEndpoint("ws://host-7.pool.internal:7443")
	for _, tenant := range []sessionwire.TenantID{"acme", "a b", "a/b", "."} {
		endpoint, err := sessionwire.HostLinkEndpoint(base, tenant)
		if err != nil {
			fmt.Println(err)
			continue
		}
		fmt.Println(endpoint)
	}
	// Output:
	// ws://host-7.pool.internal:7443/hostlink/acme
	// ws://host-7.pool.internal:7443/hostlink/a%20b
	// sessionwire/v1: cannot derive HostLink endpoint: unroutable_tenant
	// sessionwire/v1: cannot derive HostLink endpoint: unroutable_tenant
}
