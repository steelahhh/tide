package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func defaultRoots() []string {
	user := os.Getenv("USER")
	return []string{
		"/Volumes/*",
		"/run/media/" + user + "/*",
		"/media/" + user + "/*",
		"/media/*",
	}
}

func findDevices(patterns []string) ([]string, error) {
	seen := make(map[string]bool)
	var devices []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			if seen[m] {
				continue
			}
			info, err := os.Stat(filepath.Join(m, ".rockbox"))
			if err != nil || !info.IsDir() {
				continue
			}
			seen[m] = true
			devices = append(devices, m)
		}
	}
	sort.Strings(devices)
	return devices, nil
}

func resolveDevice(flagValue string) (string, error) {
	if flagValue != "" {
		info, err := os.Stat(filepath.Join(flagValue, ".rockbox"))
		if err != nil || !info.IsDir() {
			return "", fmt.Errorf("%s has no .rockbox folder; not a Rockbox device", flagValue)
		}
		return flagValue, nil
	}
	devices, err := findDevices(defaultRoots())
	if err != nil {
		return "", err
	}
	switch len(devices) {
	case 0:
		return "", fmt.Errorf("no Rockbox device mounted; use --device")
	case 1:
		return devices[0], nil
	default:
		return "", fmt.Errorf("several Rockbox devices found: %s; use --device", strings.Join(devices, ", "))
	}
}
