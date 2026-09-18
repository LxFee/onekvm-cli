package client

import (
	"context"
	"encoding/json"
	"github.com/LxFee/onekvm-cli/internal/store"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestAuthenticationIsolationAndRedirect(t *testing.T) {
	leaked := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" {
			var input map[string]string
			json.NewDecoder(r.Body).Decode(&input)
			if input["password"] != "test-password" {
				t.Error("wrong login body")
			}
			http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "test-session", MaxAge: 60})
			w.Write([]byte(`{}`))
			return
		}
		cookie, err := r.Cookie(CookieName)
		if err != nil || cookie.Value != "test-session" {
			t.Error("missing session")
		}
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	c := New(store.Target{URL: server.URL, User: "test"})
	if err := c.Login(context.Background(), "test-password", nil); err != nil {
		t.Fatal(err)
	}
	if err := c.JSON(context.Background(), "GET", "/health", nil, nil); err == nil {
		t.Fatal("redirect accepted")
	}
	if leaked {
		t.Fatal("followed cross-origin redirect")
	}
	b, _ := json.Marshal(c.Session)
	wrong := New(store.Target{URL: other.URL, User: "test"})
	if wrong.Restore(b) == nil {
		t.Fatal("cross-origin session accepted")
	}
	c.Session.Expires = time.Now().Add(-time.Second)
	b, _ = json.Marshal(c.Session)
	if c.Restore(b) == nil {
		t.Fatal("expired session accepted")
	}
}

func TestSnapshotStartsCaptureOnDemand(t *testing.T) {
	started := false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/stream/start":
			if r.Method != "POST" {
				t.Error("start method")
			}
			started = true
			w.Write([]byte(`{}`))
		case "/api/snapshot":
			if !started {
				w.WriteHeader(503)
			} else {
				w.Write([]byte("image-bytes"))
			}
		default:
			t.Error("unexpected request")
		}
	}))
	defer s.Close()
	c := New(store.Target{URL: s.URL, User: "test"})
	b, err := c.Snapshot(context.Background())
	if err != nil || string(b) != "image-bytes" || !started {
		t.Fatalf("snapshot: %q %v", b, err)
	}
}

func TestHIDBinaryProtocolAndTransportBarrier(t *testing.T) {
	expected := [][]byte{{1, 0, 4, 1}, {1, 1, 4, 0}, {2, 1, 255, 127, 255, 127, 0}}
	done := make(chan [][]byte, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(CookieName)
		if err != nil || cookie.Value != "test-session" {
			t.Error("websocket missing auth")
		}
		u := websocket.Upgrader{}
		ws, err := u.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		ws.WriteMessage(websocket.BinaryMessage, []byte{0})
		var frames [][]byte
		defer func() { done <- frames }()
		for {
			kind, b, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if kind != websocket.BinaryMessage {
				t.Error("not binary")
			}
			frames = append(frames, b)
		}
	}))
	defer s.Close()
	c := New(store.Target{URL: s.URL, User: "test"})
	c.Session.Token = "test-session"
	if err := c.SendHID(context.Background(), expected, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("frames %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("connection not released")
	}
}

func TestTOTPLogin(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" {
			w.Write([]byte(`{"next":"totp","challenge_id":"challenge"}`))
			return
		}
		var input map[string]string
		json.NewDecoder(r.Body).Decode(&input)
		if input["challenge_id"] != "challenge" || input["code"] != "123456" {
			t.Error("invalid TOTP challenge")
		}
		http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "test-session"})
		w.Write([]byte(`{}`))
	}))
	defer s.Close()
	c := New(store.Target{URL: s.URL, User: "test"})
	if err := c.Login(context.Background(), "test", func() (string, error) { return "123456", nil }); err != nil {
		t.Fatal(err)
	}
}
