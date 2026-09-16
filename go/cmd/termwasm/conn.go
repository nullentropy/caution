//go:build js && wasm

package main

import (
	"errors"
	"sync"
	"syscall/js"

	"github.com/nullentropy/caution/go/terminal/proto"
)

// jsConn is a proto.Conn over the page's WebSocket. Socket callbacks run on
// the JS thread and must never block, so messages queue under a mutex and
// ReadMessage waits on a signal channel from the session's read goroutine.
type jsConn struct {
	ws     js.Value
	funcs  []js.Func
	mu     sync.Mutex
	queue  [][]byte
	closed bool
	signal chan struct{}
}

var errClosed = errors.New("websocket closed")

func dial(url string) (proto.Conn, error) {
	c := &jsConn{ws: js.Global().Get("WebSocket").New(url), signal: make(chan struct{}, 1)}
	opened := make(chan error, 1)
	c.on("open", func(js.Value) { opened <- nil })
	c.on("message", func(ev js.Value) {
		data := ev.Get("data")
		var msg []byte
		if data.Type() == js.TypeString {
			msg = []byte(data.String())
		} else {
			arr := js.Global().Get("Uint8Array").New(data)
			msg = make([]byte, arr.Length())
			js.CopyBytesToGo(msg, arr)
		}
		c.mu.Lock()
		c.queue = append(c.queue, msg)
		c.mu.Unlock()
		c.wake()
	})
	c.on("close", func(js.Value) {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		c.wake()
		select {
		case opened <- errClosed:
		default:
		}
	})
	c.on("error", func(js.Value) {})
	if err := <-opened; err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *jsConn) on(event string, fn func(ev js.Value)) {
	f := js.FuncOf(func(_ js.Value, a []js.Value) any {
		fn(a[0])
		return nil
	})
	c.funcs = append(c.funcs, f)
	c.ws.Set("on"+event, f)
}

func (c *jsConn) wake() {
	select {
	case c.signal <- struct{}{}:
	default:
	}
}

func (c *jsConn) ReadMessage() ([]byte, error) {
	for {
		c.mu.Lock()
		if len(c.queue) > 0 {
			msg := c.queue[0]
			c.queue = c.queue[1:]
			c.mu.Unlock()
			return msg, nil
		}
		closed := c.closed
		c.mu.Unlock()
		if closed {
			return nil, errClosed
		}
		<-c.signal
	}
}

func (c *jsConn) WriteMessage(data []byte) error {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return errClosed
	}
	c.ws.Call("send", string(data))
	return nil
}

func (c *jsConn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.wake()
	c.ws.Call("close")
	for _, f := range c.funcs {
		f.Release()
	}
	c.funcs = nil
	return nil
}
