package main

import (
	"bytes"
	"runtime"
	"testing"
)

func TestRun(t *testing.T) {
	version := "Go version: " + runtime.Version() + "\n"

	tests := []struct {
		name string
		user string
		args []string
		want string
	}{
		{
			name: "no arguments",
			user: "alice",
			want: "User: alice\nArguments: none\n" + version,
		},
		{
			name: "several arguments",
			user: "alice",
			args: []string{"hello", "big world", "привет"},
			want: "User: alice\nArguments (3):\n" +
				"  1: \"hello\"\n  2: \"big world\"\n  3: \"привет\"\n" + version,
		},
		{
			name: "user is not set",
			user: "",
			want: "User: unknown (USER and USERNAME are not set)\nArguments: none\n" + version,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("USER", tt.user)
			t.Setenv("USERNAME", tt.user)

			var out bytes.Buffer
			run(&out, tt.args)
			if got := out.String(); got != tt.want {
				t.Errorf("run() output:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestUserName(t *testing.T) {
	// The variable native to the OS wins when both are set.
	primary, fallback := "USER", "USERNAME"
	if runtime.GOOS == "windows" {
		primary, fallback = "USERNAME", "USER"
	}

	tests := []struct {
		name          string
		primaryValue  string
		fallbackValue string
		want          string
		wantOK        bool
	}{
		{name: "both set", primaryValue: "alice", fallbackValue: "bob", want: "alice", wantOK: true},
		{name: "only fallback set", primaryValue: "", fallbackValue: "bob", want: "bob", wantOK: true},
		{name: "nothing set", primaryValue: "", fallbackValue: "", want: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(primary, tt.primaryValue)
			t.Setenv(fallback, tt.fallbackValue)

			got, ok := userName()
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("userName() = %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
