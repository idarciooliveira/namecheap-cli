// Package mail checks a domain's public mail DNS (MX, SPF, DKIM, DMARC) and
// the TLS endpoints of a mail server. It only reads public data.
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"sort"
	"strings"
	"time"
)

// Namecheap Private Email endpoints.
const (
	PrivateEmailHost = "mail.privateemail.com"
	PrivateEmailSPF  = "include:spf.privateemail.com"
)

// Check statuses.
const (
	OK      = "ok"
	Missing = "missing"
	Wrong   = "wrong"
	Unknown = "unknown" // can't tell (e.g. DKIM selector of a non-Private-Email provider)
	Error   = "error"   // lookup failed
)

type Check struct {
	Status  string   `json:"status"`
	Records []string `json:"records"`
	Detail  string   `json:"detail,omitempty"`
}

type DNSReport struct {
	MX    Check `json:"mx"`
	SPF   Check `json:"spf"`
	DKIM  Check `json:"dkim"`
	DMARC Check `json:"dmarc"`
}

type Resolver interface {
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

// Provider is what the MX records point at.
type Provider string

const (
	ProviderNone         Provider = "none"
	ProviderPrivateEmail Provider = "private-email"
	ProviderForwarding   Provider = "forwarding"
	ProviderGoogle       Provider = "google-workspace"
	ProviderMicrosoft    Provider = "microsoft-365"
	ProviderOther        Provider = "custom-mx"
)

// ProviderFromMX classifies MX hosts.
func ProviderFromMX(hosts []string) Provider {
	if len(hosts) == 0 {
		return ProviderNone
	}
	var pe, fwd, g, ms bool
	for _, h := range hosts {
		switch {
		case strings.HasSuffix(h, "privateemail.com"):
			pe = true
		case strings.HasSuffix(h, "registrar-servers.com"):
			fwd = true
		case strings.HasSuffix(h, "google.com"), strings.HasSuffix(h, "googlemail.com"):
			g = true
		case strings.HasSuffix(h, "outlook.com"):
			ms = true
		}
	}
	switch {
	case pe:
		return ProviderPrivateEmail
	case g:
		return ProviderGoogle
	case ms:
		return ProviderMicrosoft
	case fwd:
		return ProviderForwarding
	}
	return ProviderOther
}

func notFound(err error) bool {
	var de *net.DNSError
	return errors.As(err, &de) && de.IsNotFound
}

// LookupMX returns MX hosts sorted by preference, lowercased, without trailing dot.
func LookupMX(ctx context.Context, r Resolver, domain string) (Check, []string) {
	mx, err := r.LookupMX(ctx, domain)
	if err != nil && !notFound(err) {
		return Check{Status: Error, Records: []string{}, Detail: err.Error()}, nil
	}
	sort.SliceStable(mx, func(i, j int) bool {
		if mx[i].Pref != mx[j].Pref {
			return mx[i].Pref < mx[j].Pref
		}
		return mx[i].Host < mx[j].Host
	})
	hosts := []string{}
	for _, m := range mx {
		h := strings.TrimSuffix(strings.ToLower(m.Host), ".")
		if h != "" {
			hosts = append(hosts, h)
		}
	}
	if len(hosts) == 0 {
		return Check{Status: Missing, Records: hosts, Detail: "no MX records"}, hosts
	}
	return Check{Status: OK, Records: hosts}, hosts
}

func txt(ctx context.Context, r Resolver, name, prefix string) ([]string, error) {
	recs, err := r.LookupTXT(ctx, name)
	if err != nil {
		if notFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, t := range recs {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(t)), strings.ToLower(prefix)) {
			out = append(out, t)
		}
	}
	return out, nil
}

// CheckDNS looks up the mail records of domain. With privateEmail set, the
// records are also validated against Namecheap Private Email's settings.
func CheckDNS(ctx context.Context, r Resolver, domain string, privateEmail bool, dkimSelector string) (DNSReport, []string) {
	var rep DNSReport
	var hosts []string
	rep.MX, hosts = LookupMX(ctx, r, domain)
	if privateEmail && rep.MX.Status == OK {
		var pe, other []string
		for _, h := range hosts {
			if strings.HasSuffix(h, "privateemail.com") {
				pe = append(pe, h)
			} else {
				other = append(other, h)
			}
		}
		switch {
		case len(pe) == 0:
			rep.MX.Status, rep.MX.Detail = Wrong, "MX does not point to Private Email (mx1/mx2.privateemail.com)"
		case len(other) > 0:
			rep.MX.Status, rep.MX.Detail = Wrong, "MX also points to "+strings.Join(other, ", ")+"; mail may be delivered there"
		case len(pe) < 2:
			rep.MX.Detail = "only one Private Email MX; Namecheap recommends mx1 and mx2"
		}
	}

	// SPF
	spf, err := txt(ctx, r, domain, "v=spf1")
	switch {
	case err != nil:
		rep.SPF = Check{Status: Error, Detail: err.Error()}
	case len(spf) == 0:
		rep.SPF = Check{Status: Missing, Detail: "no SPF record"}
	case len(spf) > 1:
		rep.SPF = Check{Status: Wrong, Detail: "multiple SPF records; receivers treat this as an error"}
	case privateEmail && !strings.Contains(strings.ToLower(spf[0]), PrivateEmailSPF):
		rep.SPF = Check{Status: Wrong, Detail: "SPF does not include " + PrivateEmailSPF}
	default:
		rep.SPF = Check{Status: OK}
	}
	rep.SPF.Records = nonNil(spf)

	// DKIM: only checkable when we know the selector (Private Email uses "default").
	name := dkimSelector + "._domainkey." + domain
	dk, err := txt(ctx, r, name, "v=DKIM1")
	switch {
	case err != nil:
		rep.DKIM = Check{Status: Error, Detail: err.Error()}
	case len(dk) > 0:
		rep.DKIM = Check{Status: OK, Detail: "selector " + dkimSelector}
	case privateEmail:
		rep.DKIM = Check{Status: Missing, Detail: "no DKIM record at " + name}
	default:
		rep.DKIM = Check{Status: Unknown, Detail: "no record at selector " + dkimSelector + "; provider may use another selector"}
	}
	rep.DKIM.Records = []string{}
	for _, d := range dk {
		rep.DKIM.Records = append(rep.DKIM.Records, shorten(d, 60))
	}

	// DMARC
	dm, err := txt(ctx, r, "_dmarc."+domain, "v=DMARC1")
	switch {
	case err != nil:
		rep.DMARC = Check{Status: Error, Detail: err.Error()}
	case len(dm) == 0:
		rep.DMARC = Check{Status: Missing, Detail: "no DMARC record at _dmarc." + domain}
	default:
		rep.DMARC = Check{Status: OK, Detail: "policy " + dmarcPolicy(dm[0])}
	}
	rep.DMARC.Records = nonNil(dm)

	return rep, hosts
}

