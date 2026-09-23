package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/LxFee/onekvm-cli/internal/client"
	"github.com/LxFee/onekvm-cli/internal/hid"
	"github.com/LxFee/onekvm-cli/internal/store"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var Version = "dev"

type fault struct {
	code    int
	message string
}

func (f *fault) Error() string { return f.message }

type app struct {
	home, name, url, user string
	json                  bool
	cfg                   store.Config
	ctx                   context.Context
	client                *client.Client
}

func (a *app) print(v any) error {
	e := json.NewEncoder(os.Stdout)
	if !a.json {
		e.SetIndent("", "  ")
	}
	return e.Encode(v)
}
func (a *app) init() error {
	h, err := store.Home()
	if err != nil {
		return err
	}
	a.home = h
	a.cfg, err = store.Load(h)
	return err
}
func (a *app) newClient() (*client.Client, error) {
	t, err := store.Resolve(a.cfg, a.name, a.url, a.user, os.Getenv("ONEKVM_URL"), os.Getenv("ONEKVM_USER"))
	if err != nil {
		return nil, &fault{2, err.Error()}
	}
	c := client.New(t)
	a.client = c
	return c, nil
}
func secret(env, prompt string) (string, error) {
	if v := os.Getenv(env); v != "" {
		return v, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", &fault{3, "authentication requires a terminal or " + env + " environment variable"}
	}
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return string(b), err
}
func otp() (string, error) { return secret("ONEKVM_TOTP", "One-time code: ") }
func (a *app) connect() (*client.Client, error) {
	c, err := a.newClient()
	if err != nil {
		return nil, err
	}
	if p := os.Getenv("ONEKVM_PASSWORD"); p != "" {
		err = c.Login(a.ctx, p, otp)
		return c, err
	}
	id := store.Identity(c.Target)
	password, passwordErr := store.LoadPassword(a.home, id)
	if passwordErr == nil && len(password) != 0 {
		c.Reauthenticate = func(ctx context.Context) error {
			if err := c.Login(ctx, string(password), otp); err != nil {
				return err
			}
			b, err := json.Marshal(c.Session)
			if err != nil {
				return err
			}
			return store.SaveSession(a.home, id, b)
		}
	}
	b, err := store.LoadSession(a.home, id)
	if err != nil {
		if c.Reauthenticate != nil {
			return c, c.Reauthenticate(a.ctx)
		}
		return nil, &fault{3, "no readable saved session; run onekvm login or provide ONEKVM_PASSWORD"}
	}
	if err = c.Restore(b); err != nil {
		if c.Reauthenticate != nil {
			return c, c.Reauthenticate(a.ctx)
		}
		return nil, &fault{3, err.Error()}
	}
	return c, nil
}
func Main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	a := &app{ctx: ctx}
	root := a.command()
	err := root.ExecuteContext(ctx)
	if err == nil {
		return
	}
	code := 1
	var f *fault
	if errors.As(err, &f) {
		code = f.code
	}
	if client.IsAuth(err) {
		code = 3
	}
	message := err.Error()
	if a.client != nil {
		message = client.SafeError(err, a.client.Session.Token)
	}
	if a.json {
		_ = json.NewEncoder(os.Stderr).Encode(map[string]any{"error": message, "exit_code": code})
	} else {
		fmt.Fprintln(os.Stderr, "Error:", message)
	}
	os.Exit(code)
}
func (a *app) command() *cobra.Command {
	r := &cobra.Command{Use: "onekvm", Short: "One-KVM native CLI: inspect, capture and control", Version: Version, SilenceUsage: true, SilenceErrors: true, PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return a.init() }}
	r.CompletionOptions.DisableDefaultCmd = true
	r.PersistentFlags().StringVar(&a.name, "target", "", "named target (overrides target environment defaults)")
	r.PersistentFlags().StringVar(&a.url, "url", "", "One-KVM origin, e.g. http://host:8080")
	r.PersistentFlags().StringVar(&a.user, "user", "", "One-KVM username")
	r.PersistentFlags().BoolVar(&a.json, "json", false, "compact JSON output, including errors on stderr")
	r.AddCommand(a.targets(), a.login(), a.logout(), a.status(false), a.status(true), a.stream(), a.snapshot(), a.mouse(), a.key(), a.typing())
	return r
}
func (a *app) targets() *cobra.Command {
	r := &cobra.Command{Use: "target", Short: "Manage connection addresses and target credentials"}
	add := &cobra.Command{Use: "add NAME", Args: cobra.ExactArgs(1), Short: "Add a target or update its URL/user", RunE: func(_ *cobra.Command, args []string) error {
		name := args[0]
		if !store.ValidName(name) {
			return &fault{2, "target name must be 1–64 letters, digits, underscores or hyphens"}
		}
		if a.url == "" || a.user == "" {
			return &fault{2, "target add requires --url and --user"}
		}
		u, err := store.NormalizeURL(a.url)
		if err != nil {
			return err
		}
		t := store.Target{URL: u, User: a.user}
		if old, ok := a.cfg.Targets[name]; ok && old != t {
			id := store.Identity(old)
			if err = store.DeleteSession(a.home, id); err != nil {
				return err
			}
			if err = store.DeletePassword(a.home, id); err != nil {
				return err
			}
		}
		a.cfg.Targets[name] = t
		if a.cfg.Default == "" {
			a.cfg.Default = name
		}
		if err = store.Save(a.home, a.cfg); err != nil {
			return err
		}
		return a.print(map[string]any{"target": name, "connection": t, "config_file": filepath.Join(a.home, "config.json")})
	}}
	r.AddCommand(add)
	r.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, Short: "List saved target addresses", RunE: func(_ *cobra.Command, _ []string) error { return a.print(a.cfg) }})
	r.AddCommand(&cobra.Command{Use: "use NAME", Args: cobra.ExactArgs(1), Short: "Select default target", RunE: func(_ *cobra.Command, args []string) error {
		if _, ok := a.cfg.Targets[args[0]]; !ok {
			return &fault{2, "unknown target"}
		}
		a.cfg.Default = args[0]
		if err := store.Save(a.home, a.cfg); err != nil {
			return err
		}
		return a.print(map[string]string{"default_target": args[0]})
	}})
	r.AddCommand(&cobra.Command{Use: "remove NAME", Args: cobra.ExactArgs(1), Short: "Remove a target and its local credentials", RunE: func(_ *cobra.Command, args []string) error {
		t, ok := a.cfg.Targets[args[0]]
		if !ok {
			return &fault{2, "unknown target"}
		}
		id := store.Identity(t)
		if err := store.DeleteSession(a.home, id); err != nil {
			return err
		}
		if err := store.DeletePassword(a.home, id); err != nil {
			return err
		}
		delete(a.cfg.Targets, args[0])
		if a.cfg.Default == args[0] {
			a.cfg.Default = ""
		}
		if err := store.Save(a.home, a.cfg); err != nil {
			return err
		}
		return a.print(map[string]string{"removed": args[0]})
	}})
	return r
}
func (a *app) login() *cobra.Command {
	var rememberPassword bool
	cmd := &cobra.Command{Use: "login", Args: cobra.NoArgs, Short: "Log in and save an encrypted/keyring session", RunE: func(_ *cobra.Command, _ []string) error {
		c, err := a.newClient()
		if err != nil {
			return err
		}
		p, err := secret("ONEKVM_PASSWORD", "One-KVM password: ")
		if err != nil {
			return err
		}
		if err = c.Login(a.ctx, p, otp); err != nil {
			return err
		}
		b, err := json.Marshal(c.Session)
		if err != nil {
			return err
		}
		if err = store.SaveSession(a.home, store.Identity(c.Target), b); err != nil {
			if revokeErr := c.JSON(a.ctx, "POST", "/auth/logout", map[string]any{}, nil); revokeErr != nil {
				return &fault{3, "secure session storage unavailable; session was not saved and server revocation could not be confirmed. Use ONEKVM_PASSWORD per command"}
			}
			return &fault{3, "login succeeded but secure session storage is unavailable; session revoked. Use ONEKVM_PASSWORD per command instead"}
		}
		if rememberPassword {
			if err = store.SavePassword(a.home, store.Identity(c.Target), []byte(p)); err != nil {
				return &fault{3, "login succeeded but password could not be saved securely; automatic login is unavailable"}
			}
		}
		return a.print(map[string]any{"authenticated": true, "url": c.Target.URL, "user": c.Target.User, "session_storage": store.SessionBackend(), "password_saved_this_login": rememberPassword})
	}}
	cmd.Flags().BoolVar(&rememberPassword, "remember-password", false, "store the password in DPAPI/keyring for automatic login when the session expires")
	return cmd
}
func (a *app) logout() *cobra.Command {
	return &cobra.Command{Use: "logout", Args: cobra.NoArgs, Short: "Revoke saved session and remove local credentials", RunE: func(_ *cobra.Command, _ []string) error {
		c, err := a.newClient()
		if err != nil {
			return err
		}
		id := store.Identity(c.Target)
		b, loadErr := store.LoadSession(a.home, id)
		var revokeErr error
		revoked := false
		if loadErr == nil {
			if e := c.Restore(b); e == nil {
				revokeErr = c.JSON(a.ctx, "POST", "/auth/logout", map[string]any{}, nil)
				revoked = revokeErr == nil
			}
		}
		if err = store.DeleteSession(a.home, id); err != nil {
			return err
		}
		if err = store.DeletePassword(a.home, id); err != nil {
			return err
		}
		if revokeErr != nil && !client.IsAuth(revokeErr) {
			return &fault{1, "local session removed, but server revocation could not be confirmed"}
		}
		return a.print(map[string]any{"local_session_removed": true, "server_session_revoked": revoked})
	}}
}
func (a *app) status(capabilities bool) *cobra.Command {
	name := "status"
	desc := "Read server, video and HID status"
	if capabilities {
		name = "capabilities"
		desc = "List CLI capabilities and live availability"
	}
	return &cobra.Command{Use: name, Args: cobra.NoArgs, Short: desc, RunE: func(_ *cobra.Command, _ []string) error {
		c, err := a.connect()
		if err != nil {
			return err
		}
		var health, video map[string]any
		if err = c.JSON(a.ctx, "GET", "/health", nil, &health); err != nil {
			return err
		}
		h, err := c.HID(a.ctx)
		if err != nil {
			return err
		}
		if err = c.JSON(a.ctx, "GET", "/stream/status", nil, &video); err != nil {
			return err
		}
		result := map[string]any{"url": c.Target.URL, "server": health, "video": video, "hid": h}
		if capabilities {
			functions, functionErr := c.Functions(a.ctx, h)
			if functionErr != nil {
				result["function_detection_error"] = client.SafeError(functionErr, c.Session.Token)
			}
			ready := h.Available && h.Online && functionErr == nil
			result["hid_functions"] = functions
			_, keepAliveSupported := video["keep_alive"].(bool)
			result["capabilities"] = []map[string]any{
				{"command": "status", "available": true},
				{"command": "stream", "actions": map[string]bool{"start": keepAliveSupported, "stop": true, "status": true}, "start_mode": "mjpeg"},
				{"command": "snapshot", "supported": true, "availability": "capture-dependent; starts capture on demand"},
				{"command": "mouse", "actions": map[string]bool{"move": ready && functions.Absolute, "click": ready && functions.Absolute && functions.ButtonsAndScroll, "scroll": ready && functions.ButtonsAndScroll}},
				{"command": "key", "available": ready && functions.Keyboard, "chords": true},
				{"command": "type", "available": ready && functions.Keyboard, "layout": "US ASCII; LF and tab; Unicode rejected before sending"},
			}
			if h.Backend == "otg" && !h.PreservesPointer {
				result["pointer_caveat"] = "One-KVM 0.2.6 resets the absolute pointer to the origin when a HID connection closes. Use a single mouse click command with coordinates; standalone move is transient."
			}
			result["hid_delivery"] = "WebSocket transport only; execution must be verified from a subsequent screenshot"
			result["tested_api_version"] = "0.2.6"
		}
		return a.print(result)
	}}
}
func (a *app) stream() *cobra.Command {
	r := &cobra.Command{Use: "stream", Short: "Keep capture active until stopped, or inspect capture status", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	for _, action := range []string{"start", "stop", "status"} {
		r.AddCommand(&cobra.Command{Use: action, Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
			c, err := a.connect()
			if err != nil {
				return err
			}
			var state map[string]any
			if err = c.JSON(a.ctx, "GET", "/stream/status", nil, &state); err != nil {
				return err
			}
			if action == "status" {
				return a.print(state)
			}
			path := "/stream/" + action
			if action == "start" {
				if _, ok := state["keep_alive"].(bool); !ok {
					return errors.New("server does not support persistent capture; install the keep-alive server patch")
				}
				path += "?keep_alive=true"
			}
			var result map[string]any
			if err = c.JSON(a.ctx, "POST", path, nil, &result); err != nil {
				return err
			}
			if result["success"] != true {
				return errors.New("server did not confirm stream " + action)
			}
			if action == "start" && result["keep_alive"] != true {
				return errors.New("server did not confirm persistent capture")
			}
			return a.print(result)
		}})
	}
	return r
}

