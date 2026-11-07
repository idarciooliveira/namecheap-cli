package api

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// ---- namecheap.domains.getList ----

type Domain struct {
	ID         string `xml:"ID,attr"`
	Name       string `xml:"Name,attr"`
	Created    string `xml:"Created,attr"`
	Expires    string `xml:"Expires,attr"`
	IsExpired  Bool   `xml:"IsExpired,attr"`
	IsLocked   Bool   `xml:"IsLocked,attr"`
	AutoRenew  Bool   `xml:"AutoRenew,attr"`
	WhoisGuard string `xml:"WhoisGuard,attr"`
	IsPremium  Bool   `xml:"IsPremium,attr"`
	IsOurDNS   Bool   `xml:"IsOurDNS,attr"`
}

type ListOptions struct {
	ListType string // ALL, EXPIRING, EXPIRED
	Search   string
	SortBy   string // NAME, NAME_DESC, EXPIREDATE, EXPIREDATE_DESC, CREATEDATE, CREATEDATE_DESC
}

type listResult struct {
	Domains []Domain `xml:"DomainGetListResult>Domain"`
	Paging  struct {
		TotalItems  int `xml:"TotalItems"`
		CurrentPage int `xml:"CurrentPage"`
		PageSize    int `xml:"PageSize"`
	} `xml:"Paging"`
}

// ListDomains returns every domain in the account, following all pages.
func (c *Client) ListDomains(ctx context.Context, opts ListOptions) ([]Domain, error) {
	var all []Domain
	for page := 1; page <= 1000; page++ {
		p := url.Values{}
		p.Set("Page", strconv.Itoa(page))
		p.Set("PageSize", "100")
		if opts.ListType != "" {
			p.Set("ListType", opts.ListType)
		}
		if opts.Search != "" {
			p.Set("SearchTerm", opts.Search)
		}
		if opts.SortBy != "" {
			p.Set("SortBy", opts.SortBy)
		}
		var r listResult
		if err := c.Call(ctx, "namecheap.domains.getList", p, &r); err != nil {
			return nil, err
		}
		all = append(all, r.Domains...)
		if len(r.Domains) == 0 || len(all) >= r.Paging.TotalItems {
			break
		}
	}
	return all, nil
}

// ---- namecheap.domains.getInfo ----

type DomainInfo struct {
	Status     string `xml:"Status,attr"`
	ID         string `xml:"ID,attr"`
	DomainName string `xml:"DomainName,attr"`
	OwnerName  string `xml:"OwnerName,attr"`
	IsOwner    Bool   `xml:"IsOwner,attr"`
	IsPremium  Bool   `xml:"IsPremium,attr"`
	Created    string `xml:"DomainDetails>CreatedDate"`
	Expires    string `xml:"DomainDetails>ExpiredDate"`
	Whoisguard struct {
		Enabled Bool   `xml:"Enabled,attr"`
		Expires string `xml:"ExpiredDate"`
	} `xml:"Whoisguard"`
	DNS struct {
		ProviderType  string   `xml:"ProviderType,attr"`
		IsUsingOurDNS Bool     `xml:"IsUsingOurDNS,attr"`
		Nameservers   []string `xml:"Nameserver"`
	} `xml:"DnsDetails"`
}

func (c *Client) GetInfo(ctx context.Context, domain string) (*DomainInfo, error) {
	p := url.Values{}
	p.Set("DomainName", domain)
	var r struct {
		Info DomainInfo `xml:"DomainGetInfoResult"`
	}
	if err := c.Call(ctx, "namecheap.domains.getInfo", p, &r); err != nil {
		return nil, err
	}
	return &r.Info, nil
}

// ---- namecheap.domains.check ----

type CheckResult struct {
	Domain                   string `xml:"Domain,attr"`
	Available                Bool   `xml:"Available,attr"`
	ErrorNo                  string `xml:"ErrorNo,attr"`
	Description              string `xml:"Description,attr"`
	IsPremiumName            Bool   `xml:"IsPremiumName,attr"`
	PremiumRegistrationPrice string `xml:"PremiumRegistrationPrice,attr"`
	PremiumRenewalPrice      string `xml:"PremiumRenewalPrice,attr"`
	IcannFee                 string `xml:"IcannFee,attr"`
	EapFee                   string `xml:"EapFee,attr"`
}

// CheckMax is the API's limit of domains per check call.
const CheckMax = 50

// Check returns availability for each domain, batching 50 per call.
func (c *Client) Check(ctx context.Context, domains []string) ([]CheckResult, error) {
	var out []CheckResult
	for start := 0; start < len(domains); start += CheckMax {
		end := min(start+CheckMax, len(domains))
		p := url.Values{}
		p.Set("DomainList", strings.Join(domains[start:end], ","))
		var r struct {
			Results []CheckResult `xml:"DomainCheckResult"`
		}
		if err := c.Call(ctx, "namecheap.domains.check", p, &r); err != nil {
			return nil, err
		}
		out = append(out, r.Results...)
	}
	return out, nil
}

