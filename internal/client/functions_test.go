package client

import (
	"context"
	"fmt"
	"github.com/LxFee/onekvm-cli/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAbsoluteButtonsRequireExplicitServerCapability(t *testing.T) {
	for _, tc := range []struct {
		name                                 string
		absolute, relative, advertised, want bool
	}{
		{"old absolute-only server", true, false, false, false},
		{"patched absolute-only server", true, false, true, true},
		{"disabled endpoint despite capability", false, false, true, false},
		{"legacy relative server", false, true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"otg_profile":"custom","otg_functions":{"keyboard":true,"mouse_absolute":%t,"mouse_relative":%t}}`, tc.absolute, tc.relative)
			}))
			defer s.Close()
			c := New(store.Target{URL: s.URL})
			f, err := c.Functions(context.Background(), HIDStatus{Backend: "otg", Absolute: tc.absolute, AbsoluteButtons: tc.advertised})
			if err != nil {
				t.Fatal(err)
			}
			if f.ButtonsAndScroll != tc.want {
				t.Fatalf("buttons/scroll=%v, want %v", f.ButtonsAndScroll, tc.want)
			}
		})
	}
}
