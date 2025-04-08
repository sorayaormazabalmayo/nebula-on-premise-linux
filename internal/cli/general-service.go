package cli

import (
	"context"
	"flag"
	"sync"

	"github.com/peterbourgon/ff/v4"
	"github.com/saltosystems-internal/x/log"
	"github.com/sorayaormazabalmayo/general-service/internal/server"
)

// NewGeneralServiceCommand creates and returns the root CLI command.
func NewGeneralServiceCommand(logger log.Logger) ff.Command {
	fs := ff.NewFlagSet("general-service")

	return ff.Command{
		Name:      "general-service",
		ShortHelp: "This is the root command for the general-service",
		Usage:     "general-service [FLAGS] <SUBCOMMANDS> ...",
		Flags:     fs,
		Exec: func(context.Context, []string) error {
			return flag.ErrHelp
		},
		Subcommands: []*ff.Command{
			newServeCommand(logger),
		},
	}
}

// newServeCommand returns a usable ff.Command for the serve subcommand.
func newServeCommand(logger log.Logger) *ff.Command {
	// Create a configuration structure that will be populated from the flags.
	cfg := &server.Config{}

	// Create the flag set and declare all flags here.
	fs := ff.NewFlagSet("serve")
	_ = fs.String(0, "config", "", "config file in yaml format")
	fs.StringVar(&cfg.HTTPAddr, 0, "http-addr", "localhost:8000", "HTTP address")
	fs.StringVar(&cfg.InternatHTTPAddr, 0, "internal-http-addr", "localhost:9000", "Internal HTTP address")
	fs.BoolVarDefault(&cfg.Debug, 0, "debug", false, "Enable debug")
	fs.BoolVarDefault(&cfg.AutoUpdate, 0, "auto-update", false, "Enable updater")
	fs.StringVar(&cfg.MetadataURL, 0, "metadata-url", "https://sorayaormazabalmayo.github.io/TUF_Repository_YubiKey_Vault/metadata", "Metadata URL")

	cmd := &ff.Command{
		Name:      "serve",
		ShortHelp: "Run both serve and update concurrently",
		Flags:     fs,
		Exec: func(ctx context.Context, args []string) error {
			var wg sync.WaitGroup
			wg.Add(2)

			// Launch the server using the parsed config.
			go func() {
				defer wg.Done()
				if cfg.Debug {
					if err := logger.SetAllowedLevel(log.AllowDebug()); err != nil {
						logger.Error("failed to set debug level", "error", err)
					}
				}
				s, err := server.NewServer(cfg, logger)
				if err != nil {
					logger.Error("failed to create server", "error", err)
					return
				}
				if err := s.Run(); err != nil {
					logger.Error("server error", "error", err)
				}
			}()

			// Wait for both goroutines to finish.
			wg.Wait()
			return nil
		},
	}
	return cmd
}
