package tray

import (
	"os"
	"testing"
)

// TestPackagedStartupTask switches the real StartupTask, so it only runs
// inside the installed package, which a test binary is not unless started
// there, e.g. from PowerShell with Developer Mode on:
//
//	Invoke-CommandInDesktopPackage -PackageFamilyName <family> -AppId Tray `
//	  -Command cmd.exe -Args '/c set SSHUSH_TEST_PACKAGED=1&& tray.test.exe -test.run PackagedStartupTask -test.v > out.txt 2>&1'
//
// It leaves the task as it found it.
func TestPackagedStartupTask(t *testing.T) {
	if os.Getenv("SSHUSH_TEST_PACKAGED") == "" {
		t.Skip("set SSHUSH_TEST_PACKAGED=1 and run inside the sshush package")
	}
	if !packaged() {
		t.Fatal("not running inside a package")
	}
	// Started by a command here, not at sign-in: the main window must open.
	if StartedAtSignIn() {
		t.Error("StartedAtSignIn() is true for a process started from a command")
	}
	before, err := packagedStartupState()
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	t.Logf("state before: %d", before)
	defer func() {
		if err := setPackagedStartup(before.on()); err != nil {
			t.Errorf("restoring the task to on=%v: %v", before.on(), err)
		}
	}()

	if err := setPackagedStartup(false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if startupEnabled() {
		t.Fatal("startupEnabled() is true after disabling the task")
	}
	if err := setPackagedStartup(true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !startupEnabled() {
		t.Fatal("startupEnabled() is false after enabling the task")
	}
}
