package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/idarciooliveira/namecheap-cli/internal/api"
	"github.com/idarciooliveira/namecheap-cli/internal/cache"
	"github.com/idarciooliveira/namecheap-cli/internal/config"
)

const defaultTLDs = "com,net,org,io,dev,app,co"

type checkOut struct {
	Domain        string   `json:"domain"`
	Available     bool     `json:"available"`
	Premium       bool     `json:"premium"`
	RegisterPrice *float64 `json:"registerPrice"`
	RenewPrice    *float64 `json:"renewPrice"`
	Currency      string   `json:"currency,omitempty"`
	MinYears      int      `json:"minYears,omitempty"`
	IcannFee      *float64 `json:"icannFee,omitempty"`
	Error         string   `json:"error,omitempty"`
}

func (a *app) checkCmd() *cobra.Command {
	var tlds string
	var noPrice bool
	cmd := &cobra.Command{
		Use:   "check <name|domain>...",
		Short: "Check if domains are available, with yearly prices",
		Long: `Check availability. A bare name is tried with each TLD in --tlds;
a full domain is checked as is. Prices are per year for the shortest term
(1 year, or the TLD minimum) and exclude the ICANN fee.`,
		Example: `  ncli check myidea
  ncli check myidea --tlds com,io,ai
  ncli check myidea.com other.dev`,
		Args: minArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			domains := expandNames(args, splitTLDs(tlds))
			if len(domains) == 0 {
				return usagef("no domains to check")
			}
			c, cfg, err := a.client(ctx)
			if err != nil {
				return err
			}
			res, err := c.Check(ctx, domains)
			if err != nil {
				return err
			}
			byName := map[string]api.CheckResult{}
			for _, r := range res {
				byName[strings.ToLower(r.Domain)] = r
			}

			rows := make([]checkOut, 0, len(domains))
			for _, d := range domains {
				r, ok := byName[d]
				if !ok {
					rows = append(rows, checkOut{Domain: d, Error: "no result returned"})
					continue
				}
				o := checkOut{Domain: d, Available: bool(r.Available), Premium: bool(r.IsPremiumName)}
				if r.ErrorNo != "" && r.ErrorNo != "0" {
					o.Error = strings.TrimSpace(r.Description)
					if o.Error == "" {
						o.Error = "error " + r.ErrorNo
					}
				}
				if o.Available && o.Premium {
					if v, ok := parseMoney(r.PremiumRegistrationPrice); ok && v > 0 {
						o.RegisterPrice = &v
					}
					if v, ok := parseMoney(r.PremiumRenewalPrice); ok && v > 0 {
						o.RenewPrice = &v
					}
					o.Currency = "USD"
				}
				rows = append(rows, o)
			}

			if !noPrice {
				pr := newPricer(c, cfg)
				var need []string
				for _, o := range rows {
					if o.Available && !o.Premium {
						need = append(need, tldOf(o.Domain))
					}
				}
				prices := pr.oneYearMany(ctx, need, []string{"REGISTER", "RENEW"})
				for i, o := range rows {
					if !o.Available || o.Premium {
						continue
					}
					t := tldOf(o.Domain)
					if p, ok := prices[t+"|REGISTER"]; ok {
						rows[i].RegisterPrice, rows[i].Currency = &p.value, p.currency
						if p.years > 1 {
							rows[i].MinYears = p.years
						}
						if p.fee > 0 {
							rows[i].IcannFee = &p.fee
						}
					}
					if p, ok := prices[t+"|RENEW"]; ok {
						rows[i].RenewPrice = &p.value
					}
				}
				_ = pr.cache.Save()
			}

			avail := 0
			for _, o := range rows {
				if o.Available {
					avail++
				}
			}
			return a.emit(map[string]any{"total": len(rows), "available": avail, "results": rows}, func(w io.Writer) {
				fmt.Fprintln(w, "DOMAIN\tAVAILABLE\tPREMIUM\tREGISTER/YR\tRENEW/YR\t")
				fees := map[string]float64{}
				for _, o := range rows {
					if o.IcannFee != nil {
						fees[tldOf(o.Domain)] = *o.IcannFee
					}
					note := ""
					if o.Error != "" {
						note = o.Error
					}
					avail := yesNo(o.Available)
					if o.Available {
						avail = "YES"
					}
					if o.MinYears > 1 {
						note = strings.TrimSpace(fmt.Sprintf("%d-year minimum %s", o.MinYears, note))
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", o.Domain, avail, yesNo(o.Premium),
						money(o.RegisterPrice, o.Currency), money(o.RenewPrice, o.Currency), note)
				}
				feeFooter(w, fees)
			})
		},
	}
	cmd.Flags().StringVarP(&tlds, "tlds", "t", defaultTLDs, "TLDs to try for bare names")
	cmd.Flags().BoolVar(&noPrice, "no-price", false, "skip price lookups")
	return cmd
}

