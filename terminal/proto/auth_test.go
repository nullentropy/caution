package proto

import (
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nullentropy/caution/terminal/ui"
	"github.com/nullentropy/caution/wire"
)

// hands out its messages then reports the socket closed
type scriptedConn struct {
	mu   sync.Mutex
	msgs []string
}

func (c *scriptedConn) ReadMessage() ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.msgs) == 0 {
		return nil, errors.New("closed")
	}
	m := c.msgs[0]
	c.msgs = c.msgs[1:]
	return []byte(m), nil
}
func (c *scriptedConn) WriteMessage([]byte) error { return nil }
func (c *scriptedConn) Close() error              { return nil }

func TestDeniedConnectionIsNotRetried(t *testing.T) {
	var dials atomic.Int32
	dial := func(string) (Conn, error) {
		dials.Add(1)
		return &scriptedConn{msgs: []string{`{"t":"denied"}`}}, nil
	}
	NewSession(ui.New(), Config{URL: "ws://caution/ws", Dial: dial})
	// the first retry would land at 500ms
	time.Sleep(900 * time.Millisecond)
	if n := dials.Load(); n != 1 {
		t.Fatalf("dialed %d times after a denial; want 1", n)
	}
}

func TestCookiesFromMountAndOpsReachTheHook(t *testing.T) {
	var got []string
	s := &Session{ui: ui.New(), setCookie: func(raw string) { got = append(got, raw) }}
	s.store = NewNodeStore(s)
	s.handle(&serverMsg{T: wire.MsgMount, Cookies: []string{"a=1; Path=/"}})
	s.applyOp(&patchOp{Op: wire.OpCookie, Cookie: "a=; Path=/; Max-Age=0"})
	if want := []string{"a=1; Path=/", "a=; Path=/; Max-Age=0"}; !slices.Equal(got, want) {
		t.Fatalf("hook saw %v; want %v", got, want)
	}
}
