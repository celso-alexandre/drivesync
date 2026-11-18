package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"syscall"
)

var version = "dev"

const usage = `drivesync - two-way mirror of selected Google Drive folders

Usage:
  drivesync [--unsafe-no-sandbox] [--account ID] COMMAND

Commands:
  drivesync login                 sign in with your own OAuth client
  drivesync logout                remove the saved sign-in
  drivesync folders               list top-level folders in My Drive
  drivesync select FOLDER...      mirror these folders
  drivesync unselect FOLDER...    stop mirroring (local copies are kept)
  drivesync status                show settings and synced item counts
  drivesync sync [--allow-delete] run one sync pass
  drivesync run [--allow-delete]  keep syncing until stopped
  drivesync version               print the version
`

func main() {
	args := os.Args[1:]
	for len(args) > 0 {
		if args[0] == "--unsafe-no-sandbox" {
			sandboxDisabled = true
			args = args[1:]
			continue
		}
		if args[0] == "--account" && len(args) > 1 {
			if !accountIDPattern.MatchString(args[1]) {
				fmt.Fprintln(os.Stderr, "drivesync: account ids use lowercase letters, digits and dashes")
				os.Exit(2)
			}
			accountID = args[1]
			args = args[2:]
			continue
		}
		break
	}
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runCommand(ctx, args[0], args[1:]); err != nil {
		var code exitCodeError
		if errors.As(err, &code) {
			os.Exit(int(code))
		}
		fmt.Fprintln(os.Stderr, "drivesync:", err)
		os.Exit(1)
	}
}

func runCommand(ctx context.Context, name string, args []string) error {
	switch name {
	case "login":
		reuseFrom := ""
		if i := slices.Index(args, "--reuse-from"); i >= 0 && i+1 < len(args) {
			reuseFrom = args[i+1]
			if !accountIDPattern.MatchString(reuseFrom) {
				return errors.New("invalid account id for --reuse-from")
			}
		}
		return login(ctx, slices.Contains(args, "--stdin"), reuseFrom)
	case "logout":
		return logout()
	case "folders":
		if launched, err := maybeLaunch(ctx, append([]string{"folders"}, args...), foldersPolicy); launched {
			return err
		}
		return listFolders(ctx, slices.Contains(args, "--json"), slices.Contains(args, "--sizes"))
	case "paths":
		return printPaths()
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
		if launched, err := maybeLaunch(ctx, append([]string{name}, args...), syncPolicy); launched {
			return err
		}
		return startSync(ctx, name == "run", *allowDelete)
	case "version", "--version":
		fmt.Println("drivesync", version)
		return nil
	case "selftest-sandbox":
		return selftestSandbox(ctx, args)
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	}
	fmt.Fprint(os.Stderr, usage)
	return fmt.Errorf("unknown command %q", name)
}
