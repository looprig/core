package v1_test

import (
	"testing"

	sessionwire "github.com/looprig/core/sessionwire/v1"
)

// These assignments are a downstream-consumer compile check for the initial
// sessionwire/v1 public surface. This test deliberately imports no other Looprig
// package.
var (
	_ = sessionwire.TenantID("tenant-1")
	_ = sessionwire.SessionID("session-1")
	_ = sessionwire.AgentID("agent-1")
	_ = sessionwire.HostID("host-1")
	_ = sessionwire.CommandID("command-1")
	_ = sessionwire.EventID("event-1")
	_ = sessionwire.GateID("gate-1")

	_ sessionwire.WireVersion = sessionwire.CurrentWireVersion
	_                         = sessionwire.WireVersion(1)
)

func TestCurrentWireVersion(t *testing.T) {
	t.Parallel()
	if got, want := sessionwire.CurrentWireVersion, sessionwire.WireVersion(1); got != want {
		t.Errorf("CurrentWireVersion = %d, want %d", got, want)
	}
}
