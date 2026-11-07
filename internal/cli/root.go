// Package cli wires the ncli commands.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/idarciooliveira/namecheap-cli/internal/api"
	"github.com/idarciooliveira/namecheap-cli/internal/config"
	"github.com/idarciooliveira/namecheap-cli/internal/ip"
)

// Version is set at build time with -ldflags "-X .../internal/cli.Version=...".
var Version = "dev"

// Exit codes.
const (
	exitOK    = 0
	exitAPI   = 1
	exitUsage = 2
	exitAuth  = 3
)

type app struct {
	jsonFlag bool
	clientIP string
	out      io.Writer
	errOut   io.Writer
	isTTY    bool
	lastIP   string // client IP used for the last request, for error hints
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	a := &app{
		out:    os.Stdout,
		errOut: os.Stderr,
		isTTY:  term.IsTerminal(int(os.Stdout.Fd())),
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	root := a.rootCmd()
	err := root.ExecuteContext(ctx)
	if err == nil {
		return exitOK
	}
	return a.report(err)
}

func (a *app) rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "ncli",
		Short: "Read-only Namecheap CLI: list domains, expiry dates, availability and prices",
		Long: `ncli talks to the Namecheap API with your API key.

Output is a table in a terminal and JSON when piped (or with --json).
Run 'ncli auth login' first.`,
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().BoolVar(&a.jsonFlag, "json", false, "output JSON (default when stdout is not a terminal)")
	root.PersistentFlags().StringVar(&a.clientIP, "client-ip", "", "public IPv4 to send as ClientIp (default: auto-detect)")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageErr{err} })
	root.AddCommand(a.authCmd(), a.domainsCmd(), a.checkCmd(), a.priceCmd(), a.mailCmd())
	return root
}

func (a *app) json() bool { return a.jsonFlag || !a.isTTY }

// client builds an API client from config + env + flags.
func (a *app) client(ctx context.Context) (*api.Client, *config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, cfg, err
	}
	addr := a.clientIP
	if addr == "" {
		addr = cfg.ClientIP
	}
	if addr == "" {
		addr, err = ip.Public(ctx)
		if err != nil {
			return nil, cfg, fmt.Errorf("could not detect your public IPv4 (use --client-ip or NAMECHEAP_CLIENT_IP): %w", err)
		}
	}
	a.lastIP = addr
	base := api.ProductionURL
	if cfg.Sandbox {
		base = api.SandboxURL
	}
	if v := os.Getenv("NCLI_API_URL"); v != "" { // for testing against a fake server
		base = v
	}
	return api.New(base, cfg.APIUser, cfg.APIKey, cfg.Username, addr), cfg, nil
}

// emit prints v as JSON, or calls table with a tabwriter.
func (a *app) emit(v any, table func(w io.Writer)) error {
	if a.json() {
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	table(tw)
	return tw.Flush()
}

type usageErr struct{ err error }

func (u usageErr) Error() string { return u.err.Error() }

func usagef(format string, args ...any) error { return usageErr{fmt.Errorf(format, args...)} }

func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(n)(cmd, args); err != nil {
			return usageErr{err}
		}
		return nil
	}
}

func minArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := cobra.MinimumNArgs(n)(cmd, args); err != nil {
			return usageErr{err}
		}
		return nil
	}
}

// report prints err (JSON on stderr in JSON mode) and returns the exit code.
func (a *app) report(err error) int {
	code, exit, hint := "error", exitAPI, ""
	var ue usageErr
	var ae *api.Error
	switch {
	case errors.As(err, &ue):
		code, exit, hint = "usage", exitUsage, "run 'ncli --help' for usage"
	case errors.Is(err, config.ErrNotConfigured):
		code, exit, hint = "not_configured", exitAuth, "run 'ncli auth login' or set NAMECHEAP_API_USER and NAMECHEAP_API_KEY"
	case errors.As(err, &ae):
		code = ae.Code
		if ae.IsAuth() {
			exit = exitAuth
			hint = "check your API user/key with 'ncli auth status'"
		}
		if ae.IsIP() {
			hint = fmt.Sprintf("whitelist %s in Namecheap: Profile > Tools > Namecheap API Access > Whitelisted IPs", a.lastIP)
		}
	case errors.Is(err, context.Canceled):
		code, exit = "canceled", exitAPI
	}
	if a.json() {
		e := map[string]any{"code": code, "message": err.Error()}
		if hint != "" {
			e["hint"] = hint
		}
		_ = json.NewEncoder(a.errOut).Encode(map[string]any{"error": e})
	} else {
		fmt.Fprintln(a.errOut, "error:", err)
		if hint != "" {
			fmt.Fprintln(a.errOut, "hint: ", hint)
		}
	}
	return exit
}
