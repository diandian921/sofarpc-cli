package proto

import "testing"

func TestNegotiateVersion(t *testing.T) {
	cases := []struct {
		name    string
		client  string
		want    string
		wantErr bool
	}{
		{"supported", "2025-06-18", "2025-06-18", false},
		{"older-supported", "2024-11-05", "2024-11-05", false},
		{"unsupported-degrades-to-latest", "1.0.0", "2025-11-25", false},
		{"missing-is-invalid", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := NegotiateVersion(c.client)
			if c.wantErr {
				if err == nil || err.Code != CodeInvalidParams {
					t.Fatalf("NegotiateVersion(%q) err = %v, want -32602", c.client, err)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("NegotiateVersion(%q) = %q,%v want %q", c.client, got, err, c.want)
			}
		})
	}
}
