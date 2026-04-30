package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/singularity-ng/singularity-memory/go/internal/modeltui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	serverURL := fs.String("server", "http://127.0.0.1:8888", "Singularity Memory server URL")
	sshAddr := fs.String("ssh", "", "serve TUI over SSH at this address, e.g. :23235")
	hostKey := fs.String("host-key", ".modelwalk_ed25519", "SSH host key path")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}

	if *sshAddr != "" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return modeltui.ServeSSH(ctx, *sshAddr, *hostKey, *serverURL)
	}

	_, err := tea.NewProgram(modeltui.New(*serverURL), tea.WithAltScreen()).Run()
	return err
}
