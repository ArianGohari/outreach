package main

import (
	"context"
	"fmt"
	"outreach/internal/outreach"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Wails screen sizes are logical pixels, including on Retina and scaled Windows
// displays. Leave room for the title bar, Dock/taskbar and screen edges.
func fittedWindow(saved outreach.WindowSize, screens []runtime.Screen) (size, minimum outreach.WindowSize) {
	width, height, priority := 1280, 900, -1
	for _, screen := range screens {
		if screen.Size.Width <= 0 || screen.Size.Height <= 0 {
			continue
		}
		rank := 0
		if screen.IsPrimary {
			rank = 1
		}
		if screen.IsCurrent {
			rank = 2
		}
		if rank > priority {
			width, height, priority = screen.Size.Width, screen.Size.Height, rank
		}
	}
	available := outreach.WindowSize{Width: max(1, width-64), Height: max(1, height-96)}
	minimum = outreach.WindowSize{Width: min(640, available.Width), Height: min(480, available.Height)}
	if !saved.Valid() {
		saved = outreach.WindowSize{Width: min(1080, width*82/100), Height: min(900, height*85/100)}
	}
	size = outreach.WindowSize{
		Width:  max(minimum.Width, min(saved.Width, available.Width)),
		Height: max(minimum.Height, min(saved.Height, available.Height)),
	}
	return
}

func (a *App) restoreWindow(ctx context.Context) {
	d := a.desktop()
	screens, err := d.screens(ctx)
	if err != nil || len(screens) == 0 {
		if err == nil {
			err = fmt.Errorf("No display information available; using the default window size")
		}
		a.notifyProblem(a.reporter.Report("Read display size", err))
	}
	var saved outreach.WindowSize
	if a.store != nil {
		saved, err = a.store.LoadWindowSize()
		if err != nil {
			a.notifyProblem(a.reporter.Report("Restore window size", err))
		}
	}
	size, minimum := fittedWindow(saved, screens)
	d.setMinSize(ctx, minimum.Width, minimum.Height)
	d.setSize(ctx, size.Width, size.Height)
	d.center(ctx)
}

func (a *App) saveWindow(ctx context.Context) {
	d := a.desktop()
	if a.ctx == nil || a.store == nil || d.getSize == nil || d.isNormal == nil || !d.isNormal(ctx) {
		return
	}
	width, height := d.getSize(ctx)
	if err := a.store.SaveWindowSize(outreach.WindowSize{Width: width, Height: height}); err != nil {
		a.notifyProblem(a.reporter.Report("Save window size", err))
	}
}
