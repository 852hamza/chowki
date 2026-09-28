package netguard

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	tests := []struct {
		ip           string
		strict, open string // wanted error substring without and with AllowPrivate; "" means allowed
	}{
		{"8.8.8.8", "", ""},
		{"2606:4700::1111", "", ""},
		{"169.254.169.254", "always blocked", "always blocked"},
		{"fe80::1", "always blocked", "always blocked"},
		{"100.100.100.200", "always blocked", "always blocked"},
		{"fd00:ec2::254", "always blocked", "always blocked"},
		{"::ffff:169.254.169.254", "always blocked", "always blocked"},
		{"0.0.0.0", "not a unicast", "not a unicast"},
		{"::", "not a unicast", "not a unicast"},
		{"224.0.0.1", "not a unicast", "not a unicast"},
		{"127.0.0.1", "private", ""},
		{"::1", "private", ""},
		{"10.1.2.3", "private", ""},
		{"172.16.0.1", "private", ""},
		{"192.168.1.10", "private", ""},
		{"fd12:3456::1", "private", ""},
		{"100.64.0.1", "private", ""},
		{"::ffff:10.0.0.1", "private", ""},
	}
	for _, tt := range tests {
		ip := netip.MustParseAddr(tt.ip)
		for _, c := range []struct {
			policy Policy
			want   string
		}{{Policy{}, tt.strict}, {Policy{AllowPrivate: true}, tt.open}} {
			err := c.policy.Check(ip)
			switch {
			case c.want == "" && err != nil:
				t.Errorf("%+v.Check(%s) = %v, want allowed", c.policy, tt.ip, err)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Errorf("%+v.Check(%s) = %v, want error containing %q", c.policy, tt.ip, err, c.want)
			}
		}
	}
}

func TestCheckHost(t *testing.T) {
	strict := Policy{}
	for host, wantErr := range map[string]bool{
		"api.openai.com":  false, // checked later, at dial time
		"localhost":       true,
		"LOCALHOST.":      true,
		"127.0.0.1":       true,
		"[::1]":           true,
		"169.254.169.254": true,
		"93.184.215.14":   false,
	} {
		if err := strict.CheckHost(host); (err != nil) != wantErr {
			t.Errorf("CheckHost(%q) = %v, want error %v", host, err, wantErr)
		}
	}
}

func TestControlBlocksDialing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)

	for _, policy := range []Policy{{}, {AllowPrivate: true}} {
		dialer := &net.Dialer{Control: policy.Control}
		client := &http.Client{Transport: &http.Transport{DialContext: dialer.DialContext}}
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
		// The test server listens on loopback, a private address.
		if blocked := err != nil; blocked == policy.AllowPrivate {
			t.Errorf("%+v: dial error = %v", policy, err)
		}
		if err != nil && !strings.Contains(err.Error(), "netguard: 127.0.0.1 is a private address") {
			t.Errorf("%+v: error = %v, want the netguard reason", policy, err)
		}
	}
}
