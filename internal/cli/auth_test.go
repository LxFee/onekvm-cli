package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LxFee/onekvm-cli/internal/client"
	"github.com/LxFee/onekvm-cli/internal/store"
)

func TestRememberedPasswordRecoversRevokedSessionOnce(t *testing.T) {
	logins := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			var credentials map[string]string
			if err := json.NewDecoder(r.Body).Decode(&credentials); err != nil || credentials["password"] != "example-secret" {
				t.Error("saved password was not used for login")
			}
			logins++
			http.SetCookie(w, &http.Cookie{Name: client.CookieName, Value: "new-session", MaxAge: 3600})
			w.Write([]byte(`{}`))
		case "/api/hid/status":
			cookie, err := r.Cookie(client.CookieName)
			if err != nil || cookie.Value != "new-session" {
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"message":"Logged in elsewhere"}`))
				return
			}
			w.Write([]byte(`{"available":true}`))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer s.Close()
	home := t.TempDir()
	t.Setenv("ONEKVM_HOME", home)
	t.Setenv("ONEKVM_PASSWORD", "")
	target := store.Target{URL: s.URL, User: "test"}
	if err := store.Save(home, store.Config{Default: "home", Targets: map[string]store.Target{"home": target}}); err != nil {
		t.Fatal(err)
	}
	id := store.Identity(target)
	old, _ := json.Marshal(client.Session{Origin: s.URL, User: "test", Token: "old-session", Expires: time.Now().Add(time.Hour)})
	if err := store.SaveSession(home, id, old); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePassword(home, id, []byte("example-secret")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		a := &app{ctx: context.Background()}
		if err := a.init(); err != nil {
			t.Fatal(err)
		}
		c, err := a.connect()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.HID(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if logins != 1 {
		t.Fatalf("expected one automatic login and saved session reuse, got %d logins", logins)
	}
}

func TestRememberedPasswordRecoversLocallyExpiredSession(t *testing.T) {
	logins := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/login" {
			t.Errorf("unexpected request: %s", r.URL.Path)
			return
		}
		logins++
		http.SetCookie(w, &http.Cookie{Name: client.CookieName, Value: "new-session", MaxAge: 3600})
		w.Write([]byte(`{}`))
	}))
	defer s.Close()
	home := t.TempDir()
	t.Setenv("ONEKVM_HOME", home)
	t.Setenv("ONEKVM_PASSWORD", "")
	target := store.Target{URL: s.URL, User: "test"}
	if err := store.Save(home, store.Config{Default: "home", Targets: map[string]store.Target{"home": target}}); err != nil {
		t.Fatal(err)
	}
	id := store.Identity(target)
	old, _ := json.Marshal(client.Session{Origin: s.URL, User: "test", Token: "expired", Expires: time.Now().Add(-time.Hour)})
	if err := store.SaveSession(home, id, old); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePassword(home, id, []byte("example-secret")); err != nil {
		t.Fatal(err)
	}
	a := &app{ctx: context.Background()}
	if err := a.init(); err != nil {
		t.Fatal(err)
	}
	c, err := a.connect()
	if err != nil || c.Session.Token != "new-session" || logins != 1 {
		t.Fatalf("expired session was not replaced: %v, %d logins", err, logins)
	}
}

func TestRememberPasswordLoginAndLogout(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			http.SetCookie(w, &http.Cookie{Name: client.CookieName, Value: "saved-session", MaxAge: 3600})
			w.Write([]byte(`{}`))
		case "/api/auth/logout":
			w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer s.Close()
	home := t.TempDir()
	t.Setenv("ONEKVM_HOME", home)
	t.Setenv("ONEKVM_URL", s.URL)
	t.Setenv("ONEKVM_USER", "test")
	t.Setenv("ONEKVM_PASSWORD", "example-secret")
	a := &app{ctx: context.Background()}
	cmd := a.command()
	cmd.SetArgs([]string{"login", "--remember-password"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	id := store.Identity(store.Target{URL: s.URL, User: "test"})
	if got, err := store.LoadPassword(home, id); err != nil || string(got) != "example-secret" {
		t.Fatalf("password was not remembered: %v", err)
	}
	t.Setenv("ONEKVM_PASSWORD", "")
	a = &app{ctx: context.Background()}
	cmd = a.command()
	cmd.SetArgs([]string{"logout"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadPassword(home, id); err == nil {
		t.Fatal("logout retained remembered password")
	}
}
