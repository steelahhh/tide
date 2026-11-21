package main

import "testing"

func TestSourcePrefix(t *testing.T) {
	cases := []struct{ src, want string }{
		{"homeserver:/srv/music", "/srv/music/"},
		{"/srv/music/", "/srv/music/"},
	}
	for _, c := range cases {
		if got := sourcePrefix(c.src); got != c.want {
			t.Errorf("sourcePrefix(%q) = %q, want %q", c.src, got, c.want)
		}
	}
}

func TestRewritePlaylist(t *testing.T) {
	content := "#EXTM3U\n#EXTINF:245,Test Artist - First Song\n/srv/music/artists/Test Artist/Test Album/01 First Song.flac\n"
	want := "#EXTM3U\n#EXTINF:245,Test Artist - First Song\n/Music/artists/Test Artist/Test Album/01 First Song.flac\n"
	if got := rewritePlaylist(content, "/srv/music/", "/Music/"); got != want {
		t.Fatalf("rewritePlaylist = %q, want %q", got, want)
	}
}

func TestRewritePlaylistKeepsLineEndings(t *testing.T) {
	content := "#EXTM3U\r\n/srv/music/a.flac\r\n/srv/music/b.flac"
	want := "#EXTM3U\r\n/Music/a.flac\r\n/Music/b.flac"
	if got := rewritePlaylist(content, "/srv/music/", "/Music/"); got != want {
		t.Fatalf("rewritePlaylist = %q, want %q", got, want)
	}
}

func TestRewritePlaylistUnmatchedLines(t *testing.T) {
	content := "#EXTM3U\n/elsewhere/a.flac\n/srv/musician/b.flac\n"
	if got := rewritePlaylist(content, "/srv/music/", "/Music/"); got != content {
		t.Fatalf("rewritePlaylist changed unmatched lines: %q", got)
	}
}
