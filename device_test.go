package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFindDevices(t *testing.T) {
	tmp := t.TempDir()
	for _, d := range []string{"ipod/.rockbox", "usb", "ipod2/.rockbox"} {
		if err := os.MkdirAll(filepath.Join(tmp, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got, err := findDevices([]string{tmp + "/*"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(tmp, "ipod"), filepath.Join(tmp, "ipod2")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findDevices = %v, want %v", got, want)
	}
}

func TestFindDevicesNoDuplicates(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "ipod", ".rockbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := findDevices([]string{tmp + "/*", tmp + "/ipod"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(tmp, "ipod")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findDevices = %v, want %v", got, want)
	}
}

func TestResolveDeviceExplicit(t *testing.T) {
	tmp := t.TempDir()
	ipod := filepath.Join(tmp, "ipod")
	if err := os.MkdirAll(filepath.Join(ipod, ".rockbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveDevice(ipod)
	if err != nil {
		t.Fatal(err)
	}
	if got != ipod {
		t.Fatalf("resolveDevice = %q, want %q", got, ipod)
	}
}

func TestResolveDeviceNoRockbox(t *testing.T) {
	tmp := t.TempDir()
	usb := filepath.Join(tmp, "usb")
	if err := os.MkdirAll(usb, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveDevice(usb); err == nil {
		t.Fatal("resolveDevice(usb) succeeded, want error")
	}
}
