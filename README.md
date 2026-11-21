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

## Setup

- `LISTENBRAINZ_TOKEN` — your ListenBrainz token (or `LISTENBRAINZ_TOKEN_FILE` pointing at a file with it)
- `TIDE_SOURCE` — rsync source, e.g. `homeserver:/srv/music` (or `--source`)
- `--tz` — the zone the iPod clock is set to, e.g. `Europe/Oslo` (defaults to the host zone)
- `--device` — skip auto-detection and name the mount point

Sync deletes files on the iPod that are gone from the library. If a run would
delete more than 50 files or more than 10% of them, it stops; rerun with
`--yes` if that is right.

Needs GNU rsync 3.x (included by the nix build).