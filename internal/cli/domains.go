package cli

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/idarciooliveira/namecheap-cli/internal/api"
)

type domainOut struct {
	Domain       string `json:"domain"`
	Created      string `json:"created"`
	Expires      string `json:"expires"`
	DaysLeft     *int   `json:"daysLeft"`
	Expired      bool   `json:"expired"`
	AutoRenew    bool   `json:"autoRenew"`
	Locked       bool   `json:"locked"`
	Privacy      string `json:"privacy"`
	Premium      bool   `json:"premium"`
	NamecheapDNS bool   `json:"namecheapDns"`
}

func toDomainOut(d api.Domain, now time.Time) domainOut {
	o := domainOut{
		Domain:       d.Name,
		Created:      isoDate(d.Created),
		Expires:      isoDate(d.Expires),
		Expired:      bool(d.IsExpired),
		AutoRenew:    bool(d.AutoRenew),
		Locked:       bool(d.IsLocked),
		Privacy:      strings.ToLower(d.WhoisGuard),
		Premium:      bool(d.IsPremium),
		NamecheapDNS: bool(d.IsOurDNS),
	}
	if t, ok := parseDate(d.Expires); ok {
		n := daysUntil(t, now)
		o.DaysLeft = &n
		if n < 0 {
			o.Expired = true
		}
	}
	return o
}

func days(o domainOut) string {
	if o.DaysLeft == nil {
		return "?"
	}
	return fmt.Sprint(*o.DaysLeft)
}

func domainTable(w io.Writer, rows []domainOut) {
	fmt.Fprintln(w, "DOMAIN\tEXPIRES\tDAYS\tAUTO-RENEW\tLOCKED\tPRIVACY\t")
	for _, r := range rows {
		flag := ""
		switch {
		case r.Expired:
			flag = "EXPIRED"
		case r.DaysLeft != nil && *r.DaysLeft <= 30 && !r.AutoRenew:
			flag = "⚠ renew soon"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Domain, r.Expires, days(r), yesNo(r.AutoRenew), yesNo(r.Locked), r.Privacy, flag)
	}
}

func (a *app) domainsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "domains", Aliases: []string{"d"}, Short: "List and inspect the domains in your account"}
	cmd.AddCommand(a.domainsListCmd(), a.domainsCountCmd(), a.domainsExpiringCmd(), a.domainsInfoCmd())
	return cmd
}

func (a *app) domainsListCmd() *cobra.Command {
	var search, sortBy, listType string
	var desc bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List every domain with expiry date and status",
		Args:    exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			sortParam, err := sortParam(sortBy, desc)
			if err != nil {
				return err
			}
			lt := strings.ToUpper(listType)
			if lt != "ALL" && lt != "EXPIRING" && lt != "EXPIRED" {
				return usagef("--type must be all, expiring or expired")
			}
			c, _, err := a.client(cmd.Context())
			if err != nil {
				return err
			}
			ds, err := c.ListDomains(cmd.Context(), api.ListOptions{ListType: lt, Search: search, SortBy: sortParam})
			if err != nil {
				return err
			}
			now := time.Now()
			rows := make([]domainOut, 0, len(ds))
			for _, d := range ds {
				rows = append(rows, toDomainOut(d, now))
			}
			return a.emit(map[string]any{"total": len(rows), "domains": rows}, func(w io.Writer) {
				domainTable(w, rows)
				fmt.Fprintf(w, "\n%d domain(s)\n", len(rows))
			})
		},
	}
	cmd.Flags().StringVarP(&search, "search", "s", "", "only domains containing this keyword")
	cmd.Flags().StringVar(&sortBy, "sort", "name", "sort by name, expire or created")
	cmd.Flags().BoolVar(&desc, "desc", false, "reverse the sort order")
	cmd.Flags().StringVar(&listType, "type", "all", "all, expiring or expired (Namecheap's own filter)")
	return cmd
}

func sortParam(s string, desc bool) (string, error) {
	var p string
	switch strings.ToLower(s) {
	case "name", "":
		p = "NAME"
	case "expire", "expires", "expiry":
		p = "EXPIREDATE"
	case "created", "create":
		p = "CREATEDATE"
	default:
		return "", usagef("--sort must be name, expire or created")
	}
	if desc {
		p += "_DESC"
	}
	return p, nil
}

