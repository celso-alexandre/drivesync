# drivesync

**Two-way sync of the Google Drive folders you choose, for Linux. Small, sandboxed, and with no dependencies.**

> [!WARNING]
> **Very early software. Use at your own risk.**
> drivesync is a young project with a single developer. It is heavily tested, but it has not yet been used by many people on many machines.
> Before you point it at data you care about, **keep a separate backup**. It's also a good idea to try it first on a test folder.

> [!NOTE]
> drivesync is an **unofficial**, independent project. It is not made, endorsed or supported by Google.
> Google Drive is a trademark of Google LLC. drivesync talks to Drive only through the public, documented [Google Drive API](https://developers.google.com/workspace/drive/api).

---

## Why

Google's own desktop client exists only for Windows and macOS. drivesync brings the part most people need to Linux: pick some Drive folders, get a real local copy, and keep both sides in sync.

- **Selective sync:** mirror only the top-level folders you pick, not your whole Drive.
- **Real files on disk:** they work offline, with any app, and are indexed by your desktop search.
- **Two-way:** edits, new files, renames and deletions flow in both directions.
- **Careful with your data:** nothing is ever deleted outright, and conflicts keep both versions.
- **Sandboxed by the kernel:** even a bug or a compromised build can't reach the rest of your system.
- **One static binary:** written in Go using only the standard library.

## How it keeps your data safe

| Situation | What drivesync does |
| --- | --- |
| You edit a file locally | Uploads it as a new revision. Drive keeps the old one in its version history. |
| Someone edits it in Drive | Downloads the new version. Your local copy is replaced only if you hadn't changed it. |
| Both sides changed | Keeps Drive's version in place and saves yours next to it as `name (conflict 2026-11-18 150405).ext`, which then syncs too. |
| You delete a file locally | Moves it to the **Drive trash**, where it can be restored for 30 days. |
| It is deleted in Drive | Moves your local copy to `~/GoogleDrive/.drivesync-trash/<date>/`. Nothing is erased. |
| More than half of a folder would be deleted in one pass | **Refuses** and logs a warning. Run `drivesync sync --allow-delete` if you really meant it. |
| A synced folder goes missing locally (unmounted disk, typo) | Stops syncing that folder. It **never** treats a missing folder as "delete everything". |
| You edit a file while it is being synced | Detects the change, leaves your file alone, and retries on the next pass. |
| Network or API errors | The pass stops early without changing anything else, and the next pass picks up where it left off. |

Downloads go to a temporary file first and are renamed into place only when complete, so a crash never leaves a half-written file behind.

## Security

drivesync holds a token that can read and write your Drive, so it's designed to need as little trust as possible.

**No third-party code.** drivesync uses only the Go standard library. There is no `go.sum` and no dependency tree that could be compromised, and a test fails the build if one is ever added.

**Sandboxed by the kernel.** As soon as it has read its sign-in from your keyring, drivesync locks itself down with [Landlock](https://docs.kernel.org/userspace-api/landlock.html) and a seccomp filter. From that point on, even code running inside drivesync:

- can read and write **only** your sync folder and its own state, can read its own config, and can read `/etc` and `/usr`. Your SSH keys, browser profiles, documents and the rest of your home folder are off limits.
- can open **only** outgoing TCP connections to ports 443 (HTTPS) and 53 (DNS). It can't listen for connections or use local sockets such as D-Bus or X11.
- can't **run programs**, debug or signal other processes, create namespaces, load kernel modules or BPF programs, or use io_uring.
- can't undo or loosen any of this, because the restrictions are permanent for the life of the process.

If the kernel can't enforce the sandbox, drivesync **refuses to start**. You can override that with `--unsafe-no-sandbox`, but it prints a warning every time.

**Credentials stay in your keyring.** The sign-in is saved through the Secret Service API (KWallet, GNOME Keyring), never in a plain file. drivesync ships with no OAuth credentials of its own: you use your own Google Cloud OAuth client, so no third party ever holds a key to your Drive.

**What the sandbox can't do.** Landlock limits *ports*, not *hosts*. A compromised build could still send your Drive data to some other HTTPS server. To pin drivesync to Google's servers only, add an application firewall such as [OpenSnitch](https://github.com/evilsocket/opensnitch) and allow `www.googleapis.com` and `oauth2.googleapis.com` on port 443.

## Requirements

- Linux on x86-64, **kernel 6.7 or newer** with Landlock enabled (the default on Fedora, Arch, Ubuntu 24.04+ and Debian 13+)
- `secret-tool` (package `libsecret` or `libsecret-tools`) plus a running keyring: KWallet or GNOME Keyring
- A Google account

## Install

```sh
git clone https://github.com/celso-alexandre/drivesync
cd drivesync
scripts/dev.sh build
install -Dm755 drivesync ~/.local/bin/drivesync
```

`scripts/dev.sh build` compiles inside a throwaway container (see [Development](#development)). With Go installed locally, `DRIVESYNC_RUN=direct scripts/dev.sh build` builds on the host instead.

## Set up your own Google OAuth client (one time, about 5 minutes)

Google requires every app that uses the Drive API to have an OAuth client. Creating your own is free, and it means drivesync never depends on anyone else's keys.

1. Open the [Google Cloud console](https://console.cloud.google.com/) and **create a project**, for example "drivesync".
2. Go to **APIs & Services → Library**, search for **Google Drive API** and click **Enable**.
3. Go to **Google Auth Platform → Branding**, enter an app name and your email, and save. Under **Audience**, choose **External**.
4. Still under **Audience**, click **Publish app**. Without this, Google expires your sign-in every 7 days. Since this app is only for you, Google doesn't review it. You'll just see an "unverified app" notice when you sign in.
5. Go to **Google Auth Platform → Clients → Create client**. Choose application type **Desktop app**, then copy the **Client ID** and **Client secret**.

## Use

```sh
drivesync login              # paste your client ID and secret, then approve in the browser
drivesync folders            # list your top-level Drive folders
drivesync select Documents Photos
drivesync sync               # run one pass now and watch what happens
```

Your folders appear in `~/GoogleDrive/Documents` and `~/GoogleDrive/Photos`.

### Keep it running while you're logged in

```sh
install -Dm644 contrib/drivesync.service ~/.config/systemd/user/drivesync.service
systemctl --user daemon-reload
systemctl --user enable --now drivesync
journalctl --user -u drivesync -f      # watch it work
```

The service starts with your desktop session and stops when you log out.

### Commands

| Command | |
| --- | --- |
| `drivesync login` | Sign in with your own OAuth client |
| `drivesync logout` | Remove the saved sign-in from the keyring |
| `drivesync folders` | List top-level folders in My Drive. Selected ones are marked `*` |
| `drivesync select NAME...` | Start mirroring these folders |
| `drivesync unselect NAME...` | Stop mirroring them. Local copies are kept |
| `drivesync status` | Show settings and how many items are synced |
| `drivesync sync` | Run one sync pass |
| `drivesync run` | Keep syncing, polling every 30 seconds |

### Settings

`~/.config/drivesync/config.json`:

```json
{
  "root": "/home/you/GoogleDrive",
  "folders": ["Documents", "Photos"],
  "interval_seconds": 30
}
```

Changes to `folders` take effect on the next pass. If you change `root`, the running service exits and restarts so the sandbox can be rebuilt around the new folder.

## Current limitations

- Google Docs, Sheets and Slides aren't downloaded, because they aren't real files. They stay in Drive, and a folder that holds them is never deleted from Drive.
- Only top-level folders of **My Drive** can be selected. Shared drives and "Shared with me" aren't synced.
- Changes are checked every 30 seconds rather than instantly.
- Symbolic links and special files are ignored.
- If an upload is interrupted, it starts over from the beginning on the next pass.
- Only Linux on x86-64 is supported for now.

## Development

The whole development workflow runs in throwaway containers (podman or docker) with **no network access, no Linux capabilities and a read-only root filesystem**. The pinned Go image is the only thing ever downloaded.

```sh
scripts/dev.sh check     # format, vet, tests with race detector, sandbox tests, build
scripts/dev.sh test      # tests only
scripts/dev.sh sandbox   # sandbox tests; fails if the kernel can't enforce it
scripts/dev.sh shell     # shell inside the dev container
```

The Go version and image digest are pinned in `.tool-versions`.

The test suite runs drivesync against an in-memory fake of the Drive API. It covers:

- every sync rule above
- a **randomized test** that runs thousands of mixed local and remote edits, deletes, renames, restarts and failures, and checks after every pass that no version of any file was lost and that both sides end up identical
- **escape tests** that start a sandboxed process and confirm it can't read files outside its folders, run programs, open other ports or sockets, signal or debug other processes, or create namespaces
- a full sync run **inside** the sandbox, to prove the restrictions don't break the sync

## License

[Apache License 2.0](LICENSE)
