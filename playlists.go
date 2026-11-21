package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func sourcePrefix(src string) string {
	path := src
	if i := strings.Index(src, ":"); i >= 0 {
		path = src[i+1:]
	}
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	return path
}

func rewritePlaylist(content, srcPrefix, dstPrefix string) string {
	var b strings.Builder
	for len(content) > 0 {
		i := strings.IndexByte(content, '\n')
		var line string
		if i < 0 {
			line, content = content, ""
		} else {
			line, content = content[:i+1], content[i+1:]
		}
		if strings.HasPrefix(line, srcPrefix) {
			line = dstPrefix + line[len(srcPrefix):]
		}
		b.WriteString(line)
	}
	return b.String()
}

func syncPlaylists(device, src string, dryRun bool) error {
	tmp, err := os.MkdirTemp("", "tide-playlists-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	srcPlaylists := strings.TrimSuffix(src, "/") + "/playlists/"
	out, err := exec.Command("rsync", "-rt", "--include=*.m3u8", "--exclude=*", srcPlaylists, tmp+"/").CombinedOutput()
	if err != nil {
		fmt.Printf("playlists: none copied (%v: %s)\n", err, string(bytes.TrimSpace(out)))
		return nil
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return err
	}
	type playlist struct{ name, content string }
	var playlists []playlist
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".m3u8") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(tmp, e.Name()))
		if err != nil {
			return err
		}
		playlists = append(playlists, playlist{
			name:    e.Name(),
			content: rewritePlaylist(string(b), sourcePrefix(src), "/Music/"),
		})
	}
	if dryRun {
		for _, p := range playlists {
			fmt.Println("playlists:", p.name)
		}
		return nil
	}
	target := filepath.Join(device, "Playlists", "Navidrome")
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	for _, p := range playlists {
		if err := os.WriteFile(filepath.Join(target, p.name), []byte(p.content), 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("playlists: %d written to %s\n", len(playlists), target)
	return nil
}