// ---- namecheap.users.getPricing ----

type Price struct {
	Duration       string `xml:"Duration,attr" json:"duration"`
	DurationType   string `xml:"DurationType,attr" json:"durationType"`
	Price          string `xml:"Price,attr" json:"price"`
	RegularPrice   string `xml:"RegularPrice,attr" json:"regularPrice"`
	YourPrice      string `xml:"YourPrice,attr" json:"yourPrice"`
	AdditionalCost string `xml:"AdditionalCost,attr" json:"additionalCost,omitempty"`
	Currency       string `xml:"Currency,attr" json:"currency"`
}

type pricingResult struct {
	Types []struct {
		Name       string `xml:"Name,attr"`
		Categories []struct {
			Name     string `xml:"Name,attr"`
			Products []struct {
				Name   string  `xml:"Name,attr"`
				Prices []Price `xml:"Price"`
			} `xml:"Product"`
		} `xml:"ProductCategory"`
	} `xml:"UserGetPricingResult>ProductType"`
}

// ErrNoPrice means Namecheap returned no price for that TLD/action.
var ErrNoPrice = errors.New("no price returned")

// DomainPricing returns the prices of one TLD for one action
// (REGISTER, RENEW, TRANSFER, REACTIVATE).
func (c *Client) DomainPricing(ctx context.Context, action, tld string) ([]Price, error) {
	p := url.Values{}
	p.Set("ProductType", "DOMAIN")
	p.Set("ProductCategory", "DOMAINS")
	p.Set("ActionName", strings.ToUpper(action))
	p.Set("ProductName", strings.ToUpper(tld))
	var r pricingResult
	if err := c.Call(ctx, "namecheap.users.getPricing", p, &r); err != nil {
		return nil, err
	}
	for _, t := range r.Types {
		for _, cat := range t.Categories {
			if !strings.EqualFold(cat.Name, action) {
				continue
			}
			for _, prod := range cat.Products {
				if strings.EqualFold(prod.Name, tld) && len(prod.Prices) > 0 {
					return prod.Prices, nil
				}
			}
		}
	}
	return nil, ErrNoPrice
}

// ---- namecheap.domains.dns.getHosts ----

type Host struct {
	Name     string `xml:"Name,attr"`
	Type     string `xml:"Type,attr"`
	Address  string `xml:"Address,attr"`
	MXPref   string `xml:"MXPref,attr"`
	IsActive Bool   `xml:"IsActive,attr"`
}

// HostsResult.EmailType is Namecheap's mail setting for the domain:
// NONE, FWD (forwarding), MX / MXE (custom MX), OX (Private Email), GMAIL.
type HostsResult struct {
	Domain        string `xml:"Domain,attr"`
	EmailType     string `xml:"EmailType,attr"`
	IsUsingOurDNS Bool   `xml:"IsUsingOurDNS,attr"`
	Hosts         []Host `xml:"host"`
}

// ErrCodeNotOurDNS is returned by DNS methods when the domain uses
// nameservers other than Namecheap's.
const ErrCodeNotOurDNS = "2030288"

func (c *Client) GetHosts(ctx context.Context, domain string) (*HostsResult, error) {
	i := strings.IndexByte(domain, '.')
	if i <= 0 {
		return nil, errors.New("invalid domain " + domain)
	}
	p := url.Values{}
	p.Set("SLD", domain[:i])
	p.Set("TLD", domain[i+1:])
	var r struct {
		R HostsResult `xml:"DomainDNSGetHostsResult"`
	}
	if err := c.Call(ctx, "namecheap.domains.dns.getHosts", p, &r); err != nil {
		return nil, err
	}
	return &r.R, nil
}

// ---- namecheap.users.getBalances ----

type Balances struct {
	Currency                  string `xml:"Currency,attr"`
	AvailableBalance          string `xml:"AvailableBalance,attr"`
	AccountBalance            string `xml:"AccountBalance,attr"`
	EarnedAmount              string `xml:"EarnedAmount,attr"`
	WithdrawableAmount        string `xml:"WithdrawableAmount,attr"`
	FundsRequiredForAutoRenew string `xml:"FundsRequiredForAutoRenew,attr"`
}

func (c *Client) GetBalances(ctx context.Context) (*Balances, error) {
	var r struct {
		B Balances `xml:"UserGetBalancesResult"`
	}
	if err := c.Call(ctx, "namecheap.users.getBalances", nil, &r); err != nil {
		return nil, err
	}
	return &r.B, nil
}
