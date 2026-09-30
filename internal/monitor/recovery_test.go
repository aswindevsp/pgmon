package monitor

import "testing"

func TestParseRedoLSN(t *testing.T) {
	out := []byte("Latest checkpoint location:           9C3/99000000\nLatest checkpoint's REDO location:    9C3/93000028\n")
	if got := ParseRedoLSN(out); got != "9C3/93000028" {
		t.Fatal(got)
	}
}
