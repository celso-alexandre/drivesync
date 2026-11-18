# Developing drivesync

This page is for people who build, audit or package drivesync. Users only need the [README](../README.md).

## Architecture

- `drivesync` (Go, standard library only): the sync engine. It is the only part that talks to Google or touches synced files, and it runs sandboxed.
- `drivesync-gui` (Rust + Qt 6 / QML, in `gui/`): the tray app and wizard. It starts and supervises the engine and talks to it only through `status.json` and the settings file.

### Security

drivesync holds a token that can read and write your Drive, so it's designed to need as little trust as possible.

**No third-party code in the sync engine.** It uses only the Go standard library. There is no `go.sum`, and a test fails the build if a dependency is ever added. The Linux development container runs with no network access. CI and release builds do use the network, for example to download Go and the Flatpak runtime.

**Your keys, your keychain.** You sign in with your own Google Cloud OAuth client, so no third party ever holds a key to your Drive. The sign-in is kept in the system's own secret store: KWallet or GNOME Keyring on Linux (through Secret Service), the Keychain on macOS, and Credential Manager on Windows. It never touches a plain file.

**Sandboxed.** The part of drivesync that talks to Drive and touches your files runs locked down by the operating system:

| | Linux | macOS | Windows |
| --- | --- | --- | --- |
| Mechanism | [Landlock](https://docs.kernel.org/userspace-api/landlock.html) + seccomp, in-process | `sandbox-exec` profile on a worker process | AppContainer worker in a job object |
| Files | Only the sync folder, its state and its config | Only the sync folder, its state and its config | Only the sync folder, its state and its config |
| Network | Outgoing TCP to ports 443 and 53 only | Outgoing TCP to ports 443 and 53 only | Outgoing internet only (any port) |
| Starting other programs | Blocked | Blocked | Blocked |
| Other processes | Can't debug or signal them | Expected from the profile, not yet tested | Expected from AppContainer, not yet tested |
| Kernel attack surface | Blocks namespaces, BPF, io_uring, keyrings and module loading | Default macOS sandbox rules | Default AppContainer rules |

The restrictions can't be lifted from the inside. If the sandbox can't be set up, the sync **refuses to start**. On macOS and Windows the worker also checks itself before syncing anything: if it can still read your home folder or start a program, it stops. `--unsafe-no-sandbox` turns all of this off and prints a warning every time.

How much of this is verified: on Linux, automated tests try each escape listed above and expect it to fail. On macOS and Windows, CI runs a smaller self-test (`drivesync selftest-sandbox`) on one OS version each (macOS 15, Windows Server 2025). It checks file access, starting programs and network access, and nothing more.

On macOS and Windows, a small launcher reads your sign-in from the system keychain and hands it to the sandboxed worker. `login`, `logout`, `select` and `status` run in that launcher without the sandbox. They are short, local or single-request commands, and on Linux they are sandboxed too.

On Windows, drivesync grants its AppContainer access to the sync, state and config folders by adding an entry to each folder's permissions.

**What the sandbox can't do.** It is a safety net for bugs and for hostile data coming from Drive (for example malicious file names). It is **not** protection against a malicious build of drivesync itself: code that is already malicious runs before the sandbox starts, and on macOS and Windows it also controls the unsandboxed launcher. Only install builds you trust, and check the signatures.

It also limits *ports*, not *hosts*. Code running inside the sandbox could still send your Drive data to some other HTTPS server. To pin drivesync to Google's servers only, add an application firewall such as [OpenSnitch](https://github.com/evilsocket/opensnitch) (Linux), Little Snitch or LuLu (macOS), or Windows Firewall rules. Allow `www.googleapis.com` and `oauth2.googleapis.com` on port 443.

### Command-line install

### Linux: Flatpak (recommended, updates automatically)

```sh
flatpak remote-add --user --if-not-exists drivesync https://celso-alexandre.github.io/drivesync/drivesync.flatpakrepo
flatpak install --user drivesync io.github.celso_alexandre.drivesync
alias drivesync='flatpak run io.github.celso_alexandre.drivesync'
```

The Flatpak can only see `~/GoogleDrive`. If you change the sync folder, grant access with `flatpak override --user --filesystem=/new/path io.github.celso_alexandre.drivesync`.

### Linux, macOS, Windows: release archives

Download the archive for your system from [Releases](https://github.com/celso-alexandre/drivesync/releases). Check it against `SHA256SUMS`, whose signature can be verified with the key in [`flatpak/drivesync.asc`](flatpak/drivesync.asc). Then put `drivesync` (or `drivesync.exe`) somewhere on your `PATH`.

macOS marks downloaded files as quarantined. If you downloaded it with a browser, run `xattr -d com.apple.quarantine drivesync` once.

### From source

```sh
git clone https://github.com/celso-alexandre/drivesync
cd drivesync
scripts/dev.sh build        # builds ./drivesync in a container; DRIVESYNC_RUN=direct builds with your local Go
```

### Requirements

- **Linux:** x86-64, kernel 6.7 or newer with Landlock enabled (most current distributions enable it; drivesync tells you if yours doesn't), `secret-tool` (package `libsecret` or `libsecret-tools`, already inside the Flatpak) and KWallet or GNOME Keyring. Tested on Fedora and in CI on Ubuntu 24.04.
- **macOS:** tested only on macOS 15 (Apple silicon) in CI. Intel builds are produced but untested.
- **Windows:** x86-64. Tested only on Windows Server 2025 in CI.

### Command-line use

```sh
drivesync login              # paste your client ID and secret, then approve in the browser
drivesync folders            # list your top-level Drive folders
drivesync select Documents Photos
drivesync sync               # run one pass now and watch what happens
```

Your folders appear in `~/GoogleDrive/Documents` and `~/GoogleDrive/Photos`.

### Keep it running while you're logged in

**Linux (systemd):**

```sh
install -Dm644 contrib/drivesync.service ~/.config/systemd/user/drivesync.service
systemctl --user daemon-reload
systemctl --user enable --now drivesync
journalctl --user -u drivesync -f
```

The service starts with your desktop session and stops when you log out. For the Flatpak, change `ExecStart` to `/usr/bin/flatpak run io.github.celso_alexandre.drivesync run`.

**macOS (launchd):** set your user name in the log path, then load it:

```sh
sed "s|/Users/YOU|$HOME|" contrib/io.github.celso_alexandre.drivesync.plist > ~/Library/LaunchAgents/io.github.celso_alexandre.drivesync.plist
launchctl load ~/Library/LaunchAgents/io.github.celso_alexandre.drivesync.plist
```

**Windows (Task Scheduler):** this runs it hidden at every sign-in:

```powershell
schtasks /Create /SC ONLOGON /TN drivesync /TR "conhost.exe --headless C:\path\to\drivesync.exe run"
```

### Commands

| Command | |
| --- | --- |
| `drivesync login` | Sign in with your own OAuth client |
| `drivesync logout` | Remove the saved sign-in from the keychain |
| `drivesync folders` | List top-level folders in My Drive. Selected ones are marked `*` |
| `drivesync select NAME...` | Start mirroring these folders |
| `drivesync unselect NAME...` | Stop mirroring them. Local copies are kept |
| `drivesync status` | Show settings and how many items are synced |
| `drivesync sync` | Run one sync pass |
| `drivesync run` | Keep syncing, polling every 30 seconds |
| `drivesync version` | Print the version |

### Settings

`config.json` in drivesync's config folder: `~/.config/drivesync` on Linux, `~/Library/Application Support/drivesync` on macOS, `%AppData%\drivesync` on Windows.

```json
{
  "root": "/home/you/GoogleDrive",
  "folders": ["Documents", "Photos"],
  "interval_seconds": 30
}
```

Changes to `folders` take effect on the next pass. If you change `root`, the running sync exits and restarts so the sandbox can be rebuilt around the new folder.

### Current limitations

- Google Docs, Sheets and Slides aren't downloaded, because they aren't real files. They stay in Drive, and a folder that holds them is never deleted from Drive.
- Only top-level folders of **My Drive** can be selected. Shared drives and "Shared with me" aren't synced.
- Changes are checked every 30 seconds rather than instantly.
- Symbolic links and special files are ignored.
- If an upload is interrupted, it starts over from the beginning on the next pass.
- On Windows, an edit that keeps both the file size and the modification time unchanged isn't noticed. Linux and macOS catch it through the inode change time.
- The Linux sandbox is implemented for x86-64 only.

### Development

The whole development workflow runs in throwaway containers (podman or docker) with **no network access, no Linux capabilities and a read-only root filesystem**. The pinned Go image is the only thing ever downloaded.

```sh
scripts/dev.sh check     # format, vet, tests with race detector, sandbox tests, build
scripts/dev.sh test      # tests only
scripts/dev.sh sandbox   # sandbox tests; fails if the kernel can't enforce it
scripts/dev.sh shell     # shell inside the dev container
```

The Go version is pinned in `.tool-versions` (asdf works with it directly) and the dev image digest in `.golang-image-digest`. CI runs the same checks on Linux, plus native tests and the sandbox self-test on macOS and Windows. Workflow steps use GitHub's own actions pinned by commit. The Flatpak is built in Flathub's `flatpak-github-actions` container image.

The test suite runs drivesync against an in-memory fake of the Drive API, which keeps revision history the way real Drive does. It covers:

- every sync rule above
- a **randomized test** (60 seeds of 30 rounds each) that mixes local and remote edits, deletes and folder removals with engine restarts. After every pass it checks that no version of any file was lost, counting Drive revisions and both trashes as kept. At the end it checks that both sides are identical. Network failures and mid-transfer interruptions are covered by separate, targeted tests, not by the randomized one.
- **escape tests (Linux)** that start a sandboxed process and confirm it can't read files outside its folders, run programs, open other ports or sockets, signal or debug other processes, or create namespaces
- a full sync run **inside** the sandbox, to prove the restrictions don't break the sync

