package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

type Target struct {
	URL  string `json:"url"`
	User string `json:"user"`
}
type Config struct {
	Default string            `json:"default_target"`
	Targets map[string]Target `json:"targets"`
}

func Home() (string, error) {
	if h := os.Getenv("ONEKVM_HOME"); h != "" {
		return filepath.Abs(h)
	}
	if runtime.GOOS == "windows" {
		if h := os.Getenv("LOCALAPPDATA"); h != "" {
			return filepath.Join(h, "onekvm"), nil
		}
		return "", errors.New("LOCALAPPDATA is unset; set ONEKVM_HOME")
	}
	h, err := os.UserConfigDir()
	return filepath.Join(h, "onekvm"), err
}
func Load(home string) (Config, error) {
	c := Config{Targets: map[string]Target{}}
	b, err := os.ReadFile(filepath.Join(home, "config.json"))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(b, &c)
	if c.Targets == nil {
		c.Targets = map[string]Target{}
	}
	return c, err
}
func Save(home string, c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWrite(filepath.Join(home, "config.json"), append(b, '\n'))
}
func AtomicWrite(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".onekvm-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if _, err = f.Write(b); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func NormalizeURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", errors.New("invalid target URL")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("URL must be an http(s) origin, e.g. http://host:8080; credentials, paths, queries and fragments are not allowed")
	}
	u.Path = ""
	u.Host = strings.ToLower(u.Host)
	return u.String(), nil
}
func ValidName(name string) bool {
	return regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`).MatchString(name)
}

// Session identity includes origin and user, so aliases or URL overrides cannot leak credentials.
func Identity(t Target) string {
	s := sha256.Sum256([]byte(t.URL + "\x00" + t.User))
	return hex.EncodeToString(s[:])
}
func Resolve(c Config, name, raw, user, envURL, envUser string) (Target, error) {
	t := Target{}
	if name != "" {
		var ok bool
		t, ok = c.Targets[name]
		if !ok {
			return t, fmt.Errorf("unknown target %q", name)
		}
	} else {
		t = c.Targets[c.Default]
		if envURL != "" {
			t.URL = envURL
		}
		if envUser != "" {
			t.User = envUser
		}
	}
	if raw != "" {
		t.URL = raw
	}
	if user != "" {
		t.User = user
	}
	if t.URL == "" {
		return t, errors.New("no target configured; use target add NAME --url URL --user USER")
	}
	var err error
	t.URL, err = NormalizeURL(t.URL)
	if err != nil {
		return t, err
	}
	if strings.TrimSpace(t.User) == "" {
		return t, errors.New("username required: --user or ONEKVM_USER")
	}
	return t, nil
}
