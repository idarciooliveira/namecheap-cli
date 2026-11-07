package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/idarciooliveira/namecheap-cli/internal/api"
	"github.com/idarciooliveira/namecheap-cli/internal/mail"
)

type mailOut struct {
	Domain       string         `json:"domain"`
	Service      mail.Provider  `json:"service"`
	PrivateEmail bool           `json:"privateEmail"`
	EmailType    string         `json:"namecheapEmailType,omitempty"`
	DNS          string         `json:"dns"` // namecheap or external
	Records      mail.DNSReport `json:"records"`
	Issues       []string       `json:"issues"`
	OK           bool           `json:"ok"`
	Note         string         `json:"note,omitempty"`
}

var serviceLabels = map[mail.Provider]string{
	mail.ProviderPrivateEmail: "Private Email",
	mail.ProviderForwarding:   "Forwarding",
	mail.ProviderGoogle:       "Google Workspace",
	mail.ProviderMicrosoft:    "Microsoft 365",
	mail.ProviderOther:        "Custom MX",
	mail.ProviderNone:         "None",
}

// sendsMail reports whether the service sends mail as the domain, so SPF,
// DKIM and DMARC matter.
func sendsMail(p mail.Provider) bool {
	switch p {
	case mail.ProviderPrivateEmail, mail.ProviderGoogle, mail.ProviderMicrosoft, mail.ProviderOther:
		return true
	}
	return false
}

// inspectMail combines Namecheap's mail setting with public DNS.
func inspectMail(ctx context.Context, c *api.Client, domain, selector string) (mailOut, error) {
	o := mailOut{Domain: domain, DNS: "namecheap", Issues: []string{}}
	r := net.DefaultResolver

	hosts, err := c.GetHosts(ctx, domain)
	if err != nil {
		var ae *api.Error
		if errors.As(err, &ae) && ae.IsAuth() {
			return o, err
		}
		o.DNS = "external"
		if errors.As(err, &ae) && ae.Code != api.ErrCodeNotOurDNS {
			o.Note = "Namecheap: " + ae.Message + "; using public DNS only"
		}
		hosts = nil
	} else {
		o.EmailType = strings.ToUpper(hosts.EmailType)
	}

	_, mx := mail.LookupMX(ctx, r, domain)
	o.Service = mail.ProviderFromMX(mx)
	if hosts != nil {
		switch o.EmailType {
		case "OX":
			o.Service = mail.ProviderPrivateEmail
		case "FWD":
			o.Service = mail.ProviderForwarding
		case "GMAIL":
			o.Service = mail.ProviderGoogle
		}
	}
	o.PrivateEmail = o.Service == mail.ProviderPrivateEmail

	o.Records, _ = mail.CheckDNS(ctx, r, domain, o.PrivateEmail, selector)
	if o.PrivateEmail && o.EmailType != "" && o.EmailType != "OX" {
		o.Note = strings.TrimSpace(o.Note + " Private Email via custom MX records (Namecheap mail setting: " + o.EmailType + ")")
	}

	if sendsMail(o.Service) {
		add := func(name string, ch mail.Check) {
			if ch.Status == mail.Missing || ch.Status == mail.Wrong || ch.Status == mail.Error {
				o.Issues = append(o.Issues, name+": "+ch.Detail)
			}
		}
		add("MX", o.Records.MX)
		add("SPF", o.Records.SPF)
		add("DKIM", o.Records.DKIM)
		add("DMARC", o.Records.DMARC)
	} else if o.Records.MX.Status == mail.Error {
		o.Issues = append(o.Issues, "MX: "+o.Records.MX.Detail)
	}
	o.OK = len(o.Issues) == 0
	return o, nil
}

func cell(ch mail.Check, relevant bool) string {
	if !relevant {
		return "-"
	}
	switch ch.Status {
	case mail.Wrong:
		return "WRONG"
	case mail.Missing:
		return "missing"
	case mail.Unknown:
		return "?"
	}
	return ch.Status
}

