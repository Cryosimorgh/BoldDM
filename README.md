# BoltDM

BoltDM is a download manager written in Go. It includes a native HTTP/HTTPS engine, optional aria2 integration for additional protocols, and a Chrome extension that can find many similar download buttons on one page and send all of their links to BoltDM.

Ordinary HTTP/HTTPS files continue to use BoltDM's native segmented downloader. When `aria2c` is available, BoltDM can also handle FTP, SFTP, magnet links, remote torrent files, and Metalink sources.

![BoltDM](docs/boltdm-ui.png)

## Main features

- Download several files at the same time.
- Split one HTTP/HTTPS file into segments.
- Set the number of connections used for each file.
- Pause and resume one download.
- Pause and resume all downloads.
- Retry failed downloads.
- Set a global speed limit for the native engine.
- Use speed limit presets or enter a custom value.
- Change the default download folder.
- Move an in-progress native-engine download to another folder.
- Keep partial native-engine data when a download is paused or moved.
- Change segment and connection settings without deleting valid partial data.
- Use aria2 for FTP, SFTP, magnets, remote `.torrent` files, and Metalink.
- Restore managed aria2 session state across BoltDM restarts.
- Show transfer engine/source information in the dashboard.
- Open a completed file or multi-file output directory.
- Show a file in Windows Explorer.
- Remove a download from the list.
- Remove a download and delete its files safely under the recorded output root.
- Keep BoltDM running in the Windows system tray.
- Fully close BoltDM from the tray menu.
- Change the font, font size, font weight, font style, spacing, colors, corner size, and custom CSS.

## Transfer engines

BoltDM owns the queue, task state, history, local API, and UI. Transfer engines only execute transfers.

Default routing:

| Source | Engine |
| --- | --- |
| HTTP / HTTPS file | BoltDM native |
| FTP | aria2 |
| SFTP | aria2 |
| Magnet | aria2 |
| Remote `.torrent` URL | aria2 |
| `.meta4` / `.metalink` | aria2 |

aria2 is optional. In the default `auto` mode BoltDM looks for `aria2c` next to the BoltDM executable and then on `PATH`. An external aria2 JSON-RPC service can also be configured. BoltDM does not bundle an aria2 binary yet.

Detailed architecture, configuration, safety behavior, licensing notes, and current limitations are documented in [docs/TRANSFER_ENGINES.md](docs/TRANSFER_ENGINES.md).

## Chrome extension

The Chrome extension is made for pages that contain many similar download buttons.

1. Start BoltDM.
2. Open the page with the download buttons.
3. Open the BoltDM Chrome extension.
4. Click **Select download button**.
5. Click one download button on the page.
6. The extension finds similar buttons and collects their links.
7. Review the links.
8. Send them to BoltDM.

The extension sends the links directly to BoltDM on your computer. It does not use Chrome's normal download list and it does not use Free Download Manager.

## Download settings

BoltDM keeps **segments** and **connections** separate.

- **Segments** control how a direct file is split for resume data.
- **Connections** control how many parts of that file are downloaded at the same time.

Example:

```text
Segments: 32
Connections: 6
```

This keeps 32 resume segments but uses at most 6 active connections for that file.

You can also set:

- number of files downloaded at the same time;
- number of files downloaded from the same host at the same time;
- minimum segment size;
- retry count;
- default segment count;
- default connection count;
- native-engine global speed limit.

## Speed limit

BoltDM has speed limit presets and a custom input.

You can enter a value in KB/s or MB/s. A value of `0` means no speed limit.

The current global limiter is shared by all active **native-engine** downloads and connections. Cross-engine aggregate limiting with aria2 is not implemented yet.

## Download folders

You can change the default download folder at any time.

For native-engine downloads, you can also move an in-progress download to another folder. BoltDM pauses that download, closes its file, moves the partial file and resume data, updates the folder, and resumes the download if it was active before the move.

This also works when the old folder and new folder are on different drives.

Moving aria2-managed tasks is intentionally blocked for now rather than moving files behind aria2's resume/session state.

## Windows system tray

BoltDM stays in the Windows system tray while downloads are running.

Tray menu:

- **Open BoltDM**
- **Pause all**
- **Resume all**
- **Exit BoltDM**

Closing the browser page does not close BoltDM. Use **Exit BoltDM** when you want to fully close it.

## Appearance settings

You can change:

- font family;
- font size;
- font weight;
- normal or italic font style;
- compact, normal, or spacious spacing;
- accent color;
- background color;
- panel color;
- text color;
- panel corner size;
- reduced motion;
- custom CSS.

