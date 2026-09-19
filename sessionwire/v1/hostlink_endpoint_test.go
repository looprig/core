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
			assertHostLinkEndpointRefusal(t, got, err, sessionwire.HostLinkEndpointCode(row.Refusal))
			// endpoint_bytes is recorded only for too_long rows, where it is the
			// length the Host-routed spelling would have had. It is recomputed
			// here from the base and the dialled spelling, not from the rule.
			wantBytes := 0
			if row.Refusal == string(sessionwire.HostLinkEndpointCodeTooLong) {
				wantBytes = len(goldens.Base) + len("/hostlink/") + len(url.PathEscape(string(tenant)))
				if wantBytes <= sessionwire.MaxIDBytes {
					t.Fatalf("too_long golden is %d bytes, which fits", wantBytes)
				}
			}
			if row.EndpointBytes != wantBytes {
				t.Fatalf("golden endpoint_bytes = %d, want %d", row.EndpointBytes, wantBytes)
			}
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

func assertHostLinkEndpointRefusal(t *testing.T, got sessionwire.InternalEndpoint, err error, want sessionwire.HostLinkEndpointCode) {
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
	if endpointErr.Code != want {
		t.Fatalf("Code = %q, want %q (err: %v)", endpointErr.Code, want, err)
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
		want sessionwire.HostLinkEndpointCode
	}{
		{"empty", "", sessionwire.HostLinkEndpointCodeInvalidBase},
		{"http scheme", "http://host.internal:7443", sessionwire.HostLinkEndpointCodeInvalidBase},
		{"query", "ws://host.internal:7443?token=x", sessionwire.HostLinkEndpointCodeInvalidBase},
		{"userinfo", "ws://u:p@host.internal:7443", sessionwire.HostLinkEndpointCodeInvalidBase},
		{"no host", "ws:///hostlink/tenant-a", sessionwire.HostLinkEndpointCodeInvalidBase},
		{"v0.2.1 per-tenant endpoint", "ws://host.internal:7443/hostlink/tenant-a", sessionwire.HostLinkEndpointCodeBaseNamesTenant},
		{"hostlink prefix only", "ws://host.internal:7443/hostlink/", sessionwire.HostLinkEndpointCodeBaseNamesTenant},
		{"hostlink without slash", "ws://host.internal:7443/hostlink", sessionwire.HostLinkEndpointCodeBaseNamesTenant},
		{"hostlink tenant with trailing slash", "ws://host.internal:7443/hostlink/tenant-a/", sessionwire.HostLinkEndpointCodeBaseNamesTenant},
		{"escaped hostlink segment", "ws://host.internal:7443/%68ostlink/tenant-a", sessionwire.HostLinkEndpointCodeBaseNamesTenant},
		{"other path", "ws://host.internal:7443/prefix", sessionwire.HostLinkEndpointCodeBaseNotBare},
		// A segment that merely begins with "hostlink" is some other path, not
		// a tenant's link: the prefix is matched with its trailing '/'.
		{"segment beginning hostlink", "ws://host.internal:7443/hostlinks", sessionwire.HostLinkEndpointCodeBaseNotBare},
		{"segment beginning hostlink with child", "ws://host.internal:7443/hostlinkx/tenant-a", sessionwire.HostLinkEndpointCodeBaseNotBare},
		// Host's router is case-sensitive: host v0.2.1 answers /HOSTLINK/x and
		// /Hostlink/x with 404, so such a path names no tenant. It is some
		// other path and refused as not bare, matched case-exactly.
		{"upper-case hostlink segment", "ws://host.internal:7443/HOSTLINK/tenant-a", sessionwire.HostLinkEndpointCodeBaseNotBare},
		{"mixed-case hostlink segment", "ws://host.internal:7443/Hostlink/tenant-a", sessionwire.HostLinkEndpointCodeBaseNotBare},
		{"upper-case bare hostlink", "ws://host.internal:7443/HOSTLINK", sessionwire.HostLinkEndpointCodeBaseNotBare},
		{"ingress path prefix before a tenant", "ws://host.internal:7443/p/hostlink/tenant-a", sessionwire.HostLinkEndpointCodeBaseNotBare},
		{"path under a prefix", "ws://host.internal:7443/prefix/hostlink/tenant-a", sessionwire.HostLinkEndpointCodeBaseNotBare},
		{"two trailing slashes", "ws://host.internal:7443//", sessionwire.HostLinkEndpointCodeBaseNotBare},
		{"empty fragment marker", "ws://host.internal:7443#", sessionwire.HostLinkEndpointCodeBaseNotBare},
		{"slash then empty fragment marker", "ws://host.internal:7443/#", sessionwire.HostLinkEndpointCodeBaseNotBare},
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
		assertHostLinkEndpointRefusal(t, got, err, sessionwire.HostLinkEndpointCodeInvalidBase)
		var cause *sessionwire.RequestValidationError
		if !errors.As(err, &cause) || cause.Code != sessionwire.RequestValidationCodeMissingField || cause.Field != "internal_endpoint" {
			t.Fatalf("cause = %#v, want InternalEndpoint.Validate's missing internal_endpoint", cause)
		}
	})
	t.Run("a base naming a tenant is reported before an unroutable tenant", func(t *testing.T) {
		t.Parallel()
		got, err := sessionwire.HostLinkEndpoint("ws://h/hostlink/x", ".")
		assertHostLinkEndpointRefusal(t, got, err, sessionwire.HostLinkEndpointCodeBaseNamesTenant)
	})
	t.Run("an invalid tenant carries Core's identity code", func(t *testing.T) {
		t.Parallel()
		for tenant, code := range map[sessionwire.TenantID]sessionwire.IDValidationCode{
			"": sessionwire.IDValidationCodeEmpty,
			sessionwire.TenantID(strings.Repeat("a", 257)): sessionwire.IDValidationCodeTooLong,
			"\xff": sessionwire.IDValidationCodeInvalidUTF8,
		} {
			got, err := sessionwire.HostLinkEndpoint("ws://h", tenant)
			assertHostLinkEndpointRefusal(t, got, err, sessionwire.HostLinkEndpointCodeInvalidTenant)
			var cause *sessionwire.IDValidationError
			if !errors.As(err, &cause) || cause.Code != code {
				t.Fatalf("cause = %#v, want IDValidationError %s", cause, code)
			}
		}
	})
	t.Run("an invalid tenant is reported before an overlong endpoint", func(t *testing.T) {
		t.Parallel()
		got, err := sessionwire.HostLinkEndpoint("ws://h", sessionwire.TenantID(strings.Repeat("a", 257)))
		assertHostLinkEndpointRefusal(t, got, err, sessionwire.HostLinkEndpointCodeInvalidTenant)
	})
	t.Run("an invalid tenant is reported before an unroutable one", func(t *testing.T) {
		t.Parallel()
		for tenant, code := range map[sessionwire.TenantID]sessionwire.IDValidationCode{
			"\xff/": sessionwire.IDValidationCodeInvalidUTF8,
			"/\xff": sessionwire.IDValidationCodeInvalidUTF8,
			sessionwire.TenantID(strings.Repeat("/", 257)): sessionwire.IDValidationCodeTooLong,
		} {
			got, err := sessionwire.HostLinkEndpoint("ws://h", tenant)
			assertHostLinkEndpointRefusal(t, got, err, sessionwire.HostLinkEndpointCodeInvalidTenant)
			var cause *sessionwire.IDValidationError
			if !errors.As(err, &cause) || cause.Code != code {
				t.Fatalf("cause = %#v, want IDValidationError %s", cause, code)
			}
		}
	})
	t.Run("an unroutable tenant is reported before an overlong endpoint", func(t *testing.T) {
		t.Parallel()
		for _, tenant := range []sessionwire.TenantID{
			sessionwire.TenantID(strings.Repeat("/", 100)),
			sessionwire.TenantID("a/" + strings.Repeat("a", 250)),
		} {
			got, err := sessionwire.HostLinkEndpoint("ws://h", tenant)
			assertHostLinkEndpointRefusal(t, got, err, sessionwire.HostLinkEndpointCodeUnroutableTenant)
		}
	})
	t.Run("error text names only the code, even with a cause", func(t *testing.T) {
		t.Parallel()
		for _, call := range []struct {
			base   sessionwire.InternalEndpoint
			tenant sessionwire.TenantID
			code   sessionwire.HostLinkEndpointCode
		}{
			{"", "t", sessionwire.HostLinkEndpointCodeInvalidBase},
			{"http://h", "t", sessionwire.HostLinkEndpointCodeInvalidBase},
			{"ws://h", "", sessionwire.HostLinkEndpointCodeInvalidTenant},
			{"ws://h", "\xff", sessionwire.HostLinkEndpointCodeInvalidTenant},
			{"ws://h/hostlink/x", "t", sessionwire.HostLinkEndpointCodeBaseNamesTenant},
			{"ws://h/p", "t", sessionwire.HostLinkEndpointCodeBaseNotBare},
			{"ws://h", ".", sessionwire.HostLinkEndpointCodeUnroutableTenant},
			{"ws://h", sessionwire.TenantID(strings.Repeat("a", 250)), sessionwire.HostLinkEndpointCodeTooLong},
		} {
			_, err := sessionwire.HostLinkEndpoint(call.base, call.tenant)
			if err == nil || err.Error() != "sessionwire/v1: cannot derive HostLink endpoint: "+string(call.code) {
				t.Errorf("HostLinkEndpoint(%q, …) error text = %v, want exactly the %s code text", call.base, err, call.code)
			}
		}
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
	for _, reason := range []sessionwire.HostLinkEndpointCode{
		sessionwire.HostLinkEndpointCodeInvalidBase,
		sessionwire.HostLinkEndpointCodeBaseNamesTenant,
		sessionwire.HostLinkEndpointCodeBaseNotBare,
		sessionwire.HostLinkEndpointCodeInvalidTenant,
		sessionwire.HostLinkEndpointCodeUnroutableTenant,
		sessionwire.HostLinkEndpointCodeTooLong,
	} {
		text := (&sessionwire.HostLinkEndpointError{Code: reason}).Error()
		if text != "sessionwire/v1: cannot derive HostLink endpoint: "+string(reason) {
			t.Errorf("Error() = %q", text)
		}
	}
}

// TestHostLinkEndpointCodeStrings pins the reason vocabulary by value: a
// caller branches on these strings, so renaming one is a breaking change.
func TestHostLinkEndpointCodeStrings(t *testing.T) {
	t.Parallel()

	for got, want := range map[sessionwire.HostLinkEndpointCode]string{
		sessionwire.HostLinkEndpointCodeInvalidBase:      "invalid_base",
		sessionwire.HostLinkEndpointCodeBaseNamesTenant:  "base_names_tenant",
		sessionwire.HostLinkEndpointCodeBaseNotBare:      "base_not_bare",
		sessionwire.HostLinkEndpointCodeInvalidTenant:    "invalid_tenant",
		sessionwire.HostLinkEndpointCodeUnroutableTenant: "unroutable_tenant",
		sessionwire.HostLinkEndpointCodeTooLong:          "too_long",
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
	assertHostLinkEndpointRefusal(t, got, err, sessionwire.HostLinkEndpointCodeTooLong)

	// The limit counts ESCAPED bytes: 79 × "é" is 158 tenant bytes but 474
	// endpoint bytes, and a tenant whose raw length fits is still refused.
	got, err = sessionwire.HostLinkEndpoint(base, sessionwire.TenantID(strings.Repeat("é", 79)))
	assertHostLinkEndpointRefusal(t, got, err, sessionwire.HostLinkEndpointCodeTooLong)
	// 39 × "é" escapes to 234 bytes: 7 + 10 + 234 = 251, accepted.
	if _, err := sessionwire.HostLinkEndpoint(base, sessionwire.TenantID(strings.Repeat("é", 39))); err != nil {
		t.Fatalf("a 251-byte multibyte endpoint was refused: %v", err)
	}

	// The trimmed base is what is counted: "wss://x/" contributes 7 bytes, not
	// 8, so 239 tenant bytes still fit exactly.
	fits, err = sessionwire.HostLinkEndpoint("wss://x/", sessionwire.TenantID(strings.Repeat("a", 239)))
	if err != nil {
		t.Fatalf("a %d-byte endpoint from a slash-terminated base was refused: %v", sessionwire.MaxIDBytes, err)
	}
	if len(fits) != sessionwire.MaxIDBytes {
		t.Fatalf("len = %d, want exactly %d", len(fits), sessionwire.MaxIDBytes)
	}
	got, err = sessionwire.HostLinkEndpoint("wss://x/", sessionwire.TenantID(strings.Repeat("a", 240)))
	assertHostLinkEndpointRefusal(t, got, err, sessionwire.HostLinkEndpointCodeTooLong)

	// InternalEndpoint.Validate accepts a non-ASCII host, and the limit counts
	// BYTES: "ws://é" is 6 runes but 7 bytes, so 239 tenant bytes fit exactly
	// and 240 do not, although 240 would fit if runes were counted.
	const nonASCII = "ws://é"
	if err := sessionwire.InternalEndpoint(nonASCII).Validate(); err != nil {
		t.Fatalf("control: Validate refuses a non-ASCII host (%v); the byte-count row is then moot", err)
	}
	fits, err = sessionwire.HostLinkEndpoint(nonASCII, sessionwire.TenantID(strings.Repeat("a", 239)))
	if err != nil || len(fits) != sessionwire.MaxIDBytes || fits.Validate() != nil {
		t.Fatalf("non-ASCII base, 239 bytes: got %d bytes, err %v", len(fits), err)
	}
	got, err = sessionwire.HostLinkEndpoint(nonASCII, sessionwire.TenantID(strings.Repeat("a", 240)))
	assertHostLinkEndpointRefusal(t, got, err, sessionwire.HostLinkEndpointCodeTooLong)
}

// hostV021Router is a ROUTING-EQUIVALENT REDUCTION, not a verbatim copy, of
// how host v0.2.1 routes a HostLink request. Service.Routes() mounts the links
// handler on an http.ServeMux at "/hostlink/" (host compose.go:482-483);
// links.handler() takes the tenant from request.URL.Path with tenantFromPath
// (internal/compose/links.go:246-252) and validates it with Core's
// TenantID.Validate (links.go:96) before building a transport. Kept verbatim:
// the mux mounting and tenantFromPath. Dropped: the sibling /metrics, /readyz
// and /healthz routes, resolve's 503 answers (tenant limit, stopped Host), and
// the hand-off to the Centrifuge transport; this answers 200 with the tenant it
// resolved as the body instead. The prefix is inlined rather than read from
// Host's constant. A 527k-exec differential fuzz against the real handler
// found no divergence on /hostlink/ paths (CODEX_REVIEW_CORE_0C2F959_QUALITY).
//
// It is checked here only against goldens recorded from v0.2.1, so it cannot
// notice a HOST routing change: that guard belongs in Host, which must test its
// real Routes() against HostLinkEndpoint.
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

// bareBaseForOracle says whether base is one HostLinkEndpoint may build on:
// it validates, and carries no path beyond one '/' and no '#'. It is the fuzz
// oracle's own statement of the base rule.
func bareBaseForOracle(base string) (host string, bare bool) {
	if sessionwire.InternalEndpoint(base).Validate() != nil || strings.Contains(base, "#") {
		return "", false
	}
	parsed, err := url.Parse(base)
	if err != nil || (parsed.Path != "" && parsed.Path != "/") {
		return "", false
	}
	return parsed.Host, true
}

// FuzzHostLinkEndpointRoutesToItsTenant is the derivation's property over
// arbitrary bases and tenants. An accepted endpoint validates, fits in
// MaxIDBytes BYTES, begins with the base (one trailing '/' dropped), parses to
// exactly "/hostlink/" + tenant on the base's authority, and routes through
// Host's router to that same tenant. Every refusal is justified by an oracle
// that does not call HostLinkEndpoint, and in the documented order: a base
// refusal only for a base that is not bare; invalid_tenant only for a bare
// base and a tenant Core refuses; unroutable only for a valid tenant the router
// does not resolve; too_long only for a valid tenant the router resolves whose
// endpoint does not fit.
func FuzzHostLinkEndpointRoutesToItsTenant(f *testing.F) {
	goldens := hostLinkEndpointGoldensForFuzz(f)
	bases := []string{
		goldens.Base, "wss://x", "wss://x/", "ws://é", "ws://[::1]:7443", "WS://h",
		"ws://h/hostlink/x", "ws://h/HOSTLINK/x", "ws://h#", "ws://h//", "", "http://h",
	}
	for _, row := range goldens.Rows {
		raw, err := hex.DecodeString(row.TenantHex)
		if err != nil {
			f.Fatal(err)
		}
		for _, base := range bases {
			f.Add(base, string(raw))
		}
	}
	f.Add("wss://x/", strings.Repeat("a", 239))
	f.Add("ws://é", strings.Repeat("a", 240))
	f.Add("ws://h", "\xff/")
	f.Add("ws://h", strings.Repeat("/", 100))
	router := hostV021Router()
	f.Fuzz(func(t *testing.T, base, tenant string) {
		got, err := sessionwire.HostLinkEndpoint(sessionwire.InternalEndpoint(base), sessionwire.TenantID(tenant))
		host, bare := bareBaseForOracle(base)
		routes := func() bool {
			code, resolved, routeErr := routeThroughHostV021(router, "ws://h/hostlink/"+url.PathEscape(tenant))
			return routeErr == nil && code == http.StatusOK && resolved == tenant
		}
		length := len(strings.TrimSuffix(base, "/")) + len("/hostlink/") + len(url.PathEscape(tenant))
		if err != nil {
			var endpointErr *sessionwire.HostLinkEndpointError
			if !errors.As(err, &endpointErr) {
				t.Fatalf("untyped refusal %T: %v", err, err)
			}
			if got != "" {
				t.Fatalf("refusal returned %q", got)
			}
			switch endpointErr.Code {
			case sessionwire.HostLinkEndpointCodeInvalidBase:
				if sessionwire.InternalEndpoint(base).Validate() == nil {
					t.Fatalf("refused a valid base as invalid")
				}
			case sessionwire.HostLinkEndpointCodeBaseNamesTenant, sessionwire.HostLinkEndpointCodeBaseNotBare:
				if bare || sessionwire.InternalEndpoint(base).Validate() != nil {
					t.Fatalf("refused base with %s, but oracle bare=%v", endpointErr.Code, bare)
				}
			case sessionwire.HostLinkEndpointCodeInvalidTenant:
				if !bare || sessionwire.TenantID(tenant).Validate() == nil {
					t.Fatalf("invalid_tenant with bare=%v and a Core-valid tenant", bare)
				}
			case sessionwire.HostLinkEndpointCodeUnroutableTenant:
				if !bare || sessionwire.TenantID(tenant).Validate() != nil || routes() {
					t.Fatalf("unroutable_tenant for a tenant that is invalid or that Host's router resolves")
				}
			case sessionwire.HostLinkEndpointCodeTooLong:
				if !bare || sessionwire.TenantID(tenant).Validate() != nil || !routes() || length <= sessionwire.MaxIDBytes {
					t.Fatalf("too_long for %d bytes (bare=%v)", length, bare)
				}
			default:
				t.Fatalf("unexpected code %q", endpointErr.Code)
			}
			return
		}
		if !bare {
			t.Fatalf("accepted a base the oracle refuses")
		}
		if len(got) > sessionwire.MaxIDBytes {
			t.Fatalf("derived endpoint is %d bytes", len(got))
		}
		if err := got.Validate(); err != nil {
			t.Fatalf("derived endpoint fails Validate: %v", err)
		}
		if !strings.HasPrefix(string(got), strings.TrimSuffix(base, "/")+"/hostlink/") {
			t.Fatalf("derived endpoint %q does not extend the base", got)
		}
		parsed, err := url.Parse(string(got))
		if err != nil {
			t.Fatalf("derived endpoint does not parse: %v", err)
		}
		if parsed.Host != host || parsed.Path != "/hostlink/"+tenant {
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
