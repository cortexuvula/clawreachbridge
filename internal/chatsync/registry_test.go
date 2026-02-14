package chatsync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestRegistryRegisterAndCount(t *testing.T) {
	r := NewClientRegistry()

	if r.ClientCount("s1") != 0 {
		t.Errorf("empty session count = %d, want 0", r.ClientCount("s1"))
	}

	r.Register("s1", "c1", nil)
	if r.ClientCount("s1") != 1 {
		t.Errorf("after 1 register = %d, want 1", r.ClientCount("s1"))
	}

	r.Register("s1", "c2", nil)
	if r.ClientCount("s1") != 2 {
		t.Errorf("after 2 registers = %d, want 2", r.ClientCount("s1"))
	}
}

func TestRegistryUnregister(t *testing.T) {
	r := NewClientRegistry()

	r.Register("s1", "c1", nil)
	r.Register("s1", "c2", nil)

	r.Unregister("s1", "c1")
	if r.ClientCount("s1") != 1 {
		t.Errorf("after unregister = %d, want 1", r.ClientCount("s1"))
	}

	r.Unregister("s1", "c2")
	if r.ClientCount("s1") != 0 {
		t.Errorf("after unregister all = %d, want 0", r.ClientCount("s1"))
	}
}

func TestRegistryUnregisterNonexistent(t *testing.T) {
	r := NewClientRegistry()
	// Should not panic
	r.Unregister("nonexistent", "c1")
	r.Register("s1", "c1", nil)
	r.Unregister("s1", "nonexistent")
}

func TestRegistryIsolatesSessions(t *testing.T) {
	r := NewClientRegistry()

	r.Register("s1", "c1", nil)
	r.Register("s2", "c2", nil)

	if r.ClientCount("s1") != 1 || r.ClientCount("s2") != 1 {
		t.Errorf("sessions not isolated: s1=%d s2=%d", r.ClientCount("s1"), r.ClientCount("s2"))
	}

	r.Unregister("s1", "c1")
	if r.ClientCount("s1") != 0 || r.ClientCount("s2") != 1 {
		t.Errorf("unregister crossed sessions: s1=%d s2=%d", r.ClientCount("s1"), r.ClientCount("s2"))
	}
}

// connPair holds both ends of a WebSocket connection for testing.
type connPair struct {
	id         string
	serverConn *websocket.Conn
	clientConn *websocket.Conn
}

func TestRegistryBroadcast(t *testing.T) {
	r := NewClientRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Channel to receive server-side connections from the handler
	type serverResult struct {
		id   string
		conn *websocket.Conn
	}
	serverConns := make(chan serverResult, 2)
	done := make(chan struct{})

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		id := req.URL.Query().Get("id")
		conn, err := websocket.Accept(w, req, nil)
		if err != nil {
			return
		}
		serverConns <- serverResult{id, conn}
		<-done // Keep handler alive until test cleanup
		conn.CloseNow()
	}))
	defer s.Close()
	defer close(done)

	// Connect c1 (sender)
	c1, _, err := websocket.Dial(ctx, "ws"+s.URL[4:]+"?id=c1", nil)
	if err != nil {
		t.Fatalf("dial c1: %v", err)
	}
	defer c1.CloseNow()
	sc1 := <-serverConns

	// Connect c2 (receiver)
	c2, _, err := websocket.Dial(ctx, "ws"+s.URL[4:]+"?id=c2", nil)
	if err != nil {
		t.Fatalf("dial c2: %v", err)
	}
	defer c2.CloseNow()
	sc2 := <-serverConns

	// Register server-side connections (bridge writes to these)
	r.Register("sess", sc1.id, sc1.conn)
	r.Register("sess", sc2.id, sc2.conn)

	// Broadcast from c1 — should write to server-side of c2 only
	payload := []byte(`{"test":"broadcast"}`)
	r.Broadcast(ctx, "sess", "c1", payload)

	// Client-side of c2 should receive the broadcast
	_, msg, err := c2.Read(ctx)
	if err != nil {
		t.Fatalf("read c2: %v", err)
	}
	if string(msg) != string(payload) {
		t.Errorf("received %q, want %q", msg, payload)
	}

	// Client-side of c1 should NOT receive (sender excluded)
	readCtx, readCancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer readCancel()
	_, _, err = c1.Read(readCtx)
	if err == nil {
		t.Error("c1 (sender) should not have received broadcast")
	}
}
