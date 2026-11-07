package mail

import (
	"context"
	"net"
	"testing"
)

type fakeResolver struct {
	mx  map[string][]*net.MX
	txt map[string][]string
}

func (f fakeResolver) LookupMX(_ context.Context, name string) ([]*net.MX, error) {
	if v, ok := f.mx[name]; ok {
		return v, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func (f fakeResolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	if v, ok := f.txt[name]; ok {
		return v, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

var peMX = []*net.MX{{Host: "mx2.privateemail.com.", Pref: 10}, {Host: "mx1.privateemail.com.", Pref: 10}}

func TestPrivateEmailAllGood(t *testing.T) {
	r := fakeResolver{
		mx: map[string][]*net.MX{"a.com": peMX},
		txt: map[string][]string{
			"a.com":                    {"google-site-verification=x", "v=spf1 include:spf.privateemail.com ~all"},
			"default._domainkey.a.com": {"v=DKIM1;k=rsa;p=AAAA"},
			"_dmarc.a.com":             {"v=DMARC1; p=quarantine; rua=mailto:x@a.com"},
		},
	}
	rep, hosts := CheckDNS(context.Background(), r, "a.com", true, "default")
	for name, ch := range map[string]Check{"mx": rep.MX, "spf": rep.SPF, "dkim": rep.DKIM, "dmarc": rep.DMARC} {
		if ch.Status != OK {
			t.Errorf("%s = %+v", name, ch)
		}
	}
	if hosts[0] != "mx1.privateemail.com" || rep.DMARC.Detail != "policy quarantine" {
		t.Errorf("hosts=%v dmarc=%+v", hosts, rep.DMARC)
	}
	if ProviderFromMX(hosts) != ProviderPrivateEmail {
		t.Error("provider")
	}
}

func TestPrivateEmailProblems(t *testing.T) {
	r := fakeResolver{
		mx: map[string][]*net.MX{"b.com": {{Host: "mx1.privateemail.com.", Pref: 10}, {Host: "inbound.other.net.", Pref: 20}}},
		txt: map[string][]string{
			"b.com": {"v=spf1 include:_spf.google.com ~all"},
		},
	}
	rep, _ := CheckDNS(context.Background(), r, "b.com", true, "default")
	if rep.MX.Status != Wrong || rep.SPF.Status != Wrong || rep.DKIM.Status != Missing || rep.DMARC.Status != Missing {
		t.Errorf("got %+v", rep)
	}

	r.txt["b.com"] = []string{"v=spf1 a ~all", "v=spf1 mx ~all"}
	rep, _ = CheckDNS(context.Background(), r, "b.com", true, "default")
	if rep.SPF.Status != Wrong {
		t.Errorf("multiple SPF: %+v", rep.SPF)
	}
}

func TestNonPrivateEmailDKIMUnknown(t *testing.T) {
	r := fakeResolver{mx: map[string][]*net.MX{"c.com": {{Host: "eforward1.registrar-servers.com.", Pref: 10}}}}
	rep, hosts := CheckDNS(context.Background(), r, "c.com", false, "default")
	if rep.DKIM.Status != Unknown || rep.SPF.Status != Missing {
		t.Errorf("got %+v", rep)
	}
	if ProviderFromMX(hosts) != ProviderForwarding {
		t.Error("provider")
	}
}

func TestProviderFromMX(t *testing.T) {
	cases := map[Provider][]string{
		ProviderNone:      nil,
		ProviderGoogle:    {"aspmx.l.google.com"},
		ProviderMicrosoft: {"a-com.mail.protection.outlook.com"},
		ProviderOther:     {"mx.zoho.com"},
	}
	for want, hosts := range cases {
		if got := ProviderFromMX(hosts); got != want {
			t.Errorf("%v: got %s want %s", hosts, got, want)
		}
	}
}
