package app

import (
	"testing"
	"time"

	"github.com/diandian921/sofarpc-mcp/internal/appconfig"
	"github.com/diandian921/sofarpc-mcp/internal/direct"
)

// direct must not import appconfig (arch boundary), so the two default
// timeout constants live apart; this test keeps them from drifting.
func TestDirectDefaultTimeoutMatchesConfigDefault(t *testing.T) {
	want := time.Duration(appconfig.DefaultServerTimeoutMS) * time.Millisecond
	if direct.DefaultTimeout != want {
		t.Fatalf("direct.DefaultTimeout = %v, want %v (appconfig.DefaultServerTimeoutMS)", direct.DefaultTimeout, want)
	}
}
