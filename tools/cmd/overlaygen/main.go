// Package main provides overlaygen, a tool for generating Go build overlays.
//
// overlaygen supports two modes:
// - config: Process JSON configuration with predefined replacements
// - main: Convert main packages to importable packages
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/urfave/cli/v2"
	slogctx "github.com/veqryn/slog-context"
)

func main() {
	app := App()

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func App() *cli.App {
	app := &cli.App{
		Name:  "overlaygen",
		Usage: "Generate Go build overlays",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "verbose",
				Usage: "Enable detailed logging",
			},
			&cli.StringFlag{
				Name:  "tmpdir",
				Usage: "Directory to use for temporary files",
			},
			&cli.StringFlag{
				Name:  "append-to",
				Usage: "Append to an existing overlay.json file",
			},
		},
		Before: func(c *cli.Context) error {
			ctx := setupContext(c.Bool("verbose"))
			c.Context = ctx

			tempDir := c.String("tmpdir")
			if tempDir == "" {
				tempDir, err := os.MkdirTemp("", "overlaygen")
				if err != nil {
					return fmt.Errorf("creating temporary directory: %w", err)
				}
				c.Set("tmpdir", tempDir)
			}

			return nil
		},
		Commands: []*cli.Command{
			{
				Name:      "config",
				Usage:     "Process JSON configuration with predefined replacements",
				ArgsUsage: "<config.json>",
				Action: func(c *cli.Context) error {
					if c.NArg() != 2 {
						return fmt.Errorf("requires exactly 2 arguments: <config.json>")
					}

					configPath := c.Args().Get(0)
					if configPath == "" {
						return fmt.Errorf("config.json is required")
					}

					tempDir := c.String("tmpdir")

					overlayPath, err := runConfigModeFromFile(c.Context, configPath, tempDir, c.String("append-to"))
					if err != nil {
						return err
					}

					fmt.Print(overlayPath)
					return nil
				},
			},
			{
				Name:      "main",
				Usage:     "Convert main packages commented with //go:overlay to importable package",
				ArgsUsage: "<package>",
				Action: func(c *cli.Context) error {
					if c.NArg() != 1 {
						return fmt.Errorf("requires exactly 1 argument: <package>")
					}

					pkg := c.Args().Get(0)
					if pkg == "" {
						return fmt.Errorf("package is required")
					}

					tempDir := c.String("tmpdir")

					overlayPath, err := runSourceMainMode(c.Context, pkg, tempDir, c.String("append-to"))
					if err != nil {
						return err
					}

					fmt.Print(overlayPath)
					return nil
				},
			},
		},
	}

	return app
}

// setupContext creates a context with appropriate logging based on verbose flag
func setupContext(verbose bool) context.Context {
	var logger *slog.Logger
	if verbose {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	} else {
		logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	}

	ctx := context.Background()
	return slogctx.NewCtx(ctx, logger)
}
