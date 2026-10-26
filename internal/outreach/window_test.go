package outreach

import (
	"path/filepath"
	"testing"
)

func TestWindowSizePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "window.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	if size, err := s.LoadWindowSize(); err != nil || size != (WindowSize{}) {
		t.Fatal(size, err)
	}
	want := WindowSize{1100, 750}
	if err := s.SaveWindowSize(want); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []WindowSize{{0, 1}, {1, -1}, {20000, 900}} {
		if s.SaveWindowSize(invalid) == nil {
			t.Fatal("invalid size accepted", invalid)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if size, err := s.LoadWindowSize(); err != nil || size != want {
		t.Fatal(size, err)
	}
	for _, corrupt := range []string{"bad JSON", `{"width":0,"height":900}`} {
		if _, err := s.db.Exec("UPDATE meta SET value=? WHERE key='window_size'", corrupt); err != nil {
			t.Fatal(err)
		}
		if _, err := s.LoadWindowSize(); err == nil {
			t.Fatal("corrupt geometry accepted")
		}
	}
	s.Close()
	if _, err := s.LoadWindowSize(); err == nil {
		t.Fatal("closed read accepted")
	}
	if err := s.SaveWindowSize(want); err == nil {
		t.Fatal("closed write accepted")
	}
}