func (a *app) mailCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "mail", Short: "Email setup per domain: Private Email, MX, SPF, DKIM, DMARC"}
	cmd.AddCommand(a.mailStatusCmd(), a.mailCheckCmd())
	return cmd
}

func (a *app) mailStatusCmd() *cobra.Command {
	var privateOnly bool
	var selector string
	cmd := &cobra.Command{
		Use:   "status [domain...]",
		Short: "Which domains use Private Email, and whether their mail DNS is correct",
		Long: `Shows the email service of each domain (all domains in the account by
default) and checks MX, SPF, DKIM and DMARC in public DNS.

Namecheap's API has no Private Email methods, so subscription expiry, plan and
mailboxes are not available here.`,
		Example: `  ncli mail status
  ncli mail status --private-only
  ncli mail status example.com`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, _, err := a.client(ctx)
			if err != nil {
				return err
			}
			var domains []string
			for _, d := range args {
				domains = append(domains, normalizeDomain(d))
			}
			if len(domains) == 0 {
				ds, err := c.ListDomains(ctx, api.ListOptions{SortBy: "NAME"})
				if err != nil {
					return err
				}
				for _, d := range ds {
					domains = append(domains, strings.ToLower(d.Name))
				}
			}

			rows := make([]mailOut, len(domains))
			errs := make([]error, len(domains))
			var wg sync.WaitGroup
			sem := make(chan struct{}, 4)
			for i, d := range domains {
				wg.Add(1)
				go func(i int, d string) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					rows[i], errs[i] = inspectMail(ctx, c, d, selector)
				}(i, d)
			}
			wg.Wait()
			for _, err := range errs {
				if err != nil {
					return err
				}
			}

			out := make([]mailOut, 0, len(rows))
			pe := 0
			for _, r := range rows {
				if r.PrivateEmail {
					pe++
				}
				if privateOnly && !r.PrivateEmail {
					continue
				}
				out = append(out, r)
			}
			return a.emit(map[string]any{"total": len(out), "privateEmail": pe, "domains": out}, func(w io.Writer) {
				if len(out) == 0 {
					fmt.Fprintln(w, "No domains use Private Email.")
					return
				}
				fmt.Fprintln(w, "DOMAIN\tSERVICE\tMX\tSPF\tDKIM\tDMARC\tDNS\t")
				for _, r := range out {
					rel := sendsMail(r.Service)
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t\n", r.Domain, serviceLabels[r.Service],
						cell(r.Records.MX, r.Service != mail.ProviderNone), cell(r.Records.SPF, rel),
						cell(r.Records.DKIM, rel), cell(r.Records.DMARC, rel), r.DNS)
				}
				fmt.Fprintf(w, "\n%d of %d domain(s) use Private Email\n", pe, len(rows))
				var lines []string
				for _, r := range out {
					for _, is := range r.Issues {
						lines = append(lines, "  "+r.Domain+"  "+is)
					}
				}
				if len(lines) > 0 {
					fmt.Fprintln(w, "\nIssues:")
					for _, l := range lines {
						fmt.Fprintln(w, l)
					}
				}
			})
		},
	}
	cmd.Flags().BoolVarP(&privateOnly, "private-only", "p", false, "only show domains using Private Email")
	cmd.Flags().StringVar(&selector, "dkim-selector", "default", "DKIM selector to check")
	return cmd
}

type mailCheckOut struct {
	mailOut
	Server    string          `json:"server,omitempty"`
	Endpoints []mail.Endpoint `json:"endpoints"`
}