func (a *app) snapshot() *cobra.Command {
	var out string
	c := &cobra.Command{Use: "snapshot --out FILE", Args: cobra.NoArgs, Short: "Save a screenshot; start capture when needed", RunE: func(_ *cobra.Command, _ []string) error {
		if out == "" {
			return &fault{2, "--out is required"}
		}
		path, err := filepath.Abs(out)
		if err != nil {
			return err
		}
		if _, e := os.Stat(path); e == nil {
			return &fault{2, "output already exists; choose a new path"}
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		cl, err := a.connect()
		if err != nil {
			return err
		}
		b, err := cl.Snapshot(a.ctx)
		if err != nil {
			return err
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
		if err != nil {
			return errors.New("server did not return a valid JPEG/PNG screenshot")
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		ok := false
		defer func() {
			f.Close()
			if !ok {
				os.Remove(path)
			}
		}()
		if _, err = f.Write(b); err != nil {
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
		ok = true
		return a.print(map[string]any{"path": path, "format": format, "width": cfg.Width, "height": cfg.Height, "bytes": len(b)})
	}}
	c.Flags().StringVar(&out, "out", "", "explicit output file (parent must exist; never overwrites)")
	return c
}
func (a *app) send(frames [][]byte, needsAbs bool, delay time.Duration) error {
	c, err := a.connect()
	if err != nil {
		return err
	}
	h, err := c.HID(a.ctx)
	if err != nil {
		return err
	}
	if !h.Available || !h.Online {
		return &fault{4, "target HID is offline; connect the One-KVM OTG data cable to the controlled computer"}
	}
	if needsAbs && !h.Absolute {
		return &fault{4, "target does not support absolute mouse movement"}
	}
	functions, err := c.Functions(a.ctx, h)
	if err != nil {
		return err
	}
	for _, frame := range frames {
		if frame[0] == 1 && !functions.Keyboard {
			return &fault{4, "keyboard USB function is disabled"}
		}
		if frame[0] == 2 && frame[1] == 1 && !functions.Absolute {
			return &fault{4, "absolute mouse USB function is disabled"}
		}
		if frame[0] == 2 && frame[1] >= 2 && !functions.ButtonsAndScroll {
			return &fault{4, "target needs the relative mouse USB function or explicit absolute_mouse_buttons support for buttons and scroll"}
		}
	}
	if err = c.SendHID(a.ctx, frames, delay); err != nil {
		return err
	}
	result := map[string]any{"sent": true, "frames": len(frames), "execution_confirmed": false, "verification": "take a fresh screenshot to verify the result"}
	if h.Backend == "otg" && !h.PreservesPointer {
		result["pointer_caveat"] = "One-KVM resets the absolute pointer on disconnect; standalone movement does not persist"
	}
	return a.print(result)
}
func (a *app) key() *cobra.Command {
	return &cobra.Command{Use: "key CHORD", Example: "  onekvm key Ctrl+Shift+Esc\n  onekvm key Enter", Args: cobra.ExactArgs(1), Short: "Press and release a key or modifier chord", RunE: func(_ *cobra.Command, args []string) error {
		f, err := hid.Chord(args[0])
		if err != nil {
			return &fault{2, err.Error()}
		}
		return a.send(f, false, 30*time.Millisecond)
	}}
}
func (a *app) typing() *cobra.Command {
	var file string
	var stdin bool
	var delay int
	c := &cobra.Command{Use: "type [TEXT]", Args: cobra.MaximumNArgs(1), Short: "Type US-layout ASCII; accepts --file or --stdin", RunE: func(_ *cobra.Command, args []string) error {
		count := len(args)
		if file != "" {
			count++
		}
		if stdin {
			count++
		}
		if count != 1 {
			return &fault{2, "provide exactly one of TEXT, --file, --stdin"}
		}
		if delay < 5 || delay > 1000 {
			return &fault{2, "--delay-ms must be between 5 and 1000"}
		}
		var b []byte
		var err error
		if file != "" {
			f, e := os.Open(file)
			if e != nil {
				return e
			}
			defer f.Close()
			b, err = io.ReadAll(io.LimitReader(f, 4097))
		} else if stdin {
			b, err = io.ReadAll(io.LimitReader(os.Stdin, 4097))
		} else {
			b = []byte(args[0])
		}
		if err != nil {
			return err
		}
		if len(b) > 4096 {
			return &fault{2, "text exceeds 4096-byte limit"}
		}
		text := strings.ReplaceAll(string(b), "\r\n", "\n")
		frames, err := hid.Text(text)
		if err != nil {
			return &fault{2, err.Error()}
		}
		if len(frames) == 0 {
			return &fault{2, "text is empty"}
		}
		return a.send(frames, false, time.Duration(delay)*time.Millisecond)
	}}
	c.Flags().StringVar(&file, "file", "", "read text from file")
	c.Flags().BoolVar(&stdin, "stdin", false, "read text from stdin (use a saved session or environment credentials)")
	c.Flags().IntVar(&delay, "delay-ms", 20, "delay between key transitions")
	return c
}
func (a *app) mouse() *cobra.Command {
	r := &cobra.Command{Use: "mouse", Short: "Absolute movement, click and vertical scroll"}
	for _, action := range []string{"move", "click"} {
		action := action
		var x, y, w, h int
		var button string
		var twice bool
		c := &cobra.Command{Use: action, Args: cobra.NoArgs, Short: action + " using screenshot pixel coordinates", RunE: func(cmd *cobra.Command, _ []string) error {
			var frames [][]byte
			position := action == "move" || cmd.Flags().Changed("x") || cmd.Flags().Changed("y") || cmd.Flags().Changed("width") || cmd.Flags().Changed("height")
			if position {
				for _, f := range []string{"x", "y", "width", "height"} {
					if !cmd.Flags().Changed(f) {
						return &fault{2, "position requires --x --y --width --height from the screenshot"}
					}
				}
				frame, err := hid.Absolute(x, y, w, h)
				if err != nil {
					return &fault{2, err.Error()}
				}
				frames = append(frames, frame)
			}
			if action == "click" {
				b, err := hid.Button(button)
				if err != nil {
					return &fault{2, err.Error()}
				}
				n := 1
				if twice {
					n = 2
				}
				for i := 0; i < n; i++ {
					frames = append(frames, hid.Mouse(2, 0, 0, b), hid.Mouse(3, 0, 0, b))
				}
			}
			return a.send(frames, position, 40*time.Millisecond)
		}}
		c.Flags().IntVar(&x, "x", 0, "x in screenshot pixels")
		c.Flags().IntVar(&y, "y", 0, "y in screenshot pixels")
		c.Flags().IntVar(&w, "width", 0, "screenshot width")
		c.Flags().IntVar(&h, "height", 0, "screenshot height")
		if action == "click" {
			c.Flags().StringVar(&button, "button", "left", "left, middle or right")
			c.Flags().BoolVar(&twice, "double", false, "double click")
		}
		r.AddCommand(c)
	}
	var delta int
	c := &cobra.Command{Use: "scroll --delta N", Args: cobra.NoArgs, Short: "Scroll vertically; positive is up, negative down", RunE: func(_ *cobra.Command, _ []string) error {
		if delta == 0 || delta < -127 || delta > 127 {
			return &fault{2, "--delta must be -127..-1 or 1..127"}
		}
		return a.send([][]byte{hid.Mouse(4, 0, 0, byte(int8(delta)))}, false, 30*time.Millisecond)
	}}
	c.Flags().IntVar(&delta, "delta", 0, "wheel steps")
	r.AddCommand(c)
	return r
}