// ---- pricing with a 24h cache ----

type pricer struct {
	c     *api.Client
	env   string
	cache *cache.Cache
}

func newPricer(c *api.Client, cfg *config.Config) *pricer {
	return &pricer{c: c, env: cfg.Environment(), cache: cache.Open("pricing.json", 24*time.Hour)}
}

func (p *pricer) prices(ctx context.Context, action, tld string, refresh bool) ([]api.Price, error) {
	key := p.env + "|" + strings.ToUpper(action) + "|" + strings.ToLower(tld)
	var ps []api.Price
	if !refresh && p.cache.Get(key, &ps) {
		return ps, nil
	}
	ps, err := p.c.DomainPricing(ctx, action, tld)
	if err != nil {
		return nil, err
	}
	p.cache.Set(key, ps)
	return ps, nil
}

type amount struct {
	value    float64
	currency string
	years    int     // term the price is for: 1, or the TLD's minimum term
	fee      float64 // ICANN fee per year (AdditionalCost), not included in value
}

// priceValue picks the final price, falling back to YourPrice/RegularPrice.
func priceValue(pr api.Price) (float64, bool) {
	for _, s := range []string{pr.Price, pr.YourPrice, pr.RegularPrice} {
		if v, ok := parseMoney(s); ok && v > 0 {
			return v, true
		}
	}
	return 0, false
}

// termPrice returns the per-year price for the shortest term Namecheap sells:
// 1 year, or the TLD's minimum (e.g. .ai is 2 years).
func termPrice(ps []api.Price) (amount, bool) {
	best, found := amount{}, false
	for _, pr := range ps {
		if !strings.EqualFold(pr.DurationType, "YEAR") {
			continue
		}
		y := years(pr)
		v, ok := priceValue(pr)
		if !ok || y <= 0 || (found && y >= best.years) {
			continue
		}
		fee, _ := parseMoney(pr.AdditionalCost)
		best, found = amount{value: v, currency: pr.Currency, years: y, fee: fee}, true
	}
	return best, found
}

// feeFooter prints the ICANN fees that the shown prices exclude.
func feeFooter(w io.Writer, fees map[string]float64) {
	if len(fees) == 0 {
		return
	}
	var parts []string
	for t, f := range fees {
		parts = append(parts, fmt.Sprintf(".%s +$%.2f", t, f))
	}
	sort.Strings(parts)
	fmt.Fprintf(w, "\nPrices exclude the ICANN fee per year: %s\n", strings.Join(parts, ", "))
}

// oneYearMany fetches 1-year prices for each tld x action, 4 at a time.
// Failures are reported on stderr and left out of the result.
func (p *pricer) oneYearMany(ctx context.Context, tlds, actions []string) map[string]amount {
	out := map[string]amount{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	seen := map[string]bool{}
	for _, t := range tlds {
		for _, act := range actions {
			k := t + "|" + act
			if seen[k] {
				continue
			}
			seen[k] = true
			wg.Add(1)
			go func(t, act, k string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				ps, err := p.prices(ctx, act, t, false)
				if err != nil {
					fmt.Fprintf(os.Stderr, "warning: no %s price for .%s: %v\n", strings.ToLower(act), t, err)
					return
				}
				if a, ok := termPrice(ps); ok {
					mu.Lock()
					out[k] = a
					mu.Unlock()
				}
			}(t, act, k)
		}
	}
	wg.Wait()
	return out
}

func years(pr api.Price) int {
	n, _ := strconv.Atoi(strings.TrimSpace(pr.Duration))
	return n
}
