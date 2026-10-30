package caution

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestAuthorizeRefusalIsADeniedMessage(t *testing.T) {
	opts := Options{Authorize: func(*http.Request) (any, error) { return nil, errors.New("signed out") }}
	tc := dialSessionOpts(t, func(*Session) *Node { return Panel() }, opts)
	if m := tc.read(); m["t"] != "denied" {
		t.Fatalf("first message = %v; want denied", m)
	}
	_ = tc.c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := tc.c.ReadMessage(); err == nil {
		t.Fatal("the server kept a denied connection open")
	}
}

// the client's reconnect must not resume the closed session
func TestCloseEndsTheSessionAndTheNextConnectMountsFresh(t *testing.T) {
	mounts := make(chan *Session, 4)
	tc := dialSession(t, func(s *Session) *Node {
		mounts <- s
		return Panel()
	})
	m := tc.read()
	sid, _ := m["sid"].(string)
	sess := <-mounts

	sess.Close()
	select {
	case <-sess.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not end the session")
	}
	_ = tc.c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := tc.c.ReadMessage(); err == nil {
		t.Fatal("the closed session's socket is still open")
	}

	tc.redial(sid, seqOf(t, m))
	m2 := tc.read()
	if m2["t"] != "mount" || m2["sid"] == sid {
		t.Fatalf("reconnect after Close = %v sid %v; want a fresh mount", m2["t"], m2["sid"])
	}
	if len(mounts) != 1 {
		t.Fatalf("mount ran %d more times; want once", len(mounts))
	}
}

func TestResumeGraceIsAnOption(t *testing.T) {
	mounts := make(chan *Session, 4)
	tc := dialSessionOpts(t, func(s *Session) *Node {
		mounts <- s
		return Panel()
	}, Options{ResumeGrace: 50 * time.Millisecond})
	m := tc.read()
	sid, _ := m["sid"].(string)
	sess := <-mounts

	_ = tc.c.Close()
	select {
	case <-sess.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the session outlived a 50ms grace")
	}
	tc.redial(sid, seqOf(t, m))
	if m2 := tc.read(); m2["t"] != "mount" || m2["sid"] == sid {
		t.Fatalf("reconnect after the grace = %v; want a fresh mount", m2["t"])
	}
}

func TestCookiesRideMountAndPatches(t *testing.T) {
	var sess *Session
	tc := dialSession(t, func(s *Session) *Node {
		sess = s
		s.SetCookie(&http.Cookie{Name: "login", Value: "abc", MaxAge: 60, HttpOnly: true})
		return Panel()
	})
	m := tc.read()
	cookies, _ := m["cookies"].([]any)
	if len(cookies) != 1 || cookies[0] != "login=abc; Path=/; Max-Age=60" {
		t.Fatalf("mount cookies = %v", m["cookies"])
	}

	sess.Update(func() { sess.SetCookie(&http.Cookie{Name: "login", MaxAge: -1}) })
	ops, _ := tc.read()["ops"].([]any)
	op, _ := ops[0].(map[string]any)
	if len(ops) != 1 || op["op"] != "cookie" || op["cookie"] != "login=; Path=/; Max-Age=0" {
		t.Fatalf("live cookie ops = %v", ops)
	}
}

func TestCookieSetWhileDisconnectedArrivesOnResume(t *testing.T) {
	var sess *Session
	tc := dialSession(t, func(s *Session) *Node {
		sess = s
		return Panel()
	})
	m := tc.read()
	sid, _ := m["sid"].(string)
	_ = tc.c.Close()
	for gone := false; !gone; {
		done := make(chan bool)
		sess.Update(func() { done <- sess.conn == nil })
		gone = <-done
	}
	sess.Update(func() { sess.SetCookie(&http.Cookie{Name: "login", Value: "abc"}) })

	tc.redial(sid, seqOf(t, m))
	var got []any
	for got == nil {
		msg := tc.read()
		if msg["t"] == "mount" {
			got, _ = msg["cookies"].([]any)
		} else if ops, ok := msg["ops"].([]any); ok {
			for _, o := range ops {
				if op, _ := o.(map[string]any); op["op"] == "cookie" {
					got = append(got, op["cookie"])
				}
			}
		}
	}
	if len(got) != 1 || got[0] != "login=abc; Path=/" {
		t.Fatalf("cookies after resume = %v", got)
	}
}
