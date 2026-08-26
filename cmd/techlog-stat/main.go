package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"techlog-stat/internal/app"
	"techlog-stat/internal/cli"
	"techlog-stat/internal/config"
)

func main() {
	cfg, err := cli.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if cfg.Report == config.ReportServe {
		fmt.Fprintf(os.Stdout, "techlog-stat web interface: http://%s\n", cfg.Listen)
		fmt.Fprintln(os.Stdout, "Press Ctrl+C to stop.")
	}
	if err := app.RunContext(ctx, cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
