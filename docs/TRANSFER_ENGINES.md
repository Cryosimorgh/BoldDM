# BoltDM transfer engines

BoltDM keeps queueing, task state, history, the local API, and the UI in BoltDM. Transfer engines only execute transfers.

## Engine routing

| Source | Default engine | Status |
| --- | --- | --- |
| HTTP / HTTPS file | BoltDM native | Supported |
| FTP | aria2 | Supported when aria2 is available |
| SFTP | aria2 | Supported when the aria2 build includes SFTP |
| Magnet | aria2 | Supported when aria2 is available |
| Remote `.torrent` URL | aria2 | Supported when aria2 is available |
| Metalink `.meta4` / `.metalink` | aria2 | Supported when aria2 is available |
| HLS / DASH / website media | planned media engine | Not implemented yet |
| Cloud / WebDAV remotes | planned remote engine | Not implemented yet |

A direct HTTP/HTTPS request may explicitly select the aria2 engine, but `auto` keeps ordinary HTTP/HTTPS downloads on BoltDM's native downloader so existing segmented resume behavior remains unchanged.

## aria2 discovery

`aria2.mode` accepts:

- `auto` (default): use a configured RPC endpoint if reachable, otherwise look for `aria2c` next to BoltDM and then on `PATH`;
- `external`: require the configured RPC endpoint;
- `off`: disable aria2 integration.

Environment overrides:

```text
BOLTDM_ARIA2_MODE
BOLTDM_ARIA2_EXECUTABLE
BOLTDM_ARIA2_RPC_URL
BOLTDM_ARIA2_RPC_SECRET
```

In managed mode BoltDM starts aria2 on a random loopback-only RPC port with a random RPC secret. aria2 session state is stored under `~/.boltdm/aria2/` and is shut down with BoltDM.

BoltDM does not currently bundle an aria2 binary. Install `aria2c`, put it beside `BoltDM.exe`, put it on `PATH`, or configure an external JSON-RPC instance.

## Task identity and restart behavior

BoltDM's 16-hex-character task ID is reused as the aria2 root GID. This avoids a second persisted ID map. Metadata-following jobs are traversed through aria2's `followedBy` relationship and aggregated back into one BoltDM task.

Old BoltDM state files remain readable. Tasks without engine metadata are classified during load; ordinary HTTP/HTTPS tasks become `native` tasks.

## Current safety choices

- aria2 RPC is loopback-only in managed mode.
- Managed RPC uses a random secret on every launch.
- aria2 integrity checking is enabled.
- Torrent seeding is disabled (`seed-time=0`) until BoltDM has visible seeding controls and policy.
- Removing an aria2 task clears its aria2 result/session state.
- `Delete file & remove` only deletes external-engine paths that resolve under the task's recorded output root.
- Moving an aria2 task is rejected for now rather than moving files behind aria2's back and corrupting resume state.

## Known limitations of this phase

These are intentionally not hidden behind the engine abstraction:

1. BoltDM does not yet bundle a pinned aria2 binary. Packaging it requires explicit GPL-2.0 redistribution notices/source compliance in the release pipeline.
2. aria2 configuration is currently available through config/environment variables, not dedicated settings controls. Changing aria2 mode/path/RPC settings requires restarting BoltDM.
3. Torrent file selection, file priorities, trackers, peer/seeding controls, ratio limits, and seeding history are not exposed yet.
4. Torrent/magnet tasks currently stop at download completion instead of becoming a visible seeding state.
5. Moving active or paused aria2 tasks between folders is not implemented yet.
6. BoltDM's global speed limiter is still native-engine-specific. A cross-engine aggregate limiter has not been implemented yet.
7. Proxy profiles and credentials are not yet modeled centrally, even though aria2 itself supports proxies.
8. aria2 checksum/integrity behavior is enabled, but BoltDM does not yet expose user-entered checksum controls/results in the UI.
9. HLS/DASH/site extraction is not an aria2 responsibility and remains for the media-engine phase.
10. Local `.torrent` file ingestion is not exposed by the current URL-only add API.

## Planned complementary engines

The intended next integrations are kept behind the same BoltDM-owned task model:

- **yt-dlp + FFmpeg** for website media discovery, playlists, HLS/DASH resolution, and mux/post-processing;
- **N_m3u8DL-RE** as an optional specialized HLS/DASH/MSS path where it materially improves stream handling;
- **rclone** for cloud/object-storage/WebDAV remotes without forcing those dependencies on users who only need normal downloads.

BoltDM should keep those tools out-of-process and capability-detected, just like aria2, so they can be updated independently and so their licenses do not get mixed into BoltDM's Go source.