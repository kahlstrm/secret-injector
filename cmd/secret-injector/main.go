package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"

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
	flagConfig = &cli.StringFlag{
		Name:  "config",
		Usage: "inline config string (YAML or JSON)",
	}
)

func varsFlag() *cli.StringSliceFlag {
	return &cli.StringSliceFlag{
		Name:  "var",
		Usage: "ref substitution variable in NAME=VALUE form (repeatable)",
	}
}

func warnToStderr(_ context.Context, msg string) {
	fmt.Fprintln(os.Stderr, "warning:", msg)
}

func main() {
	cmd := &cli.Command{
		Name:                  "secret-injector",
		Usage:                 "Load secrets from cloud providers into environment variables.",
		EnableShellCompletion: true,
		Commands: []*cli.Command{
			validateCmd(),
			fetchCmd(),
			execCmd(),
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		// Let cli handle formatting of errors; keep this simple
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// fetchCmd resolves secrets using the default registry and prints
// in the specified format: env (default), json, or export.
func fetchCmd() *cli.Command {
	formatFlag := &cli.StringFlag{
		Name:  "format",
		Usage: "output format: env, json, export",
		Value: "env",
	}
	vars := varsFlag()
	return &cli.Command{
		Name:  "fetch",
		Usage: "Resolve and print environment variable bindings",
		Flags: []cli.Flag{formatFlag, vars, flagConfigFile, flagConfig},
		Action: func(ctx context.Context, c *cli.Command) error {
			cfg, err := loadConfigFromInput(c)
			if err != nil {
				return err
			}

			format := c.String("format")
			if format != "env" && format != "json" && format != "export" {
				return fmt.Errorf("unknown format %q: must be env, json, or export", format)
			}

			reg, err := loader.Default(ctx, warnToStderr)
			if err != nil {
				return err
			}
			values, err := loader.ResolveAll(ctx, cfg, reg, warnToStderr)
			if err != nil {
				return err
			}

			switch format {
			case "json":
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(values)
			case "export":
				keys := sortedKeys(values)
				for _, k := range keys {
					fmt.Printf("export %s=%s\n", k, shellQuote(values[k]))
				}
			default: // env
				keys := sortedKeys(values)
				for _, k := range keys {
					fmt.Printf("%s=%s\n", k, values[k])
				}
			}
			return nil
		},
	}
}

// execCmd returns the `exec` subcommand that loads secrets and executes a command.
func execCmd() *cli.Command {
	vars := varsFlag()
	return &cli.Command{
		Name:      "exec",
		Usage:     "Load secrets and execute a command with them as environment variables",
		ArgsUsage: "-- COMMAND [ARGS...]",
		Flags:     []cli.Flag{vars, flagConfigFile, flagConfig},
		Action: func(ctx context.Context, c *cli.Command) error {
			args := c.Args().Slice()
			if len(args) == 0 {
				return errors.New("no command specified; usage: exec [flags] -- COMMAND [ARGS...]")
			}

			cfg, err := loadConfigFromInput(c)
			if err != nil {
				return err
			}

			reg, err := loader.Default(ctx, warnToStderr)
			if err != nil {
				return err
			}
			secrets, err := loader.ResolveAll(ctx, cfg, reg, warnToStderr)
			if err != nil {
				return err
			}

			env := mergeEnv(os.Environ(), secrets)

			binary, err := exec.LookPath(args[0])
			if err != nil {
				return fmt.Errorf("command not found: %s", args[0])
			}

			return syscall.Exec(binary, args, env)
		},
	}
}

// validateCmd returns the `validate` subcommand.
func validateCmd() *cli.Command {
	debugFlag := &cli.BoolFlag{
		Name:  "debug",
		Usage: "print parsed config in original JSON shape",
	}
	vars := varsFlag()
	return &cli.Command{
		Name:  "validate",
		Usage: "Parse and validate the configuration (no secret fetching)",
		Flags: []cli.Flag{
			debugFlag,
			vars,
			flagConfigFile,
			flagConfig,
		},
		Action: func(ctx context.Context, c *cli.Command) error {
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

// loadConfigFromInput reads either --config or --config-file,
// validates --var assignments, and decodes it via pkg/config with ref substitution.
func loadConfigFromInput(c *cli.Command) (config.Config, error) {
	inline := strings.TrimSpace(c.String("config"))
	path := strings.TrimSpace(c.String("config-file"))
	vars, err := parseVars(c.StringSlice("var"))
	if err != nil {
		return config.Config{}, err
	}

	if inline != "" && path != "" {
		return config.Config{}, errors.New("only one of --config or --config-file may be provided")
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

	return config.Load(r, config.WithVars(vars))
}

func parseVars(values []string) (map[string]string, error) {
	out := make(map[string]string, len(values))
	for _, raw := range values {
		name, value, ok := strings.Cut(raw, "=")
		if !ok || name == "" {
			return nil, fmt.Errorf("invalid --var %q: expected NAME=VALUE", raw)
		}

		if _, exists := out[name]; exists {
			return nil, fmt.Errorf("duplicate --var name %q", name)
		}
		out[name] = value
	}
	return out, nil
}

// mergeEnv merges secrets into the current environment.
// Secrets override existing environment variables with the same key.
func mergeEnv(current []string, secrets map[string]string) []string {
	env := make(map[string]string, len(current))
	for _, e := range current {
		for i := 0; i < len(e); i++ {
			if e[i] == '=' {
				env[e[:i]] = e[i+1:]
				break
			}
		}
	}
	for k, v := range secrets {
		env[k] = v
	}
	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	return result
}

// shellQuote wraps a value in single quotes, escaping embedded single quotes.
// This produces output safe for POSIX shell sourcing.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// sortedKeys returns the keys of a map in sorted order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// printConfigAsInputShape emits the parsed config in the original JSON input shape.
func printConfigAsInputShape(w io.Writer, cfg config.Config) error {
	raw := struct {
		Secrets  map[string]string `json:"secrets"`
		Optional []string          `json:"optional,omitempty"`
	}{Secrets: make(map[string]string, len(cfg.Secrets))}

	for env, entry := range cfg.Secrets {
		raw.Secrets[env] = entry.Source + ":" + entry.Ref
		if entry.Optional {
			raw.Optional = append(raw.Optional, env)
		}
	}
	sort.Strings(raw.Optional)

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(raw)
}
