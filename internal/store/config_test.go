package store

import "testing"

func TestTargetSelectionAndSessionBinding(t *testing.T) {
	c := Config{Default: "home", Targets: map[string]Target{"home": {URL: "http://home:8080", User: "a"}, "usb": {URL: "http://usb:8080", User: "b"}}}
	got, err := Resolve(c, "usb", "", "", "http://env:8080", "env")
	if err != nil || got != c.Targets["usb"] {
		t.Fatalf("explicit target: %+v %v", got, err)
	}
	got, err = Resolve(c, "", "http://override:8080", "", "http://env:8080", "env")
	if err != nil || got.URL != "http://override:8080" || got.User != "env" {
		t.Fatalf("precedence: %+v %v", got, err)
	}
	if Identity(got) == Identity(c.Targets["home"]) {
		t.Fatal("different target shares credentials")
	}
	for _, raw := range []string{"http://user:pass@host", "file:///tmp", "http://host/path", "http://host/?secret=x", "http://host/#x"} {
		if _, err := NormalizeURL(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	dir := t.TempDir()
	if err := Save(dir, c); err != nil {
		t.Fatal(err)
	}
	c.Default = "usb"
	if err := Save(dir, c); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil || loaded.Default != "usb" {
		t.Fatalf("config update failed %v", err)
	}
}
