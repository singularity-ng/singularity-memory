package modeltui

import (
	"context"
	"errors"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/log"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/bubbletea"
	wishlog "github.com/charmbracelet/wish/logging"
)

func ServeSSH(ctx context.Context, addr, hostKeyPath, serverURL string) error {
	server, err := wish.NewServer(
		wish.WithAddress(addr),
		wish.WithHostKeyPath(hostKeyPath),
		wish.WithMiddleware(
			bubbletea.Middleware(func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
				return New(serverURL), []tea.ProgramOption{tea.WithAltScreen()}
			}),
			wishlog.Middleware(),
		),
	)
	if err != nil {
		return err
	}

	errs := make(chan error, 1)
	go func() {
		log.Info("starting modelwalk ssh", "addr", addr, "host_key", hostKeyPath)
		errs <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errs:
		if errors.Is(err, ssh.ErrServerClosed) {
			return nil
		}
		return err
	}
}