func dmarcPolicy(rec string) string {
	for _, part := range strings.Split(rec, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 && strings.EqualFold(kv[0], "p") {
			return strings.ToLower(strings.TrimSpace(kv[1]))
		}
	}
	return "unset"
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ---- TLS endpoints ----

type Endpoint struct {
	Service   string `json:"service"` // imap, smtp
	Port      int    `json:"port"`
	Mode      string `json:"mode"` // tls or starttls
	OK        bool   `json:"ok"`
	TLS       string `json:"tlsVersion,omitempty"`
	Subject   string `json:"certSubject,omitempty"`
	Issuer    string `json:"certIssuer,omitempty"`
	NotAfter  string `json:"certExpires,omitempty"`
	DaysLeft  *int   `json:"certDaysLeft,omitempty"`
	LatencyMs int64  `json:"latencyMs,omitempty"`
	Error     string `json:"error,omitempty"`
}

// DefaultEndpoints are the client ports Private Email documents.
var DefaultEndpoints = []Endpoint{
	{Service: "imap", Port: 993, Mode: "tls"},
	{Service: "smtp", Port: 465, Mode: "tls"},
	{Service: "smtp", Port: 587, Mode: "starttls"},
}

// CheckTLS connects to each endpoint of host and verifies its certificate.
func CheckTLS(ctx context.Context, host string, eps []Endpoint, timeout time.Duration) []Endpoint {
	out := make([]Endpoint, len(eps))
	done := make(chan struct{}, len(eps))
	for i, ep := range eps {
		go func(i int, ep Endpoint) {
			out[i] = probe(ctx, host, ep, timeout)
			done <- struct{}{}
		}(i, ep)
	}
	for range eps {
		<-done
	}
	return out
}

func probe(ctx context.Context, host string, ep Endpoint, timeout time.Duration) Endpoint {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	addr := net.JoinHostPort(host, fmt.Sprint(ep.Port))
	cfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	start := time.Now()

	var state tls.ConnectionState
	switch ep.Mode {
	case "tls":
		d := &tls.Dialer{NetDialer: &net.Dialer{}, Config: cfg}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			ep.Error = cleanErr(err)
			return ep
		}
		state = conn.(*tls.Conn).ConnectionState()
		conn.Close()
	case "starttls":
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
		if err != nil {
			ep.Error = cleanErr(err)
			return ep
		}
		if dl, ok := ctx.Deadline(); ok {
			_ = conn.SetDeadline(dl)
		}
		c, err := smtp.NewClient(conn, host)
		if err != nil {
			conn.Close()
			ep.Error = cleanErr(err)
			return ep
		}
		if err := c.Hello("ncli.localhost"); err != nil {
			c.Close()
			ep.Error = cleanErr(err)
			return ep
		}
		if err := c.StartTLS(cfg); err != nil {
			c.Close()
			ep.Error = cleanErr(err)
			return ep
		}
		state, _ = c.TLSConnectionState()
		_ = c.Quit()
	}
	ep.LatencyMs = time.Since(start).Milliseconds()
	ep.OK = true
	ep.TLS = tls.VersionName(state.Version)
	if len(state.PeerCertificates) > 0 {
		cert := state.PeerCertificates[0]
		ep.Subject = cert.Subject.CommonName
		ep.Issuer = cert.Issuer.CommonName
		if len(cert.Issuer.Organization) > 0 {
			ep.Issuer = cert.Issuer.Organization[0] + " " + cert.Issuer.CommonName
		}
		ep.NotAfter = cert.NotAfter.Format("2006-01-02")
		n := int(time.Until(cert.NotAfter).Hours() / 24)
		ep.DaysLeft = &n
	}
	return ep
}

func cleanErr(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out (port may be blocked on this network)"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timed out (port may be blocked on this network)"
	}
	return err.Error()
}
