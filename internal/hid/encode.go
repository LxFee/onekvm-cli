package hid

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
)

var keys = map[string]byte{"enter": 40, "esc": 41, "escape": 41, "backspace": 42, "tab": 43, "space": 44, "minus": 45, "equal": 46, "leftbracket": 47, "rightbracket": 48, "backslash": 49, "semicolon": 51, "quote": 52, "backquote": 53, "comma": 54, "period": 55, "slash": 56, "capslock": 57, "printscreen": 70, "scrolllock": 71, "pause": 72, "insert": 73, "home": 74, "pageup": 75, "delete": 76, "end": 77, "pagedown": 78, "right": 79, "left": 80, "down": 81, "up": 82, "numlock": 83}

func KeyCode(s string) (byte, error) {
	s = strings.ToLower(s)
	if len(s) == 1 {
		if s[0] >= 'a' && s[0] <= 'z' {
			return s[0] - 'a' + 4, nil
		}
		if s[0] >= '1' && s[0] <= '9' {
			return s[0] - '1' + 30, nil
		}
		if s == "0" {
			return 39, nil
		}
	}
	if strings.HasPrefix(s, "f") {
		n, err := strconv.Atoi(s[1:])
		if err == nil && n >= 1 && n <= 12 {
			return byte(57 + n), nil
		}
	}
	if k, ok := keys[s]; ok {
		return k, nil
	}
	return 0, fmt.Errorf("unsupported key %q", s)
}
func Chord(s string) ([][]byte, error) {
	parts := strings.Split(s, "+")
	var mods byte
	for _, m := range parts[:len(parts)-1] {
		switch strings.ToLower(m) {
		case "ctrl", "control":
			mods |= 1
		case "shift":
			mods |= 2
		case "alt":
			mods |= 4
		case "meta", "win", "super", "cmd":
			mods |= 8
		default:
			return nil, fmt.Errorf("unknown modifier %q", m)
		}
	}
	k, err := KeyCode(parts[len(parts)-1])
	if err != nil {
		return nil, err
	}
	return [][]byte{{1, 0, k, mods}, {1, 1, k, 0}}, nil
}

// US physical keyboard mapping. Unicode needs a target-specific input method,
// so unsupported text is rejected in full before any key is sent.
func Text(s string) ([][]byte, error) {
	var frames [][]byte
	for _, r := range s {
		var k, mod byte
		switch {
		case r >= 'a' && r <= 'z':
			k = byte(r - 'a' + 4)
		case r >= 'A' && r <= 'Z':
			k = byte(r - 'A' + 4)
			mod = 2
		case r >= '1' && r <= '9':
			k = byte(r - '1' + 30)
		case r == '0':
			k = 39
		case r == '\n':
			k = 40
		case r == '\t':
			k = 43
		case r == ' ':
			k = 44
		default:
			plain := "-=[]\\;'`,./"
			shift := "_+{}|:\"~<>?"
			codes := []byte{45, 46, 47, 48, 49, 51, 52, 53, 54, 55, 56}
			if i := strings.IndexRune(plain, r); i >= 0 {
				k = codes[i]
			} else if i = strings.IndexRune(shift, r); i >= 0 {
				k = codes[i]
				mod = 2
			} else if i = strings.IndexRune("!@#$%^&*()", r); i >= 0 {
				k = byte(30 + i)
				mod = 2
			} else {
				return nil, fmt.Errorf("unsupported character U+%04X; type supports US-keyboard ASCII, LF and tab only", r)
			}
		}
		frames = append(frames, []byte{1, 0, k, mod}, []byte{1, 1, k, 0})
	}
	return frames, nil
}
func Mouse(event byte, x, y int, last byte) []byte {
	b := []byte{2, event, 0, 0, 0, 0, last}
	binary.LittleEndian.PutUint16(b[2:4], uint16(x))
	binary.LittleEndian.PutUint16(b[4:6], uint16(y))
	return b
}
func Absolute(x, y, w, h int) ([]byte, error) {
	if w < 2 || h < 2 || x < 0 || y < 0 || x >= w || y >= h {
		return nil, fmt.Errorf("coordinates must be inside %dx%d image", w, h)
	}
	return Mouse(1, int(math.Round(float64(x)*32767/float64(w-1))), int(math.Round(float64(y)*32767/float64(h-1))), 0), nil
}
func Button(s string) (byte, error) {
	switch s {
	case "left":
		return 0, nil
	case "middle":
		return 1, nil
	case "right":
		return 2, nil
	default:
		return 0, fmt.Errorf("button must be left, middle or right")
	}
}
