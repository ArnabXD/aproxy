package scraper

import "testing"

func TestParseLine(t *testing.T) {
	cases := []struct {
		in          string
		defaultType string
		want        Proxy // zero Host means expect ok=false
	}{
		{"1.2.3.4:8080", "http", Proxy{Host: "1.2.3.4", Port: 8080, Type: "http"}},
		{"socks5://9.9.9.9:1080", "http", Proxy{Host: "9.9.9.9", Port: 1080, Type: "socks5"}},
		{"  5.6.7.8:3128  ", "https", Proxy{Host: "5.6.7.8", Port: 3128, Type: "https"}},
		{"# comment", "http", Proxy{}},
		{"", "http", Proxy{}},
		{"garbage-no-port", "http", Proxy{}},
		{"1.2.3.4:notaport", "http", Proxy{}},
	}

	for _, c := range cases {
		got, ok := parseLine(c.in, c.defaultType)
		wantOK := c.want.Host != ""
		if ok != wantOK {
			t.Errorf("parseLine(%q): ok=%v, want %v", c.in, ok, wantOK)
			continue
		}
		if ok && (got.Host != c.want.Host || got.Port != c.want.Port || got.Type != c.want.Type) {
			t.Errorf("parseLine(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}
