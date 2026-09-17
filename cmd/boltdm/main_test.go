package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveStateDirForLinuxUsesXDG(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(t.TempDir(), "state")
	t.Setenv("XDG_STATE_HOME", xdg)
	got := resolveStateDirForOS(home, "linux")
	want := filepath.Join(xdg, "boltdm")
	if got != want {
		t.Fatalf("state dir = %q, want %q", got, want)
	}
}

func TestResolveStateDirForLinuxPreservesLegacyInstall(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".boltdm")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	if got := resolveStateDirForOS(home, "linux"); got != legacy {
		t.Fatalf("state dir = %q, want legacy %q", got, legacy)
	}
}

func TestResolveStateDirForLinuxHasXDGCompatibleFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", "")
	want := filepath.Join(home, ".local", "state", "boltdm")
	if got := resolveStateDirForOS(home, "linux"); got != want {
		t.Fatalf("state dir = %q, want %q", got, want)
	}
}

func TestResolveStateDirForWindowsKeepsExistingLocation(t *testing.T) {
	home := t.TempDir()
	want := filepath.Join(home, ".boltdm")
	if got := resolveStateDirForOS(home, "windows"); got != want {
		t.Fatalf("state dir = %q, want %q", got, want)
	}
}

func TestShouldOpenDashboardWithoutNativeTray(t *testing.T) {
	if !shouldOpenDashboard("linux", "tray") {
		t.Fatal("Linux tray preference must not hide the dashboard")
	}
	if shouldOpenDashboard("windows", "tray") {
		t.Fatal("Windows tray launch should remain hidden")
	}
	if !shouldOpenDashboard("windows", "dashboard") {
		t.Fatal("Windows dashboard launch should open the browser")
	}
}
