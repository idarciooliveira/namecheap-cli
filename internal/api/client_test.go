package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const okHead = `<?xml version="1.0" encoding="UTF-8"?>
<ApiResponse xmlns="http://api.namecheap.com/xml.response" Status="OK"><Errors /><RequestedCommand>x</RequestedCommand>`
const okTail = `<Server>S</Server><GMTTimeDifference>+5</GMTTimeDifference><ExecutionTime>0.1</ExecutionTime></ApiResponse>`

func ok(cmd, body string) string {
	return okHead + `<CommandResponse Type="` + cmd + `">` + body + `</CommandResponse>` + okTail
}

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New(srv.URL, "user", "SECRETKEY", "user", "1.2.3.4")
	c.HTTP = srv.Client()
	c.Backoff = time.Millisecond
	return c
}

func TestCallSendsGlobalParams(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		for k, want := range map[string]string{"ApiUser": "user", "ApiKey": "SECRETKEY", "UserName": "user", "ClientIp": "1.2.3.4", "Command": "namecheap.users.getBalances"} {
			if q.Get(k) != want {
				t.Errorf("%s = %q, want %q", k, q.Get(k), want)
			}
		}
		fmt.Fprint(w, ok("namecheap.users.getBalances", `<UserGetBalancesResult Currency="USD" AvailableBalance="4932.96" AccountBalance="4932.96" EarnedAmount="381.70" WithdrawableAmount="1243.36" FundsRequiredForAutoRenew="0.00" />`))
	})
	b, err := c.GetBalances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if b.AvailableBalance != "4932.96" || b.Currency != "USD" {
		t.Fatalf("got %+v", b)
	}
}

func TestListDomainsPaginates(t *testing.T) {
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		page := r.URL.Query().Get("Page")
		var doms string
		switch page {
		case "1":
			doms = `<Domain ID="127" Name="domain1.com" User="owner" Created="02/15/2016" Expires="02/15/2022" IsExpired="false" IsLocked="false" AutoRenew="false" WhoisGuard="ENABLED" IsPremium="true" IsOurDNS="true"/>
			<Domain ID="381" Name="domain2.com" User="owner" Created="04/28/2016" Expires="04/28/2023" IsExpired="false" IsLocked="false" AutoRenew="true" WhoisGuard="NOTPRESENT" IsPremium="false" IsOurDNS="true"/>`
		case "2":
			doms = `<Domain ID="385" Name="domain3.com" User="owner" Created="05/22/2016" Expires="05/22/2023" IsExpired="True" IsLocked="" AutoRenew="True" WhoisGuard="ENABLED" IsPremium="false" IsOurDNS="false"/>`
		}
		fmt.Fprint(w, ok("namecheap.domains.getList", `<DomainGetListResult>`+doms+`</DomainGetListResult><Paging><TotalItems>3</TotalItems><CurrentPage>`+page+`</CurrentPage><PageSize>2</PageSize></Paging>`))
	})
	ds, err := c.ListDomains(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 3 || calls != 2 {
		t.Fatalf("got %d domains in %d calls", len(ds), calls)
	}
	if ds[0].Name != "domain1.com" || ds[0].Expires != "02/15/2022" || ds[0].AutoRenew || !ds[0].IsPremium {
		t.Errorf("domain1 = %+v", ds[0])
	}
	if !ds[2].IsExpired || !ds[2].AutoRenew || ds[2].IsLocked {
		t.Errorf("domain3 bools = %+v", ds[2])
	}
}

func TestCheckAndBatching(t *testing.T) {
	var lists []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		dl := r.URL.Query().Get("DomainList")
		lists = append(lists, dl)
		var b strings.Builder
		for _, d := range strings.Split(dl, ",") {
			prem := `IsPremiumName="false" PremiumRegistrationPrice="0" PremiumRenewalPrice="0"`
			if d == "us.xyz" {
				prem = `IsPremiumName="true" PremiumRegistrationPrice="13000.0000" PremiumRenewalPrice="13000.0000"`
			}
			fmt.Fprintf(&b, `<DomainCheckResult Domain="%s" Available="true" ErrorNo="0" Description="" %s PremiumRestorePrice="0" PremiumTransferPrice="0" IcannFee="0" EapFee="0"/>`, d, prem)
		}
		fmt.Fprint(w, ok("namecheap.domains.check", b.String()))
	})
	var in []string
	for i := 0; i < 51; i++ {
		in = append(in, fmt.Sprintf("d%d.com", i))
	}
	in = append(in, "us.xyz")
	res, err := c.Check(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(lists) != 2 || len(res) != 52 {
		t.Fatalf("calls=%d results=%d", len(lists), len(res))
	}
	last := res[51]
	if !last.IsPremiumName || last.PremiumRegistrationPrice != "13000.0000" || !last.Available {
		t.Errorf("premium = %+v", last)
	}
}

