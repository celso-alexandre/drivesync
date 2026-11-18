#!/usr/bin/env bash
set -euo pipefail

usage() {
   cat >&2 <<'USAGE'
usage: scripts/gui.sh <command>

  vendor   download the locked crates into gui/vendor (host, network, no build scripts run)
  check    cargo deny, clippy and tests, offline in the KDE SDK sandbox
  build    release build of gui/target/release/drivesync-gui, offline in the KDE SDK sandbox
  shell    shell inside the KDE SDK sandbox

Builds run in the org.kde.Sdk Flatpak runtime with its Rust extension, without network
access and with only this repository visible.
USAGE
   exit 2
}

[ $# -eq 1 ] || usage
TASK="$1"
REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SDK="org.kde.Sdk//6.11"
CARGO_OFFLINE=(--offline --locked --config 'source.crates-io.replace-with="vendored"' --config 'source.vendored.directory="vendor"')

if [ "$TASK" = vendor ]; then
   cd "$REPO_DIR/gui"
   cargo vendor --locked --versioned-dirs vendor >/dev/null
   echo "vendored $(ls vendor | wc -l) crates"
   exit
fi

[ -d "$REPO_DIR/gui/vendor" ] || { echo "run scripts/gui.sh vendor first" >&2; exit 1; }

case "$TASK" in
check) inner="cargo clippy ${CARGO_OFFLINE[*]@Q} --all-targets -- -D warnings && cargo test ${CARGO_OFFLINE[*]@Q}" ;;
build) inner="cargo build ${CARGO_OFFLINE[*]@Q} --release" ;;
shell) inner="bash" ;;
*) usage ;;
esac

TTY_ARGS=()
[ -t 0 ] && [ -t 1 ] || TTY_ARGS=()

exec flatpak run --user \
   --unshare=network \
   --nofilesystem=host \
   --filesystem="$REPO_DIR" \
   --env=PATH=/usr/lib/sdk/rust-stable/bin:/usr/bin \
   --env=CARGO_HOME=/tmp/cargo-home \
   --env=CARGO_TERM_COLOR=never \
   --command=bash "$SDK" -c "cd ${REPO_DIR@Q}/gui && $inner"
