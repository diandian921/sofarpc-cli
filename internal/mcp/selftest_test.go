package mcp

import (
	"testing"
)

func TestServerSelfTestPasses(t *testing.T) {
	t.Setenv("SOFARPC_HOME", t.TempDir())
	s := &Server{BuildVersion: "test"}
	if err := s.SelfTest(); err != nil {
		t.Fatalf("SelfTest should pass on a healthy server: %v", err)
	}
}
