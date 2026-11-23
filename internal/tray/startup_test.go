package tray

import (
	"errors"
	"testing"
)

func TestStartupTaskStateOn(t *testing.T) {
	for state, want := range map[startupTaskState]bool{
		taskDisabled:         false,
		taskDisabledByUser:   false,
		taskEnabled:          true,
		taskDisabledByPolicy: false,
		taskEnabledByPolicy:  true,
	} {
		if got := state.on(); got != want {
			t.Errorf("state %d: on() = %v, want %v", state, got, want)
		}
	}
}

func TestStartupResult(t *testing.T) {
	tests := []struct {
		name    string
		want    bool
		got     startupTaskState
		wantErr error
	}{
		{"enabled as asked", true, taskEnabled, nil},
		{"disabled as asked", false, taskDisabled, nil},
		{"enabled by policy counts as on", true, taskEnabledByPolicy, nil},
		{"user turned it off in Settings", true, taskDisabledByUser, errStartupInSettings},
		{"policy keeps it off", true, taskDisabledByPolicy, errStartupByPolicy},
		{"policy keeps it on", false, taskEnabledByPolicy, errStartupByPolicy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := startupResult(tt.want, tt.got)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("got %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
		})
	}
	if err := startupResult(true, taskDisabled); err == nil {
		t.Fatal("a task still disabled after asking for it on: got nil error")
	}
}
