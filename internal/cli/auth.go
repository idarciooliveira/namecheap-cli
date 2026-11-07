package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/idarciooliveira/namecheap-cli/internal/config"
)

func (a *app) authCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Save and verify your Namecheap API credentials"}
	cmd.AddCommand(a.authLoginCmd(), a.authStatusCmd(), a.authLogoutCmd())
	return cmd
}

func (a *app) authLoginCmd() *cobra.Command {
	var username, apiUser string
	var sandbox, keyStdin bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Save your API key and username, then verify them",
		Long: `Prompts for your Namecheap username and API key and saves them to the
config file (readable only by you). The API key input is hidden.

Non-interactive:  echo "$KEY" | ncli auth login --username you --api-key-stdin`,
		Args: exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			in := bufio.NewReader(os.Stdin)
			stdinTTY := term.IsTerminal(int(os.Stdin.Fd()))

			var key string
			if keyStdin {
				b, err := io.ReadAll(in)
				if err != nil {
					return err
				}
				key = strings.TrimSpace(string(b))
			}
			if username == "" {
				if !stdinTTY || keyStdin {
					return usagef("--username is required when not running interactively")
				}
				username = prompt(in, "Namecheap username: ")
			}
			if username == "" {
				return usagef("username is required")
			}
			if key == "" {
				if !stdinTTY {
					return usagef("use --api-key-stdin to pass the key when not running interactively")
				}
				fmt.Fprint(os.Stderr, "API key (input hidden): ")
				b, err := term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Fprintln(os.Stderr)
				if err != nil {
					return err
				}
				key = strings.TrimSpace(string(b))
			}
			if key == "" {
				return usagef("API key is required")
			}
			if apiUser == "" {
				apiUser = username
			}

			existing, err := config.LoadFile()
			if err != nil {
				return err
			}
			existing.APIUser, existing.APIKey, existing.Username, existing.Sandbox = apiUser, key, username, sandbox
			path, err := config.Save(existing)
			if err != nil {
				return err
			}
			if !a.json() {
				fmt.Fprintf(a.out, "Saved credentials to %s\nVerifying...\n", path)
			}
			return a.runStatus(cmd)
		},
	}
	cmd.Flags().StringVar(&username, "username", "", "Namecheap account username")
	cmd.Flags().StringVar(&apiUser, "api-user", "", "API user (default: same as username)")
	cmd.Flags().BoolVar(&sandbox, "sandbox", false, "use the sandbox environment")
	cmd.Flags().BoolVar(&keyStdin, "api-key-stdin", false, "read the API key from stdin")
	return cmd
}

func prompt(in *bufio.Reader, label string) string {
	fmt.Fprint(os.Stderr, label)
	s, _ := in.ReadString('\n')
	return strings.TrimSpace(s)
}

type statusOut struct {
	OK                        bool   `json:"ok"`
	ConfigPath                string `json:"configPath"`
	Environment               string `json:"environment"`
	APIUser                   string `json:"apiUser"`
	Username                  string `json:"username"`
	APIKey                    string `json:"apiKey"`
	ClientIP                  string `json:"clientIp"`
	Currency                  string `json:"currency,omitempty"`
	AvailableBalance          string `json:"availableBalance,omitempty"`
	FundsRequiredForAutoRenew string `json:"fundsRequiredForAutoRenew,omitempty"`
}

func (a *app) authStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check that the API key, username and current IP work",
		Args:  exactArgs(0),
		RunE:  func(cmd *cobra.Command, _ []string) error { return a.runStatus(cmd) },
	}
}

func (a *app) runStatus(cmd *cobra.Command) error {
	ctx := cmd.Context()
	path, _ := config.Path()
	c, cfg, err := a.client(ctx)
	if err != nil {
		return err
	}
	b, err := c.GetBalances(ctx)
	if err != nil {
		if !a.json() {
			fmt.Fprintf(a.out, "Environment: %s\nAPI user:    %s\nClient IP:   %s\n", cfg.Environment(), cfg.APIUser, a.lastIP)
		}
		return err
	}
	s := statusOut{
		OK:                        true,
		ConfigPath:                path,
		Environment:               cfg.Environment(),
		APIUser:                   cfg.APIUser,
		Username:                  cfg.Username,
		APIKey:                    mask(cfg.APIKey),
		ClientIP:                  a.lastIP,
		Currency:                  b.Currency,
		AvailableBalance:          b.AvailableBalance,
		FundsRequiredForAutoRenew: b.FundsRequiredForAutoRenew,
	}
	return a.emit(s, func(w io.Writer) {
		fmt.Fprintf(w, "Status:\tconnected\n")
		fmt.Fprintf(w, "Environment:\t%s\n", s.Environment)
		fmt.Fprintf(w, "API user:\t%s\n", s.APIUser)
		fmt.Fprintf(w, "Username:\t%s\n", s.Username)
		fmt.Fprintf(w, "API key:\t%s\n", s.APIKey)
		fmt.Fprintf(w, "Client IP:\t%s\n", s.ClientIP)
		fmt.Fprintf(w, "Balance:\t%s %s\n", s.AvailableBalance, s.Currency)
		fmt.Fprintf(w, "Needed for auto-renew (90d):\t%s %s\n", s.FundsRequiredForAutoRenew, s.Currency)
		fmt.Fprintf(w, "Config:\t%s\n", s.ConfigPath)
	})
}

func (a *app) authLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Delete the saved credentials",
		Args:  exactArgs(0),
		RunE: func(_ *cobra.Command, _ []string) error {
			path, err := config.Remove()
			if err != nil {
				return err
			}
			return a.emit(map[string]any{"ok": true, "removed": path}, func(w io.Writer) {
				fmt.Fprintf(w, "Removed %s\n", path)
			})
		},
	}
}

func mask(key string) string {
	if len(key) <= 4 {
		return strings.Repeat("*", len(key))
	}
	return strings.Repeat("*", 8) + key[len(key)-4:]
}