Custom CSS is loaded after the built-in style. It can be used to change parts of the interface that do not have their own setting yet.

## aria2 configuration

The default mode is `auto`.

Environment overrides:

```text
BOLTDM_ARIA2_MODE=auto|external|off
BOLTDM_ARIA2_EXECUTABLE=path/to/aria2c
BOLTDM_ARIA2_RPC_URL=http://127.0.0.1:6800
BOLTDM_ARIA2_RPC_SECRET=secret
```

In managed mode BoltDM starts aria2 on a random loopback-only port with a random RPC secret and stores session state under:

```text
%USERPROFILE%\.boltdm\aria2
```

## Windows install

The Windows release currently contains:

- `BoltDM.exe`
- `BoltDM.ico`
- `BoltDM-Companion/`
- `Install-BoltDM.cmd`
- `BoltDM.exe.sha256`

`aria2c.exe` is not currently redistributed with BoltDM. Put it next to `BoltDM.exe`, add it to `PATH`, or configure an external aria2 service if you want the aria2-backed protocols.

### Portable use

Keep `BoltDM.exe` and `BoltDM.ico` in the same folder and run `BoltDM.exe`.

### Local install

Run `Install-BoltDM.cmd`.

It installs BoltDM to:

```text
%LOCALAPPDATA%\Programs\BoltDM
```

BoltDM settings and download data are stored in:

```text
%USERPROFILE%\.boltdm
```

The default download folder on Windows is:

```text
%USERPROFILE%\Downloads\BoltDM
```

## Windows warning

The current Windows build is not signed with a trusted code-signing certificate.

Because of that, Windows SmartScreen can show **Unknown publisher** or **Windows protected your PC**.

The release includes a SHA-256 file so you can check the downloaded executable. The source is also included in this repository.

A trusted Windows code-signing certificate is required to show a trusted publisher name. The project includes `sign-release.ps1` for that release step.

## Build from source

Requires Go 1.23 or newer.

Windows PowerShell:

```powershell
.\build.ps1
```

Linux or macOS development build:

```bash
./build.sh
```

## Local API

BoltDM listens only on:

```text
127.0.0.1:17654
```

Main routes:

```text
GET  /api/v1/health
GET  /api/v1/engines
POST /api/v1/downloads
GET  /api/v1/tasks
POST /api/v1/tasks/{id}/{pause|resume|retry|cancel|remove|open|reveal|move|tune}
POST /api/v1/bulk/{pause|resume|retry-failed|restart-active|cancel|clear-completed}
GET  /api/v1/settings
POST /api/v1/settings
POST /api/v1/settings/pick-directory
POST /api/v1/system/open-downloads
POST /api/v1/system/shutdown
```

Calls that change data require BoltDM's local API header. Browser access is limited to Chrome/Firefox extension origins.

`POST /api/v1/downloads` accepts an optional `engine` value of `auto`, `native`, or `aria2`. `auto` is the default.

## Tests

The test suite covers:

- segmented downloads;
- connection limits;
- resume after interruption;
- changing segment count without deleting valid partial data;
- servers that do not support Range requests;
- native global speed limit;
- saved settings;
- pause all and resume all;
- moving an in-progress native download;
- safe file names and safe folder handling;
- transfer-source classification and engine routing;
- aria2 JSON-RPC request shape and authentication token handling.

Run:

```bash
go test ./...
go vet ./...
```

CI runs both commands for `main`, feature branches, and pull requests.

## Current limits

The aria2 phase closes the original FTP/SFTP/BitTorrent/magnet/Metalink protocol gap, but a few full download-manager features remain:

- HLS/DASH and website-media extraction;
- torrent file selection/priorities, tracker controls, and visible seeding policy;
- proxy profiles shared across engines;
- scheduled download start times;
- checksum controls/results in the interface;
- browser-wide automatic download interception;
- cross-engine aggregate speed limiting;
- moving aria2-managed transfers between folders;
- cloud/WebDAV/object-storage remotes.

See [docs/TRANSFER_ENGINES.md](docs/TRANSFER_ENGINES.md) for the planned complementary-engine path.

## License

BoltDM source is MIT-licensed. aria2 is an external optional dependency with its own license; BoltDM does not currently redistribute aria2 binaries. See [LICENSE](LICENSE) and [docs/TRANSFER_ENGINES.md](docs/TRANSFER_ENGINES.md).
