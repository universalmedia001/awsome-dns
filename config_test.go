package main

import "testing"

func TestDefaultHTTPAddress(t *testing.T) {
	tests := []struct {
		name        string
		renderPort  string
		override    string
		wantAddress string
	}{
		{name: "local default", wantAddress: defaultHTTPListenAddress},
		{name: "Render port", renderPort: "10000", wantAddress: "0.0.0.0:10000"},
		{name: "explicit override", renderPort: "10000", override: "127.0.0.1:8080", wantAddress: "127.0.0.1:8080"},
		{name: "override without Render port", override: "127.0.0.1:8080", wantAddress: "127.0.0.1:8080"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("PORT", test.renderPort)
			t.Setenv("AWSOME_DNS_HTTP_ADDR", test.override)
			if got := DefaultConfig().HTTPListenAddress; got != test.wantAddress {
				t.Errorf("HTTPListenAddress = %q, want %q", got, test.wantAddress)
			}
		})
	}
}
