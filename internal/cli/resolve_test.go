package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerAddListRemove(t *testing.T) {
	base, cleanup := tempHome(t)
	defer cleanup()

	env := Env{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	if code := runProject([]string{"add", "user", filepath.Dir(base)}, env); code != 0 {
		t.Fatalf("project add exit=%d stderr=%s", code, env.Stderr.(*bytes.Buffer).String())
	}
	if code := runServer([]string{"add", "user-test", "10.74.194.40:12200", "--project", "user"}, env); code != 0 {
		t.Fatalf("add exit=%d stderr=%s", code, env.Stderr.(*bytes.Buffer).String())
	}

	listOut := &bytes.Buffer{}
	listEnv := Env{Stdout: listOut, Stderr: &bytes.Buffer{}}
	if code := runServer([]string{"list", "--json"}, listEnv); code != 0 {
		t.Fatalf("list exit=%d", code)
	}
	if !strings.Contains(listOut.String(), `"user-test"`) {
		t.Fatalf("list missing alias: %s", listOut.String())
	}

	rmEnv := Env{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	if code := runServer([]string{"remove", "user-test", "--confirm"}, rmEnv); code != 0 {
		t.Fatalf("remove exit=%d stderr=%s", code, rmEnv.Stderr.(*bytes.Buffer).String())
	}

	rmMissingEnv := Env{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	if code := runServer([]string{"remove", "user-test", "--confirm"}, rmMissingEnv); code == 0 {
		t.Fatal("expected non-zero exit when removing missing alias")
	}
}
