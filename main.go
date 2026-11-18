package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

const usage = `drivesync - two-way mirror of selected Google Drive folders

Usage:
  drivesync [--unsafe-no-sandbox] COMMAND

Commands:
  drivesync login                 sign in with your own OAuth client
  drivesync logout                remove the saved sign-in
  drivesync folders               list top-level folders in My Drive
  drivesync select FOLDER...      mirror these folders
  drivesync unselect FOLDER...    stop mirroring (local copies are kept)
  drivesync status                show settings and synced item counts
  drivesync sync [--allow-delete] run one sync pass
  drivesync run [--allow-delete]  keep syncing until stopped
`

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "--unsafe-no-sandbox" {
		sandboxDisabled = true
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runCommand(ctx, args[0], args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "drivesync:", err)
		os.Exit(1)
	}
}

func runCommand(ctx context.Context, name string, args []string) error {
	switch name {
	case "login":
		return login(ctx)
	case "logout":
		return logout()
	case "folders":
		return listFolders(ctx)
	case "select":
		return changeSelection(args, true)
	case "unselect":
		return changeSelection(args, false)
	case "status":
		return status()
	case "sync", "run":
		flags := flag.NewFlagSet(name, flag.ContinueOnError)
		allowDelete := flags.Bool("allow-delete", false, "allow deleting more than half of a folder in one pass")
		if err := flags.Parse(args); err != nil {
			return err
		}
		return startSync(ctx, name == "run", *allowDelete)
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	}
	fmt.Fprint(os.Stderr, usage)
	return fmt.Errorf("unknown command %q", name)
}
