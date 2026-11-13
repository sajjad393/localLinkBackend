package main

import "testing"

func TestPort80Wanted299(t *testing.T) {
	cases := []struct {
		addr, env string
		tls       bool
		want      bool
	}{
		{":8080", "", false, true},
		{":8080", "OFF", false, false},
		{":8080", "", true, false},
		{"127.0.0.1:18080", "", false, false},
		{":9000", "", false, false},
	}
	for _, c := range cases {
		if got := port80Wanted299(c.addr, c.env, c.tls); got != c.want {
			t.Fatalf("%+v got %v", c, got)
		}
	}
}

func TestMDNSName299(t *testing.T) {
	for in, want := range map[string]string{"": "linkup.local", "off": "", " LinkUp ": "linkup.local", "office.local": "office.local"} {
		if got := mdnsName299(in); got != want {
			t.Fatalf("%q -> %q want %q", in, got, want)
		}
	}
	if portOf299(":8080") != 8080 || portOf299("127.0.0.1:18080") != 18080 || portOf299("bad") != 8080 {
		t.Fatal("portOf299")
	}
}
