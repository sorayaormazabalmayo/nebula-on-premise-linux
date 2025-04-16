package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
	"github.com/peterbourgon/ff/v4/ffyaml"
	sdlog "github.com/saltosystems-internal/x/log/stackdriver"
	"github.com/sorayaormazabalmayo/general-service/internal/cli"
	//"github.com/kardianos/minwinsvc"
)

func main() {

	// Create new logger
	logger := sdlog.New()

	// Create command
	generalServiceCmd := cli.NewGeneralServiceCommand(logger)

	// Control aspects of parsing behaviour
	opts := []ff.Option{
		ff.WithConfigFileFlag("config"),
		ff.WithConfigFileParser(ffyaml.Parse),
	}

	// Run CLI command
	if err := generalServiceCmd.ParseAndRun(context.Background(), os.Args[1:], opts...); err != nil {
		if errors.Is(err, ff.ErrHelp) || errors.Is(err, ff.ErrDuplicateFlag) || errors.Is(err, ff.ErrAlreadyParsed) || errors.Is(err, ff.ErrUnknownFlag) || errors.Is(err, ff.ErrNotParsed) {
			fmt.Fprintf(os.Stderr, "\n%s\n", ffhelp.Command(&generalServiceCmd))
		}

		if !errors.Is(err, ff.ErrHelp) {
			logger.Error(err)
		}
		os.Exit(1)
	}
}