func TestDomainPricing(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("ActionName") != "REGISTER" || q.Get("ProductName") != "BIZ" {
			t.Errorf("params %v", q)
		}
		fmt.Fprint(w, ok("namecheap.users.getPricing", `<UserGetPricingResult><ProductType Name="domains">
			<ProductCategory Name="register"><Product Name="biz">
			<Price Duration="1" DurationType="YEAR" Price="6.00" RegularPrice="6.00" YourPrice="6.00" CouponPrice="" Currency="USD" />
			<Price Duration="2" DurationType="YEAR" Price="8.87" RegularPrice="8.87" YourPrice="8.87" CouponPrice="" Currency="USD" />
			</Product></ProductCategory></ProductType></UserGetPricingResult>`))
	})
	ps, err := c.DomainPricing(context.Background(), "register", "biz")
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 || ps[0].Price != "6.00" || ps[1].Duration != "2" {
		t.Fatalf("got %+v", ps)
	}
}

func TestAPIErrorAndKeyNotLeaked(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0" encoding="utf-8"?><ApiResponse Status="ERROR" xmlns="http://api.namecheap.com/xml.response"><Errors><Error Number="1011150">Invalid request IP: 1.2.3.4</Error></Errors><Warnings /><RequestedCommand>namecheap.users.getbalances</RequestedCommand></ApiResponse>`)
	})
	_, err := c.GetBalances(context.Background())
	var ae *Error
	if !errors.As(err, &ae) {
		t.Fatalf("want *Error, got %T %v", err, err)
	}
	if ae.Code != "1011150" || !ae.IsAuth() || !ae.IsIP() {
		t.Errorf("got %+v", ae)
	}
	if strings.Contains(err.Error(), "SECRETKEY") {
		t.Error("API key leaked in error")
	}
}

func TestRetriesOn5xxAndScrubsTransportErrors(t *testing.T) {
	var n int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, ok("namecheap.users.getBalances", `<UserGetBalancesResult Currency="USD" AvailableBalance="1.00"/>`))
	})
	if _, err := c.GetBalances(context.Background()); err != nil {
		t.Fatalf("expected success after retries: %v", err)
	}

	dead := New("http://127.0.0.1:1/xml.response", "u", "SECRETKEY", "u", "1.2.3.4")
	dead.Backoff = time.Millisecond
	_, err := dead.GetBalances(context.Background())
	if err == nil || strings.Contains(err.Error(), "SECRETKEY") {
		t.Fatalf("want scrubbed error, got %v", err)
	}
}

func TestGetInfo(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, ok("namecheap.domains.getinfo", `<DomainGetInfoResult Status="Ok" ID="736542" DomainName="domain1.com" OwnerName="apisuer" IsOwner="true" IsPremium="true">
			<DomainDetails><CreatedDate>09/05/2016</CreatedDate><ExpiredDate>09/05/2027</ExpiredDate></DomainDetails>
			<LockDetails /><Whoisguard Enabled="True"><ID>3655801</ID><ExpiredDate>01/26/2028</ExpiredDate></Whoisguard>
			<DnsDetails ProviderType="FREE" IsUsingOurDNS="true"><Nameserver>dns1.registrar-servers.com</Nameserver><Nameserver>dns2.registrar-servers.com</Nameserver></DnsDetails>
			<Modificationrights All="true" /></DomainGetInfoResult>`))
	})
	info, err := c.GetInfo(context.Background(), "domain1.com")
	if err != nil {
		t.Fatal(err)
	}
	if info.Expires != "09/05/2027" || !info.Whoisguard.Enabled || len(info.DNS.Nameservers) != 2 || !info.DNS.IsUsingOurDNS {
		t.Fatalf("got %+v", info)
	}
}
