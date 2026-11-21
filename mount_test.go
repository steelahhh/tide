package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func stubLookupUUID(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	old := lookupUUID
	lookupUUID = fn
	t.Cleanup(func() { lookupUUID = old })
}

func stubMountDevice(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	old := mountDevice
	mountDevice = fn
	t.Cleanup(func() { mountDevice = old })
}

func stubDiskByUUID(t *testing.T, dir string) {
	t.Helper()
	old := diskByUUID
	diskByUUID = dir
	t.Cleanup(func() { diskByUUID = old })
}

func stubListCandidates(t *testing.T, fn func() ([]string, error)) {
	t.Helper()
	old := listCandidates
	listCandidates = fn
	t.Cleanup(func() { listCandidates = old })
}

func stubUnmountDevice(t *testing.T, fn func(string) error) {
	t.Helper()
	old := unmountDevice
	unmountDevice = fn
	t.Cleanup(func() { unmountDevice = old })
}

func TestDeviceStateRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	want := deviceState{Path: "/run/media/sasha/IPOD", UUID: "F704-B0EE"}
	if err := saveDeviceState(want); err != nil {
		t.Fatal(err)
	}
	got, ok := loadDeviceState()
	if !ok || got != want {
		t.Fatalf("loadDeviceState = %+v, %v; want %+v, true", got, ok, want)
	}
}

