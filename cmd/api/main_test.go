package main

import "testing"

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		want    config
		wantErr bool
	}{
		{
			name: "defaults address",
			env:  map[string]string{"DATABASE_URL": "postgres://relay"},
			want: config{address: defaultAddress, databaseURL: "postgres://relay"},
		},
		{
			name: "uses environment address",
			env: map[string]string{
				"DATABASE_URL":   "postgres://relay",
				"RELAY_API_ADDR": "127.0.0.1:5000",
			},
			want: config{address: "127.0.0.1:5000", databaseURL: "postgres://relay"},
		},
		{
			name: "flag overrides environment address",
			args: []string{"-addr", "127.0.0.1:6000"},
			env: map[string]string{
				"DATABASE_URL":   "postgres://relay",
				"RELAY_API_ADDR": "127.0.0.1:5000",
			},
			want: config{address: "127.0.0.1:6000", databaseURL: "postgres://relay"},
		},
		{name: "requires database URL", wantErr: true},
		{
			name:    "rejects empty flag address",
			args:    []string{"-addr", ""},
			env:     map[string]string{"DATABASE_URL": "postgres://relay"},
			wantErr: true,
		},
		{
			name:    "rejects unexpected argument",
			args:    []string{"extra"},
			env:     map[string]string{"DATABASE_URL": "postgres://relay"},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			getenv := func(key string) string { return test.env[key] }
			got, err := parseConfig(test.args, getenv)
			if test.wantErr {
				if err == nil {
					t.Fatal("parseConfig() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseConfig() error = %v", err)
			}
			if got != test.want {
				t.Errorf("parseConfig() = %#v, want %#v", got, test.want)
			}
		})
	}
}
