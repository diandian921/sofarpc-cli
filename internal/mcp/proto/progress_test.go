package proto

import (
	"encoding/json"
	"testing"
)

func TestValidProgressToken(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{`"abc"`, true},
		{`5`, true},
		{`-7`, true},
		{`1.0`, true},
		{`2e3`, true},
		{`9007199254740992`, true},
		{`9007199254740993`, false},
		{`1.2`, false},
		{`true`, false},
		{`null`, false},
		{`[1]`, false},
		{`{}`, false},
	}
	for _, c := range cases {
		if got := validProgressToken(json.RawMessage(c.raw)); got != c.want {
			t.Errorf("validProgressToken(%s) = %v, want %v", c.raw, got, c.want)
		}
	}
}

func TestProgressTokenFromParamsRejectsBadToken(t *testing.T) {
	if _, ok := progressTokenFromParams(json.RawMessage(`{"_meta":{"progressToken":1.2}}`)); ok {
		t.Fatal("fractional progressToken must be rejected")
	}
	if _, ok := progressTokenFromParams(json.RawMessage(`{"_meta":{"progressToken":7}}`)); !ok {
		t.Fatal("integer progressToken must be accepted")
	}
	if _, ok := progressTokenFromParams(json.RawMessage(`{"_meta":{"progressToken":"abc"}}`)); !ok {
		t.Fatal("string progressToken must be accepted")
	}
	if _, ok := progressTokenFromParams(json.RawMessage(`{}`)); ok {
		t.Fatal("absent progressToken must be false")
	}
}
