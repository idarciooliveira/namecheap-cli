package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/idarciooliveira/namecheap-cli/internal/api"
)

type priceSummary struct {
	TLD      string   `json:"tld"`
	Register *float64 `json:"register"`
	Renew    *float64 `json:"renew"`
	Currency string   `json:"currency"`
	MinYears int      `json:"minYears,omitempty"`
	IcannFee *float64 `json:"icannFee,omitempty"`
	Error    string   `json:"error,omitempty"`
}

type priceRow struct {
	TLD          string   `json:"tld"`
	Years        int      `json:"years"`
	Price        *float64 `json:"price"`
	RegularPrice *float64 `json:"regularPrice"`
	IcannFee     *float64 `json:"icannFee,omitempty"`
	Currency     string   `json:"currency"`
}

func (a *app) priceCmd() *cobra.Command {
	var action string
	var refresh bool
	cmd := &cobra.Command{
		Use:   "price <tld>...",
		Short: "Show your Namecheap price for TLDs (cached 24h)",
		Long: `Without --action, shows the per-year register and renew price for each TLD
(1 year, or the TLD's minimum term). With --action, shows every duration as
Namecheap returns it. Prices exclude the ICANN fee, which is listed separately.`,
		Example: `  ncli price com io dev
  ncli price io --action renew
  ncli price com --action transfer`,
		Args: minArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			tlds := splitTLDs(strings.Join(args, ","))
			if len(tlds) == 0 {
				return usagef("no TLDs given")
			}
			act := strings.ToUpper(action)
			switch act {
			case "", "REGISTER", "RENEW", "TRANSFER", "REACTIVATE":
			default:
				return usagef("--action must be register, renew, transfer or reactivate")
			}
			c, cfg, err := a.client(ctx)
			if err != nil {
				return err
			}
			pr := newPricer(c, cfg)
			defer pr.cache.Save()

			if act == "" {
				rows := make([]priceSummary, 0, len(tlds))
				failed := 0
				for _, t := range tlds {
					row := priceSummary{TLD: t}
					for _, ac := range []string{"REGISTER", "RENEW"} {
						ps, err := pr.prices(ctx, ac, t, refresh)
						if err != nil {
							if errors.Is(err, api.ErrNoPrice) {
								row.Error = "Namecheap has no price for this TLD"
								break
							}
							return fmt.Errorf(".%s %s: %w", t, strings.ToLower(ac), err)
						}
						if am, ok := termPrice(ps); ok {
							v := am.value
							if ac == "REGISTER" {
								row.Register = &v
								if am.years > 1 {
									row.MinYears = am.years
								}
								if am.fee > 0 {
									row.IcannFee = &am.fee
								}
							} else {
								row.Renew = &v
							}
							row.Currency = am.currency
						}
					}
					if row.Error != "" {
						failed++
					}
					rows = append(rows, row)
				}
				if failed == len(rows) {
					return fmt.Errorf("no prices found for %s", strings.Join(tlds, ", "))
				}
				return a.emit(map[string]any{"prices": rows}, func(w io.Writer) {
					fmt.Fprintln(w, "TLD\tREGISTER/YR\tRENEW/YR\t")
					fees := map[string]float64{}
					for _, r := range rows {
						note := r.Error
						if r.MinYears > 1 {
							note = fmt.Sprintf("%d-year minimum", r.MinYears)
						}
						if r.IcannFee != nil {
							fees[r.TLD] = *r.IcannFee
						}
						fmt.Fprintf(w, ".%s\t%s\t%s\t%s\n", r.TLD, money(r.Register, r.Currency), money(r.Renew, r.Currency), note)
					}
					feeFooter(w, fees)
				})
			}

			var rows []priceRow
			for _, t := range tlds {
				ps, err := pr.prices(ctx, act, t, refresh)
				if err != nil {
					return fmt.Errorf(".%s %s: %w", t, strings.ToLower(act), err)
				}
				for _, p := range ps {
					r := priceRow{TLD: t, Years: years(p), Currency: p.Currency}
					if v, ok := priceValue(p); ok {
						r.Price = &v
					}
					if v, ok := parseMoney(p.RegularPrice); ok {
						r.RegularPrice = &v
					}
					if v, ok := parseMoney(p.AdditionalCost); ok && v > 0 {
						r.IcannFee = &v
					}
					rows = append(rows, r)
				}
			}
			return a.emit(map[string]any{"action": strings.ToLower(act), "prices": rows}, func(w io.Writer) {
				fmt.Fprintln(w, "TLD\tYEARS\tPRICE\tREGULAR\t")
				fees := map[string]float64{}
				for _, r := range rows {
					if r.IcannFee != nil {
						fees[r.TLD] = *r.IcannFee
					}
					fmt.Fprintf(w, ".%s\t%d\t%s\t%s\t\n", r.TLD, r.Years, money(r.Price, r.Currency), money(r.RegularPrice, r.Currency))
				}
				fmt.Fprintln(w, "\nPrices as returned by Namecheap's pricing API.")
				feeFooter(w, fees)
			})
		},
	}
	cmd.Flags().StringVarP(&action, "action", "a", "", "register, renew, transfer or reactivate (shows all durations)")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "ignore the pricing cache")
	return cmd
}
