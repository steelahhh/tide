package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type deviceState struct {
	Path string `json:"path"`
	UUID string `json:"uuid"`
}

func deviceStatePath() string {
	return filepath.Join(stateDir(), "device.json")
}

func loadDeviceState() (deviceState, bool) {
	b, err := os.ReadFile(deviceStatePath())
	if err != nil {
		return deviceState{}, false
	}
	var st deviceState
	if err := json.Unmarshal(b, &st); err != nil || st.UUID == "" {
		return deviceState{}, false
	}
	return st, true
}

func saveDeviceState(st deviceState) error {
	dir := stateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".device-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, deviceStatePath())
}

var diskByUUID = "/dev/disk/by-uuid"

var lookupUUID = func(path string) (string, error) {
	out, err := exec.Command("findmnt", "--target", path, "-no", "UUID").Output()
	if err != nil {
		return "", fmt.Errorf("findmnt: %w", err)
	}
	uuid := strings.TrimSpace(string(out))
	if uuid == "" {
		return "", fmt.Errorf("no filesystem UUID for %s", path)
	}
	return uuid, nil
}

var mountDevice = func(dev string) (string, error) {
	out, err := exec.Command("udisksctl", "mount", "-b", dev, "--no-user-interaction").CombinedOutput()
	if path, ok := parseMountedPath(string(out)); ok {
		return path, nil
	}
	if err != nil {
		return "", fmt.Errorf("udisksctl mount: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return "", fmt.Errorf("udisksctl mount: unexpected output: %s", strings.TrimSpace(string(out)))
}

var unmountDevice = func(dev string) error {
	out, err := exec.Command("udisksctl", "unmount", "-b", dev, "--no-user-interaction").CombinedOutput()
	if err != nil {
		return fmt.Errorf("udisksctl unmount: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

type lsblkDevice struct {
	Name       string        `json:"name"`
	Tran       string        `json:"tran"`
	FSType     string        `json:"fstype"`
	Mountpoint string        `json:"mountpoint"`
	Children   []lsblkDevice `json:"children"`
}

var listCandidates = func() ([]string, error) {
	out, err := exec.Command("lsblk", "-Jno", "NAME,TRAN,FSTYPE,MOUNTPOINT").Output()
	if err != nil {
		return nil, err
	}
	return parseCandidates(out)
}

// parseCandidates picks unmounted USB partitions with a filesystem from the
// lsblk JSON tree. tran is only set on the parent disk, so it is inherited
// down the tree; a USB disk with a filesystem of its own also counts.
func parseCandidates(out []byte) ([]string, error) {
	var tree struct {
		Blockdevices []lsblkDevice `json:"blockdevices"`
	}
	if err := json.Unmarshal(out, &tree); err != nil {
		return nil, err
	}
	var devs []string
	var walk func(nodes []lsblkDevice, usb bool)
	walk = func(nodes []lsblkDevice, usb bool) {
		for _, n := range nodes {
			isUSB := usb || n.Tran == "usb"
			if isUSB && n.FSType != "" && n.Mountpoint == "" {
				devs = append(devs, "/dev/"+n.Name)
			}
			walk(n.Children, isUSB)
		}
	}
	walk(tree.Blockdevices, false)
	return devs, nil
}

// parseMountedPath reads the mount point from udisksctl output, both the
// success line and the AlreadyMounted error (device mounted outside the
// auto-detection roots).
func parseMountedPath(out string) (string, bool) {
	i := strings.Index(out, " at ")
	if i < 0 {
		return "", false
	}
	path := out[i+len(" at "):]
	if j := strings.IndexAny(path, "\r\n"); j >= 0 {
		path = path[:j]
	}
	path = strings.TrimSpace(path)
	if strings.HasPrefix(path, "`") {
		// AlreadyMounted quotes each mount point: at `/path', `/other'.
		path = path[1:]
		if j := strings.Index(path, "'"); j >= 0 {
			path = path[:j]
		}
	} else {
		path = strings.TrimSuffix(path, ".")
	}
	if !strings.HasPrefix(path, "/") {
		return "", false
	}
	return path, true
}

func rememberDevice(path string) {
	if runtime.GOOS != "linux" {
		return
	}
	uuid, err := lookupUUID(path)
	if err != nil {
		return
	}
	_ = saveDeviceState(deviceState{Path: path, UUID: uuid})
}

// autoMount mounts the remembered device if it is plugged in but not
// mounted, or discovers a device on the first run. Returns ("", nil) when
// there is nothing to try.
func autoMount() (string, error) {
	if runtime.GOOS != "linux" {
		return "", nil
	}
	st, ok := loadDeviceState()
	if !ok {
		return discoverDevice()
	}
	dev := filepath.Join(diskByUUID, st.UUID)
	if _, err := os.Stat(dev); err != nil {
		return "", nil
	}
	return mountAndVerify(dev)
}

func mountAndVerify(dev string) (string, error) {
	path, err := mountDevice(dev)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(filepath.Join(path, ".rockbox")); err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s mounted at %s has no .rockbox folder", dev, path)
	}
	return path, nil
}

// discoverDevice handles the first run, before anything is remembered: with
// exactly one unmounted removable USB partition, mount it and keep it only if
// it carries .rockbox. Ambiguous cases are left alone.
func discoverDevice() (string, error) {
	cands, err := listCandidates()
	if err != nil || len(cands) != 1 {
		return "", nil
	}
	dev := cands[0]
	path, err := mountDevice(dev)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(filepath.Join(path, ".rockbox")); err != nil || !info.IsDir() {
		_ = unmountDevice(dev)
		return "", nil
	}
	return path, nil
}