type countOut struct {
	Total        int `json:"total"`
	Active       int `json:"active"`
	Expired      int `json:"expired"`
	ExpiringIn30 int `json:"expiringIn30Days"`
	ExpiringIn60 int `json:"expiringIn60Days"`
	ExpiringIn90 int `json:"expiringIn90Days"`
	AutoRenewOn  int `json:"autoRenewOn"`
	AutoRenewOff int `json:"autoRenewOff"`
}

func (a *app) domainsCountCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "count",
		Short: "Count domains: total, expired, expiring soon, auto-renew",
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, _, err := a.client(cmd.Context())
			if err != nil {
				return err
			}
			ds, err := c.ListDomains(cmd.Context(), api.ListOptions{})
			if err != nil {
				return err
			}
			now := time.Now()
			var s countOut
			for _, d := range ds {
				o := toDomainOut(d, now)
				s.Total++
				if o.AutoRenew {
					s.AutoRenewOn++
				} else {
					s.AutoRenewOff++
				}
				if o.Expired {
					s.Expired++
					continue
				}
				s.Active++
				if o.DaysLeft != nil {
					if *o.DaysLeft <= 30 {
						s.ExpiringIn30++
					}
					if *o.DaysLeft <= 60 {
						s.ExpiringIn60++
					}
					if *o.DaysLeft <= 90 {
						s.ExpiringIn90++
					}
				}
			}
			return a.emit(s, func(w io.Writer) {
				fmt.Fprintf(w, "Total:\t%d\n", s.Total)
				fmt.Fprintf(w, "Active:\t%d\n", s.Active)
				fmt.Fprintf(w, "Expired:\t%d\n", s.Expired)
				fmt.Fprintf(w, "Expiring in 30 days:\t%d\n", s.ExpiringIn30)
				fmt.Fprintf(w, "Expiring in 60 days:\t%d\n", s.ExpiringIn60)
				fmt.Fprintf(w, "Expiring in 90 days:\t%d\n", s.ExpiringIn90)
				fmt.Fprintf(w, "Auto-renew on / off:\t%d / %d\n", s.AutoRenewOn, s.AutoRenewOff)
			})
		},
	}
}

func (a *app) domainsExpiringCmd() *cobra.Command {
	var within string
	var includeExpired bool
	cmd := &cobra.Command{
		Use:   "expiring",
		Short: "Domains expiring within a window, soonest first",
		Example: `  ncli domains expiring
  ncli domains expiring --within 90d
  ncli domains expiring --within 6m --include-expired`,
		Args: exactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			n, err := parseDays(within)
			if err != nil {
				return usageErr{err}
			}
			c, _, err := a.client(cmd.Context())
			if err != nil {
				return err
			}
			ds, err := c.ListDomains(cmd.Context(), api.ListOptions{})
			if err != nil {
				return err
			}
			now := time.Now()
			var rows []domainOut
			for _, d := range ds {
				o := toDomainOut(d, now)
				if o.DaysLeft == nil {
					continue
				}
				if o.Expired && !includeExpired {
					continue
				}
				if *o.DaysLeft <= n {
					rows = append(rows, o)
				}
			}
			sort.SliceStable(rows, func(i, j int) bool { return *rows[i].DaysLeft < *rows[j].DaysLeft })
			if rows == nil {
				rows = []domainOut{}
			}
			return a.emit(map[string]any{"withinDays": n, "total": len(rows), "domains": rows}, func(w io.Writer) {
				if len(rows) == 0 {
					fmt.Fprintf(w, "No domains expire in the next %d days.\n", n)
					return
				}
				domainTable(w, rows)
				fmt.Fprintf(w, "\n%d domain(s) expiring within %d days\n", len(rows), n)
			})
		},
	}
	cmd.Flags().StringVarP(&within, "within", "w", "60d", "window: 30d, 8w, 3m, 1y")
	cmd.Flags().BoolVar(&includeExpired, "include-expired", false, "also show domains that already expired")
	return cmd
}