func TestLoadDeviceStateBad(t *testing.T) {
	for _, tc := range []struct{ name, content string }{
		{"missing", ""},
		{"garbage", "not json"},
		{"no uuid", `{"path":"/run/media/sasha/IPOD"}`},
		{"empty uuid", `{"path":"/x","uuid":""}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("XDG_STATE_HOME", dir)
			if tc.content != "" {
				if err := os.MkdirAll(filepath.Join(dir, "tide"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(deviceStatePath(), []byte(tc.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if _, ok := loadDeviceState(); ok {
				t.Fatal("loadDeviceState succeeded, want failure")
			}
		})
	}
}

func TestParseMountedPath(t *testing.T) {
	for _, tc := range []struct {
		out  string
		want string
		ok   bool
	}{
		{"Mounted /dev/sda1 at /run/media/sasha/IPOD.\n", "/run/media/sasha/IPOD", true},
		{"Mounted /dev/sda1 at /run/media/sasha/IPOD.", "/run/media/sasha/IPOD", true},
		{"Error mounting /dev/sda1: GDBus.Error:org.freedesktop.UDisks2.Error.AlreadyMounted: Device /dev/sda1 is already mounted at `/mnt/ipod'.\n", "/mnt/ipod", true},
		{"Error mounting /dev/nvme0n1p2: GDBus.Error:org.freedesktop.UDisks2.Error.AlreadyMounted: Device /dev/nvme0n1p2 is already mounted at `/', `/nix/store'.\n", "/", true},
		{"Mounted /dev/sda1 at /media/My iPod.\n", "/media/My iPod", true},
		{"garbage", "", false},
		{"nothing at relative/path", "", false},
	} {
		got, ok := parseMountedPath(tc.out)
		if got != tc.want || ok != tc.ok {
			t.Errorf("parseMountedPath(%q) = %q, %v; want %q, %v", tc.out, got, ok, tc.want, tc.ok)
		}
	}
}

func TestRememberDevice(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	stubLookupUUID(t, func(string) (string, error) { return "ABCD-1234", nil })
	rememberDevice("/run/media/sasha/IPOD")
	got, ok := loadDeviceState()
	want := deviceState{Path: "/run/media/sasha/IPOD", UUID: "ABCD-1234"}
	if !ok || got != want {
		t.Fatalf("loadDeviceState = %+v, %v; want %+v, true", got, ok, want)
	}
}

func TestRememberDeviceNoUUID(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	stubLookupUUID(t, func(string) (string, error) { return "", errors.New("no findmnt") })
	rememberDevice("/run/media/sasha/IPOD")
	if _, ok := loadDeviceState(); ok {
		t.Fatal("state saved despite UUID lookup failure")
	}
}

// autoMountFixture saves a remembered device and optionally makes its block
// device present; the returned mountPoint is a fresh dir without .rockbox.
func autoMountFixture(t *testing.T, pluggedIn bool) (byUUID, mountPoint string) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	byUUID = t.TempDir()
	stubDiskByUUID(t, byUUID)
	if err := saveDeviceState(deviceState{Path: "/old", UUID: "TEST-UUID"}); err != nil {
		t.Fatal(err)
	}
	if pluggedIn {
		if err := os.WriteFile(filepath.Join(byUUID, "TEST-UUID"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return byUUID, t.TempDir()
}

func TestAutoMountNoState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	stubDiskByUUID(t, t.TempDir())
	stubListCandidates(t, func() ([]string, error) { return nil, nil })
	stubMountDevice(t, func(string) (string, error) {
		t.Fatal("mountDevice called without remembered state or candidates")
		return "", nil
	})
	got, err := autoMount()
	if got != "" || err != nil {
		t.Fatalf("autoMount = %q, %v; want empty, nil", got, err)
	}
}

func TestAutoMountNotPluggedIn(t *testing.T) {
	autoMountFixture(t, false)
	stubListCandidates(t, func() ([]string, error) {
		t.Fatal("discovery ran although a device is remembered")
		return nil, nil
	})
	stubMountDevice(t, func(string) (string, error) {
		t.Fatal("mountDevice called for an absent device")
		return "", nil
	})
	got, err := autoMount()
	if got != "" || err != nil {
		t.Fatalf("autoMount = %q, %v; want empty, nil", got, err)
	}
}

func TestAutoMountSuccess(t *testing.T) {
	byUUID, mountPoint := autoMountFixture(t, true)
	if err := os.MkdirAll(filepath.Join(mountPoint, ".rockbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubMountDevice(t, func(dev string) (string, error) {
		if want := filepath.Join(byUUID, "TEST-UUID"); dev != want {
			t.Fatalf("mountDevice(%q), want %q", dev, want)
		}
		return mountPoint, nil
	})
	got, err := autoMount()
	if got != mountPoint || err != nil {
		t.Fatalf("autoMount = %q, %v; want %q, nil", got, err, mountPoint)
	}
}

func TestAutoMountError(t *testing.T) {
	autoMountFixture(t, true)
	stubMountDevice(t, func(string) (string, error) { return "", errors.New("not authorized") })
	got, err := autoMount()
	if got != "" || err == nil {
		t.Fatalf("autoMount = %q, %v; want empty, error", got, err)
	}
}

func TestAutoMountNoRockbox(t *testing.T) {
	_, mountPoint := autoMountFixture(t, true)
	stubMountDevice(t, func(string) (string, error) { return mountPoint, nil })
	got, err := autoMount()
	if got != "" || err == nil {
		t.Fatalf("autoMount = %q, %v; want empty, error", got, err)
	}
}

func TestParseCandidates(t *testing.T) {
	out := []byte(`{"blockdevices": [
		{"name":"sda","tran":"usb","fstype":null,"mountpoint":null,"children":[
			{"name":"sda1","tran":null,"fstype":null,"mountpoint":null},
			{"name":"sda2","tran":null,"fstype":"vfat","mountpoint":null}]},
		{"name":"sdb","tran":"usb","fstype":"vfat","mountpoint":"/run/media/sasha/STICK"},
		{"name":"sdc","tran":"usb","fstype":null,"mountpoint":null,"children":[
			{"name":"sdc1","tran":null,"fstype":"ext4","mountpoint":null}]},
		{"name":"nvme0n1","tran":"nvme","fstype":null,"mountpoint":null,"children":[
			{"name":"nvme0n1p1","tran":"nvme","fstype":"vfat","mountpoint":"/boot"}]}
	]}`)
	want := []string{"/dev/sda2", "/dev/sdc1"}
	got, err := parseCandidates(out)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("parseCandidates = %v, %v; want %v, nil", got, err, want)
	}
	if got, err := parseCandidates([]byte(`{"blockdevices":[]}`)); err != nil || got != nil {
		t.Fatalf("parseCandidates(empty) = %v, %v; want nil, nil", got, err)
	}
	if _, err := parseCandidates([]byte("garbage")); err == nil {
		t.Fatal("parseCandidates(garbage) succeeded, want error")
	}
}

func TestDiscoverDevice(t *testing.T) {
	for _, tc := range []struct {
		name      string
		cands     []string
		listErr   error
		mountErr  error
		rockbox   bool
		wantPath  bool
		wantErr   bool
		unmounted bool
	}{
		{name: "no candidates"},
		{name: "list error", listErr: errors.New("no lsblk")},
		{name: "ambiguous", cands: []string{"/dev/sda2", "/dev/sdc"}},
		{name: "found", cands: []string{"/dev/sda2"}, rockbox: true, wantPath: true},
		{name: "mount error", cands: []string{"/dev/sda2"}, mountErr: errors.New("dirty fs"), wantErr: true},
		{name: "not rockbox", cands: []string{"/dev/sda2"}, unmounted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubListCandidates(t, func() ([]string, error) { return tc.cands, tc.listErr })
			mountPoint := t.TempDir()
			if tc.rockbox {
				if err := os.MkdirAll(filepath.Join(mountPoint, ".rockbox"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			stubMountDevice(t, func(dev string) (string, error) {
				if dev != "/dev/sda2" {
					t.Fatalf("mountDevice(%q), want /dev/sda2", dev)
				}
				if tc.mountErr != nil {
					return "", tc.mountErr
				}
				return mountPoint, nil
			})
			var unmounted string
			stubUnmountDevice(t, func(dev string) error {
				unmounted = dev
				return nil
			})
			got, err := discoverDevice()
			if tc.wantPath && (got != mountPoint || err != nil) {
				t.Fatalf("discoverDevice = %q, %v; want %q, nil", got, err, mountPoint)
			}
			if !tc.wantPath && got != "" {
				t.Fatalf("discoverDevice = %q, want empty", got)
			}
			if (err != nil) != tc.wantErr {
				t.Fatalf("discoverDevice error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.unmounted && unmounted != "/dev/sda2" {
				t.Fatalf("unmountDevice called with %q, want /dev/sda2", unmounted)
			}
			if !tc.unmounted && unmounted != "" {
				t.Fatalf("unmountDevice called with %q, want no call", unmounted)
			}
		})
	}
}

func TestAutoMountFirstRun(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	stubListCandidates(t, func() ([]string, error) { return []string{"/dev/sda2"}, nil })
	mountPoint := t.TempDir()
	if err := os.MkdirAll(filepath.Join(mountPoint, ".rockbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubMountDevice(t, func(string) (string, error) { return mountPoint, nil })
	got, err := autoMount()
	if got != mountPoint || err != nil {
		t.Fatalf("autoMount = %q, %v; want %q, nil", got, err, mountPoint)
	}
}
