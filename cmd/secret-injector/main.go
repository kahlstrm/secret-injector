package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/kahlstrm/secret-injector/pkg/config"
	"github.com/kahlstrm/secret-injector/pkg/loader"
	"github.com/urfave/cli/v3"
)

// Shared flag definitions for config input
var (
	flagConfigFile = &cli.StringFlag{
		Name:      "config-file",
		Aliases:   []string{"f"},
		Usage:     "path to config file (use '-' for stdin)",
		TakesFile: true,
	}
	flagConfigJSON = &cli.StringFlag{
		Name:  "config-json",
		Usage: "inline config JSON string",
	}
	// Mutually exclusive group: exactly one config input must be provided
	configInputGroup = cli.MutuallyExclusiveFlags{
		Required: true,
		Flags: [][]cli.Flag{
			{flagConfigFile},
			{flagConfigJSON},
		},
	}
)

func main() {
	cmd := &cli.Command{
		Name:                  "secret-injector",
		Usage:                 "Load secrets from cloud providers into environment variables (MVP: SSM).",
		EnableShellCompletion: true,
		Commands: []*cli.Command{
			validateCmd(),
			fetchCmd(),
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		// Let cli handle formatting of errors; keep this simple
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// fetchCmd resolves secrets using the default registry and prints
// either KEY=VALUE lines or a JSON object of {ENV: VALUE} when --json is set.
func fetchCmd() *cli.Command {
	jsonFlag := &cli.BoolFlag{
		Name:  "json",
		Usage: "output JSON instead of KEY=VALUE lines",
	}
	return &cli.Command{
		Name:                   "fetch",
		Usage:                  "Resolve and print environment variable bindings",
		Flags:                  []cli.Flag{jsonFlag},
		MutuallyExclusiveFlags: []cli.MutuallyExclusiveFlags{configInputGroup},
		Action: func(ctx context.Context, c *cli.Command) error {
			cfg, err := loadConfigFromInput(c)
			if err != nil {
				return err
			}

			warn := func(_ context.Context, msg string) {
				// best-effort warning to stderr (no secret values included)
				fmt.Fprintln(os.Stderr, "warning:", msg)
			}
			reg, err := loader.Default(ctx, warn)
			if err != nil {
				return err
			}
			values, err := loader.ResolveAll(ctx, cfg, reg)
			if err != nil {
				return err
			}

			if c.Bool("json") {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(values)
			}

			// print in deterministic key order
			keys := make([]string, 0, len(values))
			for k := range values {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Printf("%s=%s\n", k, values[k])
			}
			return nil
		},
	}
}

// validateCmd returns the `validate` subcommand.
func validateCmd() *cli.Command {
	debugFlag := &cli.BoolFlag{
		Name:  "debug",
		Usage: "print parsed config in original JSON shape",
	}
	return &cli.Command{
		Name:  "validate",
		Usage: "Parse and validate the configuration (no secret fetching)",
		Flags: []cli.Flag{
			debugFlag,
		},
		MutuallyExclusiveFlags: []cli.MutuallyExclusiveFlags{configInputGroup},
		Action: func(_ context.Context, c *cli.Command) error {
			cfg, err := loadConfigFromInput(c)
			if err != nil {
				return err
			}
			if c.Bool("debug") {
				if err := printConfigAsInputShape(os.Stdout, cfg); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// loadConfigFromInput reads either --config-json or --config-file and decodes it via pkg/config.
func loadConfigFromInput(c *cli.Command) (config.Config, error) {
	inline := strings.TrimSpace(c.String("config-json"))
	path := strings.TrimSpace(c.String("config-file"))

	if inline != "" && path != "" {
		// Should be prevented by MutuallyExclusiveFlags, but keep a guard.
		return config.Config{}, errors.New("only one of --config-json or --config-file may be provided")
	}

	var r io.Reader
	switch {
	case inline != "":
		r = strings.NewReader(inline)
	case path == "-":
		r = os.Stdin
	case path != "":
		f, err := os.Open(path)
		if err != nil {
			return config.Config{}, err
		}
		defer func() { _ = f.Close() }()
		r = f
	default:
		return config.Config{}, errors.New("no config input provided")
	}

	return config.Load(r)
}

// printConfigAsInputShape emits the parsed config in the original JSON input shape.
func printConfigAsInputShape(w io.Writer, cfg config.Config) error {
	raw := struct {
		Secrets map[string]string `json:"secrets"`
	}{Secrets: make(map[string]string, len(cfg.Secrets))}

	for env, entry := range cfg.Secrets {
		raw.Secrets[env] = entry.Source + ":" + entry.Ref
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(raw)
}
