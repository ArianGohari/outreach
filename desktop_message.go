package main

import (
	"context"
	"fmt"
	goruntime "runtime"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func showMessageDialog(ctx context.Context, options runtime.MessageDialogOptions) (string, error) {
	return platformMessageDialog(ctx, goruntime.GOOS, options, runtime.MessageDialog)
}

// Wails 2 ignores custom question buttons on Windows and returns fixed English
// Yes/No values, even on localized systems. Keep cancellation the default and
// translate only an explicit Yes into the action label expected by callers.
func platformMessageDialog(ctx context.Context, platform string, options runtime.MessageDialogOptions, show func(context.Context, runtime.MessageDialogOptions) (string, error)) (string, error) {
	if platform != "windows" || options.Type != runtime.QuestionDialog {
		return show(ctx, options)
	}
	if len(options.Buttons) != 2 || options.CancelButton == "" || options.Buttons[0] == options.Buttons[1] {
		return "", fmt.Errorf("Confirmation dialog requires an action and a cancel button")
	}
	var action string
	switch options.CancelButton {
	case options.Buttons[0]:
		action = options.Buttons[1]
	case options.Buttons[1]:
		action = options.Buttons[0]
	default:
		return "", fmt.Errorf("Confirmation dialog cancel button is missing")
	}
	if action == "" {
		return "", fmt.Errorf("Confirmation dialog action is missing")
	}
	windowsOptions := options
	windowsOptions.Buttons = []string{"No", "Yes"}
	windowsOptions.DefaultButton = "No"
	windowsOptions.CancelButton = "No"
	answer, err := show(ctx, windowsOptions)
	if err != nil {
		return "", err
	}
	switch answer {
	case "Yes":
		return action, nil
	case "No", "Cancel":
		return options.CancelButton, nil
	default:
		return "", fmt.Errorf("Windows did not return a recognized confirmation response. No action was authorized")
	}
}
