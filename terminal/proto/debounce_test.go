package proto

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/nullentropy/caution/terminal/ui"
	"github.com/nullentropy/caution/wire"
)

type sentConn struct {
	mu   sync.Mutex
	sent []string
}

func (c *sentConn) ReadMessage() ([]byte, error) { select {} }
func (c *sentConn) Close() error                 { return nil }
func (c *sentConn) WriteMessage(data []byte) error {
	var m clientEvent
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, fmt.Sprintf("%d %s %v", m.ID, m.Ev, m.Value))
	return nil
}

func (c *sentConn) events() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.sent)
}

func TestPendingInputGoesOutBeforeTheNextEvent(t *testing.T) {
	c := &sentConn{}
	s := &Session{ui: ui.New(), conn: c}
	s.Debounce(1, wire.EvInput, "a", 150*time.Millisecond)
	s.Debounce(1, wire.EvInput, "ab", 150*time.Millisecond)
	s.Event(0, wire.EvKey, 7)
	time.Sleep(250 * time.Millisecond)
	if got, want := c.events(), []string{"1 input ab", "0 key 7"}; !slices.Equal(got, want) {
		t.Fatalf("sent %v; want %v", got, want)
	}
}

func TestQuietInputSendsItsLastValueOnce(t *testing.T) {
	c := &sentConn{}
	s := &Session{ui: ui.New(), conn: c}
	s.Debounce(1, wire.EvInput, "a", 50*time.Millisecond)
	s.Debounce(1, wire.EvInput, "ab", 50*time.Millisecond)
	time.Sleep(150 * time.Millisecond)
	if got, want := c.events(), []string{"1 input ab"}; !slices.Equal(got, want) {
		t.Fatalf("sent %v; want %v", got, want)
	}
}
