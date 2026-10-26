package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func TestWindowsConfirmationsMapOnlyExplicitYes(t *testing.T) {
	for _, labels := range [][2]string{{"Cancel", "Start sending"}, {"Cancel", "Remove connection"}, {"Keep open", "Pause and quit"}} {
		for _, answer := range []string{"Yes", "No", "Cancel", "", "Error", "Start sending", "yes"} {
			t.Run(labels[1]+"/"+answer, func(t *testing.T) {
				options := runtime.MessageDialogOptions{Type: runtime.QuestionDialog, Title: "Confirm", Message: "Synthetic confirmation", Buttons: []string{labels[0], labels[1]}, DefaultButton: labels[0], CancelButton: labels[0]}
				got, err := platformMessageDialog(context.Background(), "windows", options, func(_ context.Context, o runtime.MessageDialogOptions) (string, error) {
					if o.DefaultButton != "No" || o.CancelButton != "No" || !reflect.DeepEqual(o.Buttons, []string{"No", "Yes"}) || o.Title != options.Title || o.Message != options.Message {
						t.Fatal("unsafe Windows dialog options", o)
					}
					return answer, nil
				})
				switch answer {
				case "Yes":
					if err != nil || got != labels[1] {
						t.Fatal("confirmation lost", got, err)
					}
				case "No", "Cancel":
					if err != nil || got != labels[0] {
						t.Fatal("cancellation lost", got, err)
					}
				default:
					if err == nil || got != "" {
						t.Fatal("unexpected response authorized", got, err)
					}
				}
				if options.DefaultButton != labels[0] || options.Buttons[0] != labels[0] {
					t.Fatal("caller options mutated")
				}
			})
		}
	}
}

func TestMessageDialogPreservesMacAndInformationalResponses(t *testing.T) {
	for _, tc := range []struct {
		platform string
		kind     runtime.DialogType
		answer   string
	}{
		{"darwin", runtime.QuestionDialog, "Start sending"}, {"darwin", runtime.QuestionDialog, "Cancel"}, {"windows", runtime.InfoDialog, "Ok"},
	} {
		options := runtime.MessageDialogOptions{Type: tc.kind, Buttons: []string{"Cancel", "Start sending"}, DefaultButton: "Cancel", CancelButton: "Cancel"}
		got, err := platformMessageDialog(context.Background(), tc.platform, options, func(_ context.Context, o runtime.MessageDialogOptions) (string, error) {
			if !reflect.DeepEqual(o, options) {
				t.Fatal("dialog unexpectedly changed")
			}
			return tc.answer, nil
		})
		if err != nil || got != tc.answer {
			t.Fatal(got, err)
		}
	}
}

func TestWindowsDialogErrorsNeverAuthorize(t *testing.T) {
	options := runtime.MessageDialogOptions{Type: runtime.QuestionDialog, Buttons: []string{"Cancel", "Start sending"}, CancelButton: "Cancel"}
	failure := errors.New("native dialog failed")
	got, err := platformMessageDialog(context.Background(), "windows", options, func(context.Context, runtime.MessageDialogOptions) (string, error) { return "Yes", failure })
	if !errors.Is(err, failure) || got != "" {
		t.Fatal(got, err)
	}
	for _, invalid := range []runtime.MessageDialogOptions{
		{Type: runtime.QuestionDialog},
		{Type: runtime.QuestionDialog, Buttons: []string{"Cancel", ""}, CancelButton: "Cancel"},
		{Type: runtime.QuestionDialog, Buttons: []string{"Cancel", "Cancel"}, CancelButton: "Cancel"},
		{Type: runtime.QuestionDialog, Buttons: []string{"No", "Yes"}, CancelButton: "missing"},
	} {
		if _, err := platformMessageDialog(context.Background(), "windows", invalid, func(context.Context, runtime.MessageDialogOptions) (string, error) {
			t.Fatal("invalid dialog opened")
			return "Yes", nil
		}); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
}

func TestWindowsStartConfirmationThroughDesktopBoundary(t *testing.T) {
	for _, answer := range []string{"Yes", "No", "Cancel", "Error", ""} {
		t.Run(answer, func(t *testing.T) {
			a, _, sender := desktopFixture(t)
			job, err := a.CreateJob("Windows confirmation")
			if err != nil {
				t.Fatal(err)
			}
			r := desktopRequest(job)
			if _, err = a.ChooseCSV(job.ID, false); err != nil {
				t.Fatal(err)
			}
			if err = a.CheckConnection(job.ID, r.Config); err != nil {
				t.Fatal(err)
			}
			token, err := a.SendTest(job.ID, r, 0, "test@example.org")
			if err != nil {
				t.Fatal(err)
			}
			a.services.message = func(ctx context.Context, o runtime.MessageDialogOptions) (string, error) {
				return platformMessageDialog(ctx, "windows", o, func(_ context.Context, native runtime.MessageDialogOptions) (string, error) {
					if native.DefaultButton != "No" {
						t.Fatal("sending selected by default")
					}
					return answer, nil
				})
			}
			err = a.Start(job.ID, r, token)
			if answer == "Yes" {
				if err != nil {
					t.Fatal(err)
				}
				waitDesktop(t, a, job.ID)
				if err = a.Start(job.ID, r, token); err != nil {
					t.Fatal(err)
				}
				waitDesktop(t, a, job.ID)
			} else if err == nil {
				t.Fatal("sending accepted without confirmation")
			}
			sender.mu.Lock()
			count := len(sender.sent)
			sender.mu.Unlock()
			want := 1 // Only the explicitly requested test email.
			if answer == "Yes" {
				want += 3
			}
			if count != want {
				t.Fatalf("sent %d messages, want %d", count, want)
			}
		})
	}
}
