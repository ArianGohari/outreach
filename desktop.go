package main

import (
	"context"
	"embed"
	"github.com/pkg/browser"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/zalando/go-keyring"
)

//go:embed all:frontend/dist
var assets embed.FS

// ponytail: function fields isolate OS calls without replacing the Wails bridge or app logic.
type desktopServices struct {
	openFile       func(context.Context, runtime.OpenDialogOptions) (string, error)
	message        func(context.Context, runtime.MessageDialogOptions) (string, error)
	setPassword    func(string, string, string) error
	getPassword    func(string, string) (string, error)
	deletePassword func(string, string) error
	emit           func(context.Context, string, ...interface{})
	openBrowser    func(string) error
	screens        func(context.Context) ([]runtime.Screen, error)
	getSize        func(context.Context) (int, int)
	setSize        func(context.Context, int, int)
	setMinSize     func(context.Context, int, int)
	center         func(context.Context)
	isNormal       func(context.Context) bool
}

var nativeServices = desktopServices{
	openFile: runtime.OpenFileDialog, message: runtime.MessageDialog,
	setPassword: keyring.Set, getPassword: keyring.Get, deletePassword: keyring.Delete,
	emit:        runtime.EventsEmit,
	openBrowser: browser.OpenURL,
	screens:     runtime.ScreenGetAll, getSize: runtime.WindowGetSize, setSize: runtime.WindowSetSize,
	setMinSize: runtime.WindowSetMinSize, center: runtime.WindowCenter, isNormal: runtime.WindowIsNormal,
}

func (a *App) desktop() *desktopServices {
	if a.services != nil {
		return a.services
	}
	return &nativeServices
}
func desktopOptions(app *App) *options.App {
	return &options.App{Title: "Outreach", Width: 1000, Height: 760, MinWidth: 640, MinHeight: 480,
		AssetServer: &assetserver.Options{Assets: assets}, BackgroundColour: &options.RGBA{R: 255, G: 255, B: 255, A: 255},
		OnDomReady: app.restoreWindow, OnStartup: app.startup, OnShutdown: app.shutdown, OnBeforeClose: app.beforeClose, Bind: []interface{}{app},
		ErrorFormatter: app.formatError, Logger: &frameworkLogger{reporter: app.reporter, notify: app.notifyProblem}}
}
