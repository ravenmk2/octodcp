package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"
	_ "time/tzdata"

	"github.com/gin-gonic/gin"
	"github.com/ravenmk2/octodcp/internal/modules/install"
	"github.com/ravenmk2/octodcp/internal/platform/config"
	"github.com/ravenmk2/octodcp/internal/server"
	"github.com/ravenmk2/octodcp/internal/store/mysql"
	"github.com/ravenmk2/octodcp/web"
	"golang.org/x/term"
)

func Run(ctx context.Context, args []string, version string, out, errOut io.Writer) error {
	command := "serve"
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		command = args[0]
		args = args[1:]
	}
	if command == "version" {
		_, err := fmt.Fprintln(out, version)
		return err
	}
	if command == "db" || command == "setup" {
		if len(args) == 0 {
			return fmt.Errorf("%s requires a subcommand", command)
		}
		command += " " + args[0]
		args = args[1:]
	}
	if command != "serve" && command != "db migrate" && command != "setup recover" {
		return errors.New("unknown command")
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(errOut)
	var overrides config.Overrides
	flags.StringVar(&overrides.DataDir, "data-dir", "", "persistent data directory (default: <cwd>/data)")
	flags.StringVar(&overrides.Listen, "listen", "", "HTTP listen address")
	flags.StringVar(&overrides.PublicURL, "public-url", "", "public HTTP(S) origin")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	r, _, err := config.Load(overrides)
	var fileErr *config.FileError
	if err != nil && !errors.As(err, &fileErr) {
		return err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	installer, err := install.New(probeCtx, r, mysql.Open)
	cancel()
	if err != nil {
		return err
	}
	defer func() { _ = installer.Close() }()
	if command == "db migrate" {
		return installer.Migrate(ctx, os.Getenv("OCTODCP_MIGRATION_USERNAME"), os.Getenv("OCTODCP_MIGRATION_PASSWORD"))
	}
	if command == "setup recover" {
		err := installer.Recover(ctx, "")
		if errors.Is(err, install.ErrFounderPasswordRequired) {
			secret, readErr := readPassword(errOut)
			if readErr != nil {
				return readErr
			}
			return installer.Recover(ctx, secret)
		}
		return err
	}
	gin.SetMode(gin.ReleaseMode)
	handler := server.New(server.Options{Runtime: r, Installer: installer, Static: web.DistFS(), Version: version})
	listener, err := net.Listen("tcp", r.Server.Listen)
	if err != nil {
		return err
	}
	httpServer := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 150 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10, BaseContext: func(net.Listener) context.Context { return ctx }}
	logger := slog.New(slog.NewJSONHandler(errOut, nil))
	logger.Info("HTTP server started", "listen", listener.Addr().String(), "version", version, "state", installer.Status(ctx).State)
	stopped := make(chan struct{})
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
			shutdownCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				_ = httpServer.Close()
			}
		case <-stopped:
		}
	}()
	err = httpServer.Serve(listener)
	close(stopped)
	<-shutdownDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	_ = httpServer.Close()
	return err
}

func readPassword(out io.Writer) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("password entry requires an interactive terminal")
	}
	if _, err := fmt.Fprint(out, "New founder password: "); err != nil {
		return "", err
	}
	first, err := term.ReadPassword(fd)
	_, _ = fmt.Fprintln(out)
	if err != nil {
		return "", err
	}
	defer clear(first)
	if _, err := fmt.Fprint(out, "Confirm password: "); err != nil {
		return "", err
	}
	second, err := term.ReadPassword(fd)
	_, _ = fmt.Fprintln(out)
	if err != nil {
		return "", err
	}
	defer clear(second)
	if string(first) != string(second) {
		return "", errors.New("passwords do not match")
	}
	return string(first), nil
}
