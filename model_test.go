package main

import (
	"strings"
	"testing"
)

func TestRepositoryRepoString(t *testing.T) {
	cases := []struct {
		name    string
		repo    Repository
		want    string
		wantErr bool
	}{
		{"local", Repository{BackendType: "Local", LocalPath: "/srv/repo"}, "/srv/repo", false},
		{"local trims", Repository{BackendType: "Local", LocalPath: "  /srv/repo  "}, "/srv/repo", false},
		{"local empty", Repository{BackendType: "Local"}, "", true},
		{"s3", Repository{BackendType: "S3", Endpoint: "https://s3.amazonaws.com", Bucket: "b"}, "s3:https://s3.amazonaws.com/b", false},
		{"s3 trailing slash", Repository{BackendType: "S3", Endpoint: "https://s3.amazonaws.com/", Bucket: "b"}, "s3:https://s3.amazonaws.com/b", false},
		{"s3 missing bucket", Repository{BackendType: "S3", Endpoint: "https://s3.amazonaws.com"}, "", true},
		{"unknown backend", Repository{BackendType: "FTP"}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.repo.Repo()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Repo() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRepositoryValidate(t *testing.T) {
	ok := Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/x", Password: "pw"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid repo rejected: %v", err)
	}
	noPass := Repository{Meta: Meta{Name: "r"}, BackendType: "Local", LocalPath: "/x"}
	if err := noPass.Validate(); err == nil {
		t.Fatal("missing password should fail validation")
	}
	noName := Repository{BackendType: "Local", LocalPath: "/x", Password: "pw"}
	if err := noName.Validate(); err == nil {
		t.Fatal("missing name should fail validation")
	}
}

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

func TestRepositoryEnvLocal(t *testing.T) {
	r := Repository{BackendType: "Local", LocalPath: "/x", Password: "secret-pw"}
	m := envMap(r.Env())
	if m["RESTIC_PASSWORD"] != "secret-pw" {
		t.Fatalf("RESTIC_PASSWORD = %q", m["RESTIC_PASSWORD"])
	}
	if m["RESTIC_PROGRESS_FPS"] == "" {
		t.Fatal("RESTIC_PROGRESS_FPS should be pinned")
	}
	if _, ok := m["AWS_ACCESS_KEY_ID"]; ok {
		t.Fatal("Local backend should not set AWS credentials")
	}
}

func TestRepositoryEnvS3(t *testing.T) {
	r := Repository{
		BackendType: "S3", Endpoint: "https://s3", Bucket: "b", Region: "us-east-1",
		AccessKey: "AK", SecretKey: "SK", Password: "pw",
	}
	m := envMap(r.Env())
	if m["AWS_ACCESS_KEY_ID"] != "AK" || m["AWS_SECRET_ACCESS_KEY"] != "SK" {
		t.Fatalf("AWS creds not set: %v", m)
	}
	if m["AWS_DEFAULT_REGION"] != "us-east-1" {
		t.Fatalf("AWS region = %q", m["AWS_DEFAULT_REGION"])
	}
	if m["RESTIC_PASSWORD"] != "pw" {
		t.Fatalf("RESTIC_PASSWORD = %q", m["RESTIC_PASSWORD"])
	}
}

func TestRunStatusTerminal(t *testing.T) {
	terminal := []RunStatus{StatusSuccess, StatusSuccessWarnings, StatusFailed, StatusCanceled, StatusInterrupted}
	for _, s := range terminal {
		if !s.Terminal() {
			t.Fatalf("%s should be terminal", s)
		}
		if s.Active() {
			t.Fatalf("%s should not be active", s)
		}
	}
	for _, s := range []RunStatus{StatusStarting, StatusRunning} {
		if s.Terminal() {
			t.Fatalf("%s should not be terminal", s)
		}
		if !s.Active() {
			t.Fatalf("%s should be active", s)
		}
	}
}

func TestRunCloneIsDeep(t *testing.T) {
	code := 3
	orig := &Run{
		ID:      "r1",
		Params:  map[string]string{"source": "/x"},
		Summary: &Summary{SnapshotID: "abc"},
		ExitCode: &code,
	}
	cp := orig.clone()
	cp.Params["source"] = "/y"
	cp.Summary.SnapshotID = "def"
	*cp.ExitCode = 9
	if orig.Params["source"] != "/x" {
		t.Fatal("clone shares Params map")
	}
	if orig.Summary.SnapshotID != "abc" {
		t.Fatal("clone shares Summary pointer")
	}
	if *orig.ExitCode != 3 {
		t.Fatal("clone shares ExitCode pointer")
	}
}
