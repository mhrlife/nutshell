package browser

import (
	"reflect"
	"testing"
)

func TestLaunchers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		goos string
		env  string
		want [][]string
	}{
		{"linux defaults", "linux", "", [][]string{{"xdg-open"}, {"sensible-browser"}, {"x-www-browser"}, {"wslview"}}},
		{"darwin", "darwin", "", [][]string{{"open"}}},
		{"windows", "windows", "", [][]string{{"rundll32", "url.dll,FileProtocolHandler"}}},
		{"BROWSER wins and keeps its arguments", "linux", "firefox --private-window", [][]string{{"firefox", "--private-window"}, {"xdg-open"}, {"sensible-browser"}, {"x-www-browser"}, {"wslview"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := launchers(tt.goos, tt.env); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("launchers(%q, %q) = %v, want %v", tt.goos, tt.env, got, tt.want)
			}
		})
	}
}
