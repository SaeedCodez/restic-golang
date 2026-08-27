package core

import "testing"

func TestClassifyResticFailure(t *testing.T) {
	cases := []struct {
		text string
		want int
	}{
		{"Fatal: unable to open config file: stat /x/config: no such file or directory\nIs there a repository at the following location?\n/x", resticExitNotInitialized},
		{"Fatal: repository does not exist: unable to open config file", resticExitNotInitialized},
		{"wrong password or no key found", resticExitBadPassword},
		{"Fatal: unable to create lock in backend: repository is already locked", resticExitLocked},
		{"some unrelated failure", 0},
		{"open /data/src: no such file or directory", 0}, // source path missing — not a repo init signal
	}
	for _, tc := range cases {
		if got := classifyResticFailure(tc.text); got != tc.want {
			t.Fatalf("classifyResticFailure(%q)=%d want %d", tc.text, got, tc.want)
		}
	}
}
