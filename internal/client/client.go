package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/LxFee/onekvm-cli/internal/store"
	"github.com/gorilla/websocket"
)

const CookieName = "one_kvm_session"

type APIError struct {
	Status  int
	Path    string
	Message string
}

func (e *APIError) Error() string {
	if e.Status == 401 || e.Status == 403 {
		if e.Message == "Logged in elsewhere" || e.Message == "Session expired" {
			return e.Message + "; run onekvm login"
		}
		return "authentication expired or rejected; run onekvm login"
	}
	return fmt.Sprintf("One-KVM returned HTTP %d for %s", e.Status, e.Path)
}

type Session struct {
	Origin  string    `json:"origin"`
	User    string    `json:"user"`
	Token   string    `json:"token"`
	Expires time.Time `json:"expires"`
}
type Client struct {
	Target         store.Target
	HTTP           *http.Client
	Session        Session
	Reauthenticate func(context.Context) error
}

func New(t store.Target) *Client {
	return &Client{Target: t, HTTP: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *Client) Restore(b []byte) error {
	var s Session
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("invalid stored session; log in again")
	}
	if s.Origin != c.Target.URL || s.User != c.Target.User || s.Token == "" {
		return errors.New("stored session belongs to a different target")
	}
	if !s.Expires.IsZero() && time.Now().After(s.Expires) {
		return errors.New("stored session expired; run onekvm login")
	}
	c.Session = s
	return nil
}
func (c *Client) request(ctx context.Context, method, path string, data any) ([]byte, http.Header, error) {
	b, h, err := c.requestOnce(ctx, method, path, data)
	var apiErr *APIError
	if c.Reauthenticate != nil && path != "/auth/login" && path != "/auth/login/totp" && errors.As(err, &apiErr) && apiErr.Status == 401 && (apiErr.Message == "Session expired" || apiErr.Message == "Logged in elsewhere") {
		if err = c.Reauthenticate(ctx); err != nil {
			return nil, nil, fmt.Errorf("automatic login failed: %w", err)
		}
		return c.requestOnce(ctx, method, path, data)
	}
	return b, h, err
}
func (c *Client) requestOnce(ctx context.Context, method, path string, data any) ([]byte, http.Header, error) {
	var body io.Reader
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return nil, nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Target.URL+"/api"+path, body)
	if err != nil {
		return nil, nil, err
	}
	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Session.Token != "" {
		req.AddCookie(&http.Cookie{Name: CookieName, Value: c.Session.Token})
	}
	r, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer r.Body.Close()
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		var body struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)
		return nil, nil, &APIError{Status: r.StatusCode, Path: path, Message: body.Message}
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 16*1024*1024+1))
	if len(b) > 16*1024*1024 {
		return nil, nil, errors.New("response exceeds 16 MiB limit")
	}
	return b, r.Header, err
}
func (c *Client) JSON(ctx context.Context, method, path string, data, out any) error {
	b, _, err := c.request(ctx, method, path, data)
	if err != nil {
		return err
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}
func (c *Client) Login(ctx context.Context, password string, otp func() (string, error)) error {
	b, h, err := c.request(ctx, "POST", "/auth/login", map[string]string{"username": c.Target.User, "password": password})
	if err != nil {
		return err
	}
	var result struct {
		Next      string `json:"next"`
		Challenge string `json:"challenge_id"`
	}
	_ = json.Unmarshal(b, &result)
	if result.Next == "totp" {
		code, e := otp()
		if e != nil {
			return e
		}
		_, h, err = c.request(ctx, "POST", "/auth/login/totp", map[string]string{"challenge_id": result.Challenge, "code": code})
		if err != nil {
			return err
		}
	}
	r := http.Response{Header: h}
	for _, cookie := range r.Cookies() {
		if cookie.Name == CookieName && cookie.Value != "" {
			expires := cookie.Expires
			if expires.IsZero() && cookie.MaxAge > 0 {
				expires = time.Now().Add(time.Duration(cookie.MaxAge) * time.Second)
			}
			c.Session = Session{c.Target.URL, c.Target.User, cookie.Value, expires}
			return nil
		}
	}
	return errors.New("login response did not include a session cookie")
}

type HIDStatus struct {
	Available        bool    `json:"available"`
	Online           bool    `json:"online"`
	Initialized      bool    `json:"initialized"`
	Backend          string  `json:"backend"`
	Absolute         bool    `json:"supports_absolute_mouse"`
	AbsoluteButtons  bool    `json:"absolute_mouse_buttons"`
	PreservesPointer bool    `json:"preserves_pointer_on_reset"`
	Resolution       []int   `json:"screen_resolution"`
	Error            *string `json:"error"`
	ErrorCode        *string `json:"error_code"`
}

func (c *Client) HID(ctx context.Context) (HIDStatus, error) {
	var h HIDStatus
	err := c.JSON(ctx, "GET", "/hid/status", nil, &h)
	return h, err
}

type HIDFunctions struct {
	Keyboard         bool `json:"keyboard"`
	ButtonsAndScroll bool `json:"mouse_buttons_and_scroll"`
	Absolute         bool `json:"absolute_mouse"`
}

// The status endpoint reports backend availability, not individual USB functions.
// Decode only the function-selection fields; never expose the full configuration.
func (c *Client) Functions(ctx context.Context, h HIDStatus) (HIDFunctions, error) {
	if h.Backend != "otg" {
		return HIDFunctions{true, true, h.Absolute}, nil
	}
	var cfg struct {
		Profile   string `json:"otg_profile"`
		Functions struct {
			Keyboard bool `json:"keyboard"`
			Relative bool `json:"mouse_relative"`
			Absolute bool `json:"mouse_absolute"`
		} `json:"otg_functions"`
	}
	if err := c.JSON(ctx, "GET", "/config/hid", nil, &cfg); err != nil {
		return HIDFunctions{}, err
	}
	switch cfg.Profile {
	case "full", "full_no_msd", "full_no_consumer", "full_no_consumer_no_msd":
		return HIDFunctions{true, true, true}, nil
	case "legacy_keyboard":
		return HIDFunctions{Keyboard: true}, nil
	case "legacy_mouse_relative":
		return HIDFunctions{ButtonsAndScroll: true}, nil
	case "custom":
		return HIDFunctions{cfg.Functions.Keyboard, cfg.Functions.Relative || (cfg.Functions.Absolute && h.Absolute && h.AbsoluteButtons), cfg.Functions.Absolute}, nil
	default:
		return HIDFunctions{}, errors.New("unknown OTG function profile; cannot establish input capabilities")
	}
}
func (c *Client) Snapshot(ctx context.Context) ([]byte, error) {
	b, _, err := c.request(ctx, "GET", "/snapshot", nil)
	var e *APIError
	if errors.As(err, &e) && e.Status == 503 {
		if err = c.JSON(ctx, "POST", "/stream/start", map[string]any{}, nil); err != nil {
			return nil, err
		}
		for i := 0; i < 10; i++ {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
			b, _, err = c.request(ctx, "GET", "/snapshot", nil)
			if err == nil {
				break
			}
			if !errors.As(err, &e) || e.Status != 503 {
				break
			}
		}
	}
	return b, err
}

// SendHID uses the versioned binary protocol, not JSON. A pong is a transport
// barrier only: One-KVM does not acknowledge execution of individual events.
func (c *Client) SendHID(ctx context.Context, frames [][]byte, delay time.Duration) error {
	u, err := url.Parse(c.Target.URL)
	if err != nil {
		return err
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = "/api/ws/hid"
	h := http.Header{}
	h.Set("Cookie", (&http.Cookie{Name: CookieName, Value: c.Session.Token}).String())
	d := websocket.Dialer{HandshakeTimeout: 10 * time.Second, Proxy: http.ProxyFromEnvironment}
	ws, r, err := d.DialContext(ctx, u.String(), h)
	if err != nil && r != nil && r.StatusCode == 401 && c.Reauthenticate != nil {
		if r.Body != nil {
			_ = r.Body.Close()
		}
		if loginErr := c.Reauthenticate(ctx); loginErr != nil {
			return fmt.Errorf("automatic login failed: %w", loginErr)
		}
		h.Set("Cookie", (&http.Cookie{Name: CookieName, Value: c.Session.Token}).String())
		ws, r, err = d.DialContext(ctx, u.String(), h)
	}
	if err != nil {
		if r != nil && r.StatusCode == 401 {
			if r.Body != nil {
				_ = r.Body.Close()
			}
			return &APIError{Status: 401, Path: "/ws/hid"}
		}
		return err
	}
	defer ws.Close()
	_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	typ, b, err := ws.ReadMessage()
	if err != nil {
		return err
	}
	if typ != websocket.BinaryMessage || len(b) != 1 || b[0] != 0 {
		return errors.New("One-KVM HID backend unavailable")
	}
	defer func() {
		_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	}()
	for _, f := range frames {
		if err = ctx.Err(); err != nil {
			return err
		}
		_ = ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err = ws.WriteMessage(websocket.BinaryMessage, f); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	barrier := fmt.Sprintf("cli-%d", time.Now().UnixNano())
	ack := errors.New("transport barrier reached")
	ws.SetPongHandler(func(s string) error {
		if s == barrier {
			return ack
		}
		return nil
	})
	if err = ws.WriteControl(websocket.PingMessage, []byte(barrier), time.Now().Add(time.Second)); err != nil {
		return err
	}
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		typ, b, err = ws.ReadMessage()
		if errors.Is(err, ack) {
			return nil
		}
		if err != nil {
			return err
		}
		if typ == websocket.BinaryMessage && len(b) == 1 && b[0] != 0 {
			return errors.New("One-KVM rejected HID input")
		}
	}
}
func IsAuth(err error) bool {
	var e *APIError
	return errors.As(err, &e) && (e.Status == 401 || e.Status == 403)
}
func SafeError(err error, token string) string {
	s := err.Error()
	if token != "" {
		s = strings.ReplaceAll(s, token, "<redacted>")
	}
	return s
}