type infoOut struct {
	Domain       string   `json:"domain"`
	ID           string   `json:"id"`
	Status       string   `json:"status"`
	Owner        string   `json:"owner"`
	Created      string   `json:"created"`
	Expires      string   `json:"expires"`
	DaysLeft     *int     `json:"daysLeft"`
	AutoRenew    *bool    `json:"autoRenew"`
	Locked       *bool    `json:"locked"`
	Premium      bool     `json:"premium"`
	Privacy      bool     `json:"privacy"`
	PrivacyUntil string   `json:"privacyExpires,omitempty"`
	DNSProvider  string   `json:"dnsProvider"`
	NamecheapDNS bool     `json:"namecheapDns"`
	Nameservers  []string `json:"nameservers"`
}

func (a *app) domainsInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info <domain>",
		Short: "Details for one domain in your account",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			domain := normalizeDomain(args[0])
			ctx := cmd.Context()
			c, _, err := a.client(ctx)
			if err != nil {
				return err
			}
			info, err := c.GetInfo(ctx, domain)
			if err != nil {
				var ae *api.Error
				if errors.As(err, &ae) {
					switch ae.Code {
					case "2030166", "2019166", "2016166", "4011103":
						ae.Message = domain + " is not in your Namecheap account (Namecheap says: " + ae.Message + ")"
					}
				}
				return err
			}
			now := time.Now()
			o := infoOut{
				Domain:       info.DomainName,
				ID:           info.ID,
				Status:       info.Status,
				Owner:        info.OwnerName,
				Created:      isoDate(info.Created),
				Expires:      isoDate(info.Expires),
				Premium:      bool(info.IsPremium),
				Privacy:      bool(info.Whoisguard.Enabled),
				PrivacyUntil: isoDate(info.Whoisguard.Expires),
				DNSProvider:  info.DNS.ProviderType,
				NamecheapDNS: bool(info.DNS.IsUsingOurDNS),
				Nameservers:  info.DNS.Nameservers,
			}
			if o.Nameservers == nil {
				o.Nameservers = []string{}
			}
			if t, ok := parseDate(info.Expires); ok {
				n := daysUntil(t, now)
				o.DaysLeft = &n
			}
			// getInfo has no auto-renew flag; getList does.
			if ds, err := c.ListDomains(ctx, api.ListOptions{Search: domain}); err == nil {
				for _, d := range ds {
					if strings.EqualFold(d.Name, domain) {
						ar, lk := bool(d.AutoRenew), bool(d.IsLocked)
						o.AutoRenew, o.Locked = &ar, &lk
					}
				}
			}
			return a.emit(o, func(w io.Writer) {
				fmt.Fprintf(w, "Domain:\t%s\n", o.Domain)
				fmt.Fprintf(w, "Status:\t%s\n", o.Status)
				fmt.Fprintf(w, "Created:\t%s\n", o.Created)
				left := "?"
				if o.DaysLeft != nil {
					left = fmt.Sprintf("%d days", *o.DaysLeft)
				}
				fmt.Fprintf(w, "Expires:\t%s (%s)\n", o.Expires, left)
				fmt.Fprintf(w, "Auto-renew:\t%s\n", optBool(o.AutoRenew))
				fmt.Fprintf(w, "Locked:\t%s\n", optBool(o.Locked))
				fmt.Fprintf(w, "Premium:\t%s\n", yesNo(o.Premium))
				priv := yesNo(o.Privacy)
				if o.Privacy && o.PrivacyUntil != "" {
					priv += " (until " + o.PrivacyUntil + ")"
				}
				fmt.Fprintf(w, "Privacy:\t%s\n", priv)
				dns := o.DNSProvider
				if o.NamecheapDNS {
					dns += " (Namecheap DNS)"
				}
				fmt.Fprintf(w, "DNS:\t%s\n", dns)
				if len(o.Nameservers) > 0 {
					fmt.Fprintf(w, "Nameservers:\t%s\n", strings.Join(o.Nameservers, ", "))
				}
			})
		},
	}
}

func optBool(b *bool) string {
	if b == nil {
		return "?"
	}
	return yesNo(*b)
}
