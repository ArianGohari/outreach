package main

import (
	"context"
	"errors"
	"outreach/internal/diagnostics"
	"outreach/internal/outreach"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func display(width, height int, current, primary bool) runtime.Screen {
	s := runtime.Screen{IsCurrent: current, IsPrimary: primary}
	s.Size.Width, s.Size.Height = width, height
	// Physical pixels must never be used for sizing a window.
	s.PhysicalSize.Width, s.PhysicalSize.Height = width*2, height*2
	return s
}

func TestWindowFitsLogicalDisplay(t *testing.T) {
	for _, tc := range []struct {
		name    string
		saved   outreach.WindowSize
		screens []runtime.Screen
		want    outreach.WindowSize
	}{
		{"laptop", outreach.WindowSize{}, []runtime.Screen{display(1366, 768, true, true)}, outreach.WindowSize{Width: 1080, Height: 652}},
		{"retina", outreach.WindowSize{}, []runtime.Screen{display(1512, 982, true, true)}, outreach.WindowSize{Width: 1080, Height: 834}},
		{"large", outreach.WindowSize{}, []runtime.Screen{display(2560, 1440, true, true)}, outreach.WindowSize{Width: 1080, Height: 900}},
		{"restore", outreach.WindowSize{Width: 900, Height: 700}, []runtime.Screen{display(1512, 982, true, true)}, outreach.WindowSize{Width: 900, Height: 700}},
		{"smaller monitor", outreach.WindowSize{Width: 2200, Height: 1300}, []runtime.Screen{display(1280, 720, true, true)}, outreach.WindowSize{Width: 1216, Height: 624}},
		{"current before primary", outreach.WindowSize{}, []runtime.Screen{display(2560, 1440, false, true), display(1024, 768, true, false)}, outreach.WindowSize{Width: 839, Height: 652}},
		{"primary fallback", outreach.WindowSize{}, []runtime.Screen{display(1024, 768, false, false), display(2560, 1440, false, true)}, outreach.WindowSize{Width: 1080, Height: 900}},
		{"invalid displays", outreach.WindowSize{}, []runtime.Screen{display(0, 0, true, true)}, outreach.WindowSize{Width: 1049, Height: 765}},
		{"no displays", outreach.WindowSize{}, nil, outreach.WindowSize{Width: 1049, Height: 765}},
		{"invalid saved size", outreach.WindowSize{Width: -1, Height: 700}, []runtime.Screen{display(1366, 768, true, true)}, outreach.WindowSize{Width: 1080, Height: 652}},
		{"minimum", outreach.WindowSize{Width: 1, Height: 1}, []runtime.Screen{display(1366, 768, true, true)}, outreach.WindowSize{Width: 640, Height: 480}},
		{"small logical display", outreach.WindowSize{}, []runtime.Screen{display(600, 500, true, true)}, outreach.WindowSize{Width: 536, Height: 404}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, minimum := fittedWindow(tc.saved, tc.screens)
			if got != tc.want || minimum.Width > got.Width || minimum.Height > got.Height {
				t.Fatalf("got %+v (minimum %+v), want %+v", got, minimum, tc.want)
			}
		})
	}
}

func TestWindowRestoreAndConfirmedClose(t *testing.T) {
	a, store, _ := desktopFixture(t)
	ctx := context.Background()
	if err := store.SaveWindowSize(outreach.WindowSize{Width: 920, Height: 710}); err != nil {
		t.Fatal(err)
	}
	var applied outreach.WindowSize
	centered := false
	a.services.screens = func(context.Context) ([]runtime.Screen, error) {
		return []runtime.Screen{display(1440, 900, true, true)}, nil
	}
	a.services.setSize = func(_ context.Context, w, h int) { applied = outreach.WindowSize{Width: w, Height: h} }
	a.services.setMinSize = func(_ context.Context, w, h int) {
		if w != 640 || h != 480 {
			t.Errorf("minimum %d x %d", w, h)
		}
	}
	a.services.center = func(context.Context) { centered = true }
	a.services.isNormal = func(context.Context) bool { return true }
	a.services.getSize = func(context.Context) (int, int) { return 1110, 750 }
	a.restoreWindow(ctx)
	if applied != (outreach.WindowSize{Width: 920, Height: 710}) || !centered {
		t.Fatal(applied, centered)
	}
	if a.beforeClose(ctx) {
		t.Fatal("idle close blocked")
	}
	size, err := store.LoadWindowSize()
	if err != nil || size != (outreach.WindowSize{Width: 1110, Height: 750}) {
		t.Fatal(size, err)
	}
	// Maximised/fullscreen/minimised windows must not overwrite normal geometry.
	a.services.isNormal = func(context.Context) bool { return false }
	a.services.getSize = func(context.Context) (int, int) { t.Fatal("read non-normal size"); return 0, 0 }
	a.saveWindow(ctx)
	size, err = store.LoadWindowSize()
	if err != nil || size != (outreach.WindowSize{Width: 1110, Height: 750}) {
		t.Fatal(size, err)
	}
}

func TestWindowFailuresAreLogged(t *testing.T) {
	a, store, _ := desktopFixture(t)
	ctx := context.Background()
	a.services.screens = func(context.Context) ([]runtime.Screen, error) { return nil, errors.New("display unavailable") }
	a.services.setSize = func(context.Context, int, int) {}
	a.services.setMinSize = func(context.Context, int, int) {}
	a.services.center = func(context.Context) {}
	a.services.isNormal = func(context.Context) bool { return true }
	a.services.getSize = func(context.Context) (int, int) { return 900, 700 }
	var operations []string
	a.services.emit = func(_ context.Context, name string, args ...interface{}) {
		if name == "outreach:error" {
			p := args[0].(*diagnostics.Problem)
			if !p.Logged {
				t.Error("window failure was not logged", p)
			}
			operations = append(operations, p.Operation)
		}
	}
	store.Close()
	a.restoreWindow(ctx)
	a.saveWindow(ctx)
	if len(operations) != 3 {
		t.Fatal("missing error notifications", operations)
	}
}
