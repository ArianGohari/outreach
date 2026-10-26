//go:build nativee2e && integration

package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"net"
	"os"
	"outreach/internal/diagnostics"
	"outreach/internal/outreach"
	"outreach/internal/testsupport"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"time"
)

//go:embed scripts/native-e2e.js
var nativeScript string

// This executable is test-only. It never opens the user's database or SMTP endpoints.
func main() {
	if len(os.Args) != 2 {
		panic("native test requires an isolated output directory")
	}
	dir := os.Args[1]
	if e := os.MkdirAll(dir, 0700); e != nil {
		panic(e)
	}
	sink, e := testsupport.NewSMTP()
	if e != nil {
		panic(e)
	}
	defer sink.Close()
	store, e := outreach.OpenStore(filepath.Join(dir, "native.db"))
	if e != nil {
		panic(e)
	}
	defer store.Close()
	log, e := diagnostics.OpenLog(filepath.Join(dir, "logs"))
	if e != nil {
		panic(e)
	}
	a, e := newApp(store, outreach.IntegrationSender(sink.Roots, sink.Listener.Addr().String()), outreach.RealClock{}, diagnostics.NewReporter(log))
	if e != nil {
		panic(e)
	}
	csv := filepath.Join(dir, "synthetic.csv")
	if e = os.WriteFile(csv, []byte("First Name,Last Name,Title,Company Name,Email,Contact Owner,Person Linkedin Url,City\nLena,Example,Lead,Example,lena@example.org,Owner,,Berlin\nMax,Example,Lead,Example,max@example.org,Owner,,London\n"), 0600); e != nil {
		panic(e)
	}
	// OS selection/confirmation is deterministic here; the real WebView, bridge, file reads,
	// validation, database and TLS SMTP transport remain in use.
	services := nativeServices
	services.openFile = func(context.Context, runtime.OpenDialogOptions) (string, error) { return csv, nil }
	services.message = func(ctx context.Context, o runtime.MessageDialogOptions) (string, error) {
		return platformMessageDialog(ctx, goruntime.GOOS, o, func(_ context.Context, native runtime.MessageDialogOptions) (string, error) {
			if goruntime.GOOS == "windows" {
				return "Yes", nil
			}
			return native.Buttons[len(native.Buttons)-1], nil
		})
	}
	services.deletePassword = func(string, string) error { return nil }
	a.services = &services
	opts := desktopOptions(a)
	opts.Title = "Outreach Integration Test"
	var once sync.Once
	finish := func(ctx context.Context, result string) {
		once.Do(func() {
			a.shutdown(ctx)
			a.saveWindow(ctx)
			savedSize, sizeErr := a.store.LoadWindowSize()
			if sizeErr != nil || !savedSize.Valid() {
				result = fmt.Sprintf("Window size was not persisted: %+v (%v)", savedSize, sizeErr)
			}
			messages, recipients := sink.Snapshot()
			payload, _ := json.Marshal(map[string]interface{}{"result": result, "messages": messages, "recipients": recipients})
			if e := os.WriteFile(filepath.Join(dir, "result.json"), payload, 0600); e != nil {
				fmt.Fprintln(os.Stderr, e)
			}
			runtime.Quit(ctx)
		})
	}
	opts.OnStartup = func(ctx context.Context) {
		a.startup(ctx)
		runtime.EventsOn(ctx, "native-test-result", func(data ...interface{}) {
			if len(data) == 1 {
				finish(ctx, fmt.Sprint(data[0]))
			}
		})
		go func() { time.Sleep(90 * time.Second); finish(ctx, "native test timed out") }()
	}
	opts.OnDomReady = func(ctx context.Context) {
		a.restoreWindow(ctx)
		cfg, _ := json.Marshal(map[string]interface{}{"port": sink.Listener.Addr().(*net.TCPAddr).Port})
		runtime.WindowExecJS(ctx, "window.__nativeTest="+string(cfg)+";"+nativeScript)
	}
	if e = wails.Run(opts); e != nil {
		panic(e)
	}
}
