#!/usr/bin/env bash
set -euo pipefail

usage() {
   cat >&2 <<'EOF'
usage: scripts/dev.sh <command>

  check     everything CI runs: format, vet, tests (race + sandbox), build
  test      unit and randomized tests with the race detector
  sandbox   sandbox tests (fails if the kernel sandbox cannot be enforced)
  build     static, sandbox-ready binary at ./drivesync
  fmt       format the code
  shell     interactive shell in the dev container

Runs in a throwaway container with no network, no capabilities and a
read-only root filesystem. DRIVESYNC_RUN=direct runs on the host instead.
EOF
   exit 2
}

[ $# -eq 1 ] || usage
TASK="$1"
REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

task() {
   case "$1" in
   fmt) gofmt -w . ;;
   vet)
      unformatted="$(gofmt -l .)"
      if [ -n "$unformatted" ]; then
         echo "not gofmt-ed: $unformatted" >&2
         return 1
      fi
      go vet ./...
      ;;
   test) CGO_ENABLED=1 go test -race -count=1 ./... ;;
   sandbox) CGO_ENABLED=0 DRIVESYNC_REQUIRE_SANDBOX=1 go test -count=1 -run 'Sandbox' -v ./... | grep -E '^(---|ok|FAIL|PASS)|_test.go' ;;
   build) CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o drivesync . ;;
   check)
      task vet
      task test
      task sandbox
      task build
      echo "all checks passed"
      ;;
   shell) bash ;;
   *) usage ;;
   esac
}

if [ "${DRIVESYNC_IN_CONTAINER:-0}" = 1 ] || [ "${DRIVESYNC_RUN:-container}" = direct ]; then
   cd "$REPO_DIR"
   task "$TASK"
   exit
fi

case "$TASK" in
check | test | sandbox | build | fmt | vet | shell) ;;
*) usage ;;
esac

GO_VERSION="$(awk '$1 == "golang" { print $2; exit }' "$REPO_DIR/.tool-versions")"
GO_IMAGE="${DRIVESYNC_GO_IMAGE:-docker.io/library/golang:$GO_VERSION@$(cat "$REPO_DIR/.golang-image-digest")}"

ENGINE=""
for candidate in ${CONTAINER_ENGINE:-podman docker}; do
   if command -v "$candidate" >/dev/null 2>&1 && "$candidate" info >/dev/null 2>&1; then
      ENGINE="$candidate"
      break
   fi
done
if [ -z "$ENGINE" ]; then
   echo "No working container engine (tried: ${CONTAINER_ENGINE:-podman docker}). DRIVESYNC_RUN=direct runs on the host." >&2
   exit 1
fi

if "$ENGINE" --version 2>/dev/null | grep -qi podman; then
   USER_ARGS=(--userns=keep-id)
elif "$ENGINE" info --format '{{.SecurityOptions}}' 2>/dev/null | grep -q rootless; then
   USER_ARGS=()
else
   USER_ARGS=(--user "$(id -u):$(id -g)")
fi

if ! "$ENGINE" image inspect "$GO_IMAGE" >/dev/null 2>&1; then
   echo "Pulling $GO_IMAGE (the only network access the dev setup needs)..." >&2
   "$ENGINE" pull -q "$GO_IMAGE" >/dev/null
fi

if ! "$ENGINE" volume inspect drivesync-gocache >/dev/null 2>&1; then
   "$ENGINE" volume create drivesync-gocache >/dev/null
   if [ "${#USER_ARGS[@]}" -gt 0 ] && [ "${USER_ARGS[0]}" = --user ]; then
      "$ENGINE" run --rm --network=none -v drivesync-gocache:/cache "$GO_IMAGE" chown "$(id -u):$(id -g)" /cache
   fi
fi

TTY_ARGS=()
[ -t 0 ] && [ -t 1 ] && TTY_ARGS=(-it)

exec "$ENGINE" run --rm --init --pull=never \
   --network=none \
   --cap-drop=all \
   --security-opt=no-new-privileges \
   --security-opt=label=disable \
   --read-only \
   --tmpfs /tmp:rw,exec,nosuid,nodev,size=2g \
   "${USER_ARGS[@]}" "${TTY_ARGS[@]}" \
   -v "$REPO_DIR:/src" \
   -v "$REPO_DIR/.git:/src/.git:ro" \
   -v drivesync-gocache:/cache \
   -e HOME=/tmp/home \
   -e GOCACHE=/cache/go-build \
   -e GOMODCACHE=/tmp/gomod \
   -e GOPROXY=off \
   -e GOFLAGS=-buildvcs=false \
   -e GOTOOLCHAIN=local \
   -e GOTELEMETRY=off \
   -e DRIVESYNC_IN_CONTAINER=1 \
   -w /src \
   "$GO_IMAGE" scripts/dev.sh "$TASK"
