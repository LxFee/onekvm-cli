package hid

import (
	"bytes"
	"reflect"
	"testing"
)

func TestProtocolVectors(t *testing.T) {
	f, err := Chord("Ctrl+Shift+Esc")
	if err != nil || !reflect.DeepEqual(f, [][]byte{{1, 0, 41, 3}, {1, 1, 41, 0}}) {
		t.Fatalf("chord %v %v", f, err)
	}
	b, err := Absolute(1919, 1079, 1920, 1080)
	if err != nil || !bytes.Equal(b, []byte{2, 1, 255, 127, 255, 127, 0}) {
		t.Fatalf("absolute %v %v", b, err)
	}
	if _, err = Absolute(1920, 0, 1920, 1080); err == nil {
		t.Fatal("out-of-bounds position accepted")
	}
	if !bytes.Equal(Mouse(4, 0, 0, 255), []byte{2, 4, 0, 0, 0, 0, 255}) {
		t.Fatal("negative scroll encoding")
	}
	f, err = Text("A0!\n")
	if err != nil || !reflect.DeepEqual(f, [][]byte{{1, 0, 4, 2}, {1, 1, 4, 0}, {1, 0, 39, 0}, {1, 1, 39, 0}, {1, 0, 30, 2}, {1, 1, 30, 0}, {1, 0, 40, 0}, {1, 1, 40, 0}}) {
		t.Fatalf("text %v %v", f, err)
	}
	if f, err = Text("abc中文"); err == nil || f != nil {
		t.Fatal("must reject entire unsupported input before sending")
	}
}
