package cli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOfflineHIDNeverSendsInput(t *testing.T) {
	wsRequested := false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "one_kvm_session", Value: "test"})
			w.Write([]byte(`{}`))
		case "/api/hid/status":
			w.Write([]byte(`{"available":true,"online":false}`))
		default:
			wsRequested = true
			w.WriteHeader(500)
		}
	}))
	defer s.Close()
	t.Setenv("ONEKVM_HOME", t.TempDir())
	t.Setenv("ONEKVM_URL", s.URL)
	t.Setenv("ONEKVM_USER", "test")
	t.Setenv("ONEKVM_PASSWORD", "test")
	a := &app{ctx: context.Background()}
	r := a.command()
	r.SetArgs([]string{"key", "Enter"})
	err := r.Execute()
	var f *fault
	if !errors.As(err, &f) || f.code != 4 {
		t.Fatalf("expected offline exit: %v", err)
	}
	if wsRequested {
		t.Fatal("input attempted while offline")
	}
}

func TestExcludedCommandsAndUnsupportedText(t *testing.T) {
	t.Setenv("ONEKVM_HOME", t.TempDir())
	for _, args := range [][]string{{"stream"}, {"ai"}, {"config", "get"}, {"type", "valid prefix中文"}} {
		a := &app{ctx: context.Background()}
		r := a.command()
		r.SetArgs(args)
		if err := r.Execute(); err == nil {
			t.Errorf("accepted %v", args)
		}
		if a.client != nil {
			t.Error("invalid input reached network")
		}
	}
}

func TestMissingMouseFunctionNeverSendsClick(t *testing.T) {
	wsRequested := false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "one_kvm_session", Value: "test"})
			w.Write([]byte(`{}`))
		case "/api/hid/status":
			w.Write([]byte(`{"available":true,"online":true,"backend":"otg","supports_absolute_mouse":true}`))
		case "/api/config/hid":
			w.Write([]byte(`{"otg_profile":"custom","otg_functions":{"keyboard":true,"mouse_absolute":true,"mouse_relative":false}}`))
		default:
			wsRequested = true
			w.WriteHeader(500)
		}
	}))
	defer s.Close()
	t.Setenv("ONEKVM_HOME", t.TempDir())
	t.Setenv("ONEKVM_URL", s.URL)
	t.Setenv("ONEKVM_USER", "test")
	t.Setenv("ONEKVM_PASSWORD", "test")
	a := &app{ctx: context.Background()}
	r := a.command()
	r.SetArgs([]string{"mouse", "click", "--x", "10", "--y", "10", "--width", "100", "--height", "100"})
	err := r.Execute()
	var f *fault
	if !errors.As(err, &f) || f.code != 4 {
		t.Fatalf("expected unavailable: %v", err)
	}
	if wsRequested {
		t.Fatal("partially sent input before checking all required functions")
	}
}
