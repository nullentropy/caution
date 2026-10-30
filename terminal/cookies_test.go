package terminal

import (
	"testing"
)

func names(j *cookieJar) map[string]string {
	out := map[string]string{}
	for _, c := range j.Cookies(nil) {
		out[c.Name] = c.Value
	}
	return out
}

func TestCookieJarPersistsAcrossRestarts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	j := openCookieJar("app-a")
	j.set("login=abc; Path=/; Max-Age=3600")
	j.set("pref=dark; Path=/; Max-Age=3600")
	j.set("tab=2; Path=/") // a session cookie, gone with the process
	if got := names(j); len(got) != 3 {
		t.Fatalf("live jar = %v", got)
	}

	j.set("pref=; Path=/; Max-Age=0")
	got := names(openCookieJar("app-a"))
	if len(got) != 1 || got["login"] != "abc" {
		t.Fatalf("after restart = %v; want only login", got)
	}
	if other := names(openCookieJar("app-b")); len(other) != 0 {
		t.Fatalf("another app sees %v", other)
	}
}
