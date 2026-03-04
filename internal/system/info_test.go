package system

import "testing"

func TestParseWhoOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "single user",
			in:   "ubuntu pts/0 2026-03-04 07:00 (10.0.0.5)\n",
			want: "ubuntu",
		},
		{
			name: "skip reboot line",
			in:   "reboot system boot 2026-03-04 06:00\nhakan pts/1 2026-03-04 07:10 (10.0.0.6)\n",
			want: "hakan",
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseWhoOutput(tt.in)
			if got != tt.want {
				t.Fatalf("parseWhoOutput()=%q want=%q", got, tt.want)
			}
		})
	}
}
