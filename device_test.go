package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func stubRoots(t *testing.T, patterns ...string) {
	t.Helper()
	old := defaultRoots
	defaultRoots = func() []string { return patterns }
	t.Cleanup(func() { defaultRoots = old })
}

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
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	stubLookupUUID(t, func(string) (string, error) { return "", errors.New("stubbed") })
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

func TestResolveDeviceRemembers(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	stubLookupUUID(t, func(string) (string, error) { return "ABCD-1234", nil })
	tmp := t.TempDir()
	ipod := filepath.Join(tmp, "ipod")
	if err := os.MkdirAll(filepath.Join(ipod, ".rockbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubRoots(t, tmp+"/*")
	got, err := resolveDevice("")
	if err != nil {
		t.Fatal(err)
	}
	if got != ipod {
		t.Fatalf("resolveDevice = %q, want %q", got, ipod)
	}
	st, ok := loadDeviceState()
	if !ok || st.Path != ipod || st.UUID != "ABCD-1234" {
		t.Fatalf("loadDeviceState = %+v, %v; want path %q, uuid ABCD-1234", st, ok, ipod)
	}
}

func TestResolveDeviceAutoMount(t *testing.T) {
	byUUID, mountPoint := autoMountFixture(t, true)
	if err := os.MkdirAll(filepath.Join(mountPoint, ".rockbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubRoots(t, filepath.Join(t.TempDir(), "*"))
	stubMountDevice(t, func(dev string) (string, error) {
		if want := filepath.Join(byUUID, "TEST-UUID"); dev != want {
			t.Fatalf("mountDevice(%q), want %q", dev, want)
		}
		return mountPoint, nil
	})
	stubLookupUUID(t, func(string) (string, error) { return "TEST-UUID", nil })
	got, err := resolveDevice("")
	if err != nil {
		t.Fatal(err)
	}
	if got != mountPoint {
		t.Fatalf("resolveDevice = %q, want %q", got, mountPoint)
	}
	st, ok := loadDeviceState()
	if !ok || st.Path != mountPoint {
		t.Fatalf("loadDeviceState = %+v, %v; want path %q", st, ok, mountPoint)
	}
}

func TestResolveDeviceAutoMountFails(t *testing.T) {
	autoMountFixture(t, true)
	stubRoots(t, filepath.Join(t.TempDir(), "*"))
	stubMountDevice(t, func(string) (string, error) { return "", errors.New("not authorized") })
	_, err := resolveDevice("")
	if err == nil || err.Error() != "no Rockbox device mounted; use --device" {
		t.Fatalf("resolveDevice error = %v; want the usual no-device error", err)
	}
}

func TestResolveDeviceMountedWins(t *testing.T) {
	autoMountFixture(t, true)
	tmp := t.TempDir()
	ipod := filepath.Join(tmp, "ipod")
	if err := os.MkdirAll(filepath.Join(ipod, ".rockbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubRoots(t, tmp+"/*")
	stubMountDevice(t, func(string) (string, error) {
		t.Fatal("mountDevice called although a device is already mounted")
		return "", nil
	})
	stubLookupUUID(t, func(string) (string, error) { return "", errors.New("stubbed") })
	got, err := resolveDevice("")
	if err != nil {
		t.Fatal(err)
	}
	if got != ipod {
		t.Fatalf("resolveDevice = %q, want %q", got, ipod)
	}
}

func TestResolveDeviceFirstRun(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	stubRoots(t, filepath.Join(t.TempDir(), "*"))
	stubListCandidates(t, func() ([]string, error) { return []string{"/dev/sda2"}, nil })
	mountPoint := t.TempDir()
	if err := os.MkdirAll(filepath.Join(mountPoint, ".rockbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubMountDevice(t, func(dev string) (string, error) {
		if dev != "/dev/sda2" {
			t.Fatalf("mountDevice(%q), want /dev/sda2", dev)
		}
		return mountPoint, nil
	})
	stubLookupUUID(t, func(string) (string, error) { return "4094-F3BD", nil })
	got, err := resolveDevice("")
	if err != nil {
		t.Fatal(err)
	}
	if got != mountPoint {
		t.Fatalf("resolveDevice = %q, want %q", got, mountPoint)
	}
	st, ok := loadDeviceState()
	if !ok || st.Path != mountPoint || st.UUID != "4094-F3BD" {
		t.Fatalf("loadDeviceState = %+v, %v; want path %q, uuid 4094-F3BD", st, ok, mountPoint)
	}
}