func (a *app) mailCheckCmd() *cobra.Command {
	var host, selector string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "check <domain>",
		Short: "Full mail check for one domain: DNS records plus IMAP/SMTP TLS",
		Long: `Runs the mail status checks for one domain, prints every record, and, for
Private Email domains (or with --host), connects to the mail server on
IMAP 993, SMTP 465 and SMTP 587 (STARTTLS) and reports the TLS certificate.

The mail.privateemail.com certificate belongs to Namecheap and is renewed by
them; its expiry is shown for information.`,
		Example: `  ncli mail check example.com
  ncli mail check example.com --host mail.example.com`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			domain := normalizeDomain(args[0])
			c, _, err := a.client(ctx)
			if err != nil {
				return err
			}
			m, err := inspectMail(ctx, c, domain, selector)
			if err != nil {
				return err
			}
			o := mailCheckOut{mailOut: m, Endpoints: []mail.Endpoint{}}
			server := host
			if server == "" && m.PrivateEmail {
				server = mail.PrivateEmailHost
			}
			if server != "" {
				o.Server = server
				o.Endpoints = mail.CheckTLS(ctx, server, mail.DefaultEndpoints, timeout)
				for _, ep := range o.Endpoints {
					if !ep.OK {
						o.Issues = append(o.Issues, fmt.Sprintf("%s %d: %s", strings.ToUpper(ep.Service), ep.Port, ep.Error))
					} else if ep.DaysLeft != nil && *ep.DaysLeft < 14 {
						o.Issues = append(o.Issues, fmt.Sprintf("%s %d: certificate expires in %d days", strings.ToUpper(ep.Service), ep.Port, *ep.DaysLeft))
					}
				}
				o.OK = len(o.Issues) == 0
			}
			return a.emit(o, func(w io.Writer) {
				svc := serviceLabels[o.Service]
				if o.EmailType != "" {
					svc += " (Namecheap mail setting: " + o.EmailType + ")"
				}
				fmt.Fprintf(w, "Domain:\t%s\n", o.Domain)
				fmt.Fprintf(w, "Service:\t%s\n", svc)
				fmt.Fprintf(w, "DNS:\t%s\n", o.DNS)
				if o.Note != "" {
					fmt.Fprintf(w, "Note:\t%s\n", o.Note)
				}
				fmt.Fprintln(w)
				rec := func(name string, ch mail.Check) {
					val := strings.Join(ch.Records, ", ")
					if ch.Detail != "" && (ch.Status != mail.OK || val == "") {
						val = strings.TrimSpace(val + "  (" + ch.Detail + ")")
					} else if ch.Detail != "" {
						val += "  (" + ch.Detail + ")"
					}
					fmt.Fprintf(w, "%s\t%s\t%s\n", name, strings.ToUpper(ch.Status), val)
				}
				rec("MX", o.Records.MX)
				rec("SPF", o.Records.SPF)
				rec("DKIM", o.Records.DKIM)
				rec("DMARC", o.Records.DMARC)
				if o.Server != "" {
					fmt.Fprintf(w, "\nServer %s:\n", o.Server)
					for _, ep := range o.Endpoints {
						label := fmt.Sprintf("  %s %d (%s)", strings.ToUpper(ep.Service), ep.Port, ep.Mode)
						if !ep.OK {
							fmt.Fprintf(w, "%s\tFAIL\t%s\n", label, ep.Error)
							continue
						}
						exp := ""
						if ep.DaysLeft != nil {
							exp = fmt.Sprintf("cert %s, expires %s (%d days)", ep.Subject, ep.NotAfter, *ep.DaysLeft)
						}
						fmt.Fprintf(w, "%s\tOK\t%s, %s, %dms\n", label, ep.TLS, exp, ep.LatencyMs)
					}
				} else if !o.PrivateEmail {
					fmt.Fprintln(w, "\nNot a Private Email domain; skipped server checks (use --host to check a mail server).")
				}
				fmt.Fprintln(w)
				if len(o.Issues) == 0 {
					fmt.Fprintln(w, "No issues found.")
				} else {
					fmt.Fprintln(w, "Issues:")
					for _, is := range o.Issues {
						fmt.Fprintln(w, "  - "+is)
					}
				}
			})
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "mail server to test (default: mail.privateemail.com for Private Email)")
	cmd.Flags().StringVar(&selector, "dkim-selector", "default", "DKIM selector to check")
	cmd.Flags().DurationVar(&timeout, "timeout", 8*time.Second, "timeout per server connection")
	return cmd
}
