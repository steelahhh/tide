# tide

Scrobble a Rockbox iPod to ListenBrainz and sync music to it with rsync.

## Build

```
nix build
```

## Use

Plug in the iPod, then:

```
tide            # scrobble, then sync
tide scrobble   # send the play log to ListenBrainz
tide sync       # mirror the music library onto the iPod
```

Add `--dry-run` to see what would happen without changing anything.

### Editing the play log

```
tide scrobble --edit   # review the log in a TUI before sending
```

The editor lists every play: listened tracks start included, skipped ones
start excluded. Space toggles an entry, enter saves and sends, q cancels
without changing anything. Saving rewrites the log to exactly the included
entries, so excluded plays (including untouched skips) are deleted from it.
With `--dry-run` nothing is written; the preview shows the post-edit listens.

`m` cycles the display between raw fields, prettified untagged filenames, and
a custom format template (`t` to edit it; placeholders like `{artist}`,
`{title}`, `{time}`). Display modes never change what is written.

### Device memory and auto-mount (Linux)

After a successful run tide remembers the iPod (filesystem UUID and mount
point) in `$XDG_STATE_HOME/tide/device.json`. If a later run finds no Rockbox
device mounted but the remembered one is plugged in, tide mounts it via
udisks2 (`udisksctl`, no root needed) and continues. A device you mounted
yourself always wins. If the remembered device is absent or cannot be
mounted, tide prints a note and the usual no-device error.

On the very first run, before anything is remembered: if exactly one
unmounted USB partition is present, tide mounts it, keeps it if it carries
`.rockbox`, and unmounts it again otherwise.

## Setup

- `LISTENBRAINZ_TOKEN` — your ListenBrainz token (or `LISTENBRAINZ_TOKEN_FILE` pointing at a file with it)
- `TIDE_SOURCE` — rsync source, e.g. `homeserver:/srv/music` (or `--source`)
- `--tz` — the zone the iPod clock is set to, e.g. `Europe/Oslo` (defaults to the host zone)
- `--device` — skip auto-detection and name the mount point

Sync deletes files on the iPod that are gone from the library. If a run would
delete more than 50 files or more than 10% of them, it stops; rerun with
`--yes` if that is right.

Needs GNU rsync 3.x (included by the nix build).