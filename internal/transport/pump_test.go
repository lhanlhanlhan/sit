package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/sit/sit/internal/protocol"
)

// echoServer accepts one websocket connection and echoes every text frame back.
func echoServer(t *testing.T) (*httptest.Server, chan []byte) {
	t.Helper()
	received := make(chan []byte, 16)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		ws.SetReadLimit(protocol.MaxFrameBytes)
		ctx := context.Background()
		for {
			typ, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			received <- data
			_ = ws.Write(ctx, typ, data)
		}
	})
	return httptest.NewServer(h), received
}

func dialTestConn(t *testing.T, url string) *wsConn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(url, "http")
	ws, _, err := websocket.Dial(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return newWSConn(ws, SessionInfo{RemoteAddr: url, ConnectedAt: protocol.NowMillis()})
}

func TestWSConn_SendRecvRoundTrip(t *testing.T) {
	srv, _ := echoServer(t)
	defer srv.Close()
	c := dialTestConn(t, srv.URL)
	defer c.Close("done")

	env, _ := protocol.NewNotification(protocol.Notification{Kind: protocol.KindEvent, Event: "online"})
	if err := c.Send(context.Background(), env); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case got := <-c.Recv():
		n, err := got.AsNotification()
		if err != nil {
			t.Fatalf("AsNotification: %v", err)
		}
		if n.Event != "online" {
			t.Errorf("event: got %q want online", n.Event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for echo")
	}
}

func TestWSConn_CloseSemantics(t *testing.T) {
	srv, _ := echoServer(t)
	defer srv.Close()
	c := dialTestConn(t, srv.URL)

	c.Close("bye")
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("Done not closed after Close")
	}
	if err := c.Send(context.Background(), protocol.Envelope{}); err != ErrConnClosed {
		t.Errorf("Send after close: got %v want ErrConnClosed", err)
	}
	// Recv channel should drain/close.
	select {
	case <-c.Recv():
	case <-time.After(time.Second):
		t.Fatal("Recv channel not closed after Close")
	}
}

func TestWSConn_ReadLimitRejectsOversize(t *testing.T) {
	// Server sends an oversize frame; our read pump must error and close the conn
	// rather than allocate it. (read limit == MaxFrameBytes)
	bigClosed := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		// Write a frame larger than MaxFrameBytes.
		payload := strings.Repeat("x", protocol.MaxFrameBytes+100)
		_ = ws.Write(context.Background(), websocket.MessageText, []byte(payload))
		<-bigClosed
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	defer close(bigClosed)

	c := dialTestConn(t, srv.URL)
	select {
	case <-c.Done():
		// Good: read limit exceeded -> read error -> connection closed.
	case <-time.After(2 * time.Second):
		t.Fatal("connection not closed on oversize inbound frame")
	}
}
