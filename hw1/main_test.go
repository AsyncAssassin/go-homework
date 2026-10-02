package main

import "testing"

func TestUserName(t *testing.T) {
	tests := []struct {
		name     string
		user     string
		username string
		want     string
	}{
		{name: "USER is set", user: "alice", username: "bob", want: "alice"},
		{name: "only USERNAME is set", user: "", username: "bob", want: "bob"},
		{name: "nothing is set", user: "", username: "", want: "unknown (USER and USERNAME are not set)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("USER", tt.user)
			t.Setenv("USERNAME", tt.username)

			if got := userName(); got != tt.want {
				t.Errorf("userName() = %q, want %q", got, tt.want)
			}
		})
	}
}
