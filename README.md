# BoltDM

BoltDM is a download manager written in Go. It also includes a Chrome extension that can find many similar download buttons on one page and send all of their links to BoltDM.

BoltDM is built for direct HTTP and HTTPS file downloads.

![BoltDM](docs/boltdm-ui.png)

## Main features

- Download several files at the same time.
- Split one file into segments.
- Set the number of connections used for each file.
- Pause and resume one download.
- Pause and resume all downloads.
- Retry failed downloads.
- Set a global speed limit.
- Use speed limit presets or enter a custom value.
- Change the default download folder.
- Move an in-progress download to another folder.
- Keep partial data when a download is paused or moved.
- Change segment and connection settings without deleting valid partial data.
- Open a completed file.
- Show a file in Windows Explorer.
- Remove a download from the list.
- Remove a download and delete its file.
- Keep BoltDM running in the Windows system tray.
- Fully close BoltDM from the tray menu.
- Change the font, font size, font weight, font style, spacing, colors, corner size, and custom CSS.

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

- **Segments** control how a file is split for resume data.
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
- global speed limit.

## Speed limit

BoltDM has speed limit presets and a custom input.

You can enter a value in KB/s or MB/s. A value of `0` means no speed limit.

The speed limit is shared by all active downloads and all active connections.

## Download folders

You can change the default download folder at any time.

You can also move an in-progress download to another folder. BoltDM pauses that download, closes its file, moves the partial file and resume data, updates the folder, and resumes the download if it was active before the move.

This also works when the old folder and new folder are on different drives.

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

## Windows install

The Windows release contains:

- `BoltDM.exe`
- `BoltDM.ico`
- `BoltDM-Companion/`
- `Install-BoltDM.cmd`
- `BoltDM.exe.sha256`

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

Calls that change data require BoltDM's local API header. Browser access is limited to Chrome extension origins.

## Tests

The test suite covers:

- segmented downloads;
- connection limits;
- resume after interruption;
- changing segment count without deleting valid partial data;
- servers that do not support Range requests;
- global speed limit;
- saved settings;
- pause all and resume all;
- moving an in-progress download;
- safe file names and safe folder handling.

Run:

```bash
go test ./...
go vet ./...
```

## Current limits

BoltDM currently focuses on direct HTTP and HTTPS file downloads.

The following are not included yet:

- BitTorrent and magnet links;
- HLS and DASH video capture;
- FTP and SFTP;
- proxy profiles;
- scheduled download start times;
- checksum check controls in the interface.

## License

See [LICENSE](LICENSE).
