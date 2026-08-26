package main

import (
	"errors"
	"testing"
)

func newTestRepoStore(t *testing.T) *repoStore {
	t.Helper()
	return newRepoStore(testPool(t))
}

func TestEntityStoreCreateGetList(t *testing.T) {
	s := newTestRepoStore(t)

	created, err := s.Create(Repository{Meta: Meta{Name: "Local one"}, BackendType: "Local", LocalPath: "/tmp/repo", Password: "pw"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("Create did not assign an id")
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatal("Create did not stamp timestamps")
	}

	got, ok := s.Get(created.ID)
	if !ok {
		t.Fatal("Get returned not found for a just-created entity")
	}
	if got.Name != "Local one" || got.LocalPath != "/tmp/repo" {
		t.Fatalf("Get returned wrong entity: %+v", got)
	}

	got.Name = "mutated"
	again, _ := s.Get(created.ID)
	if again.Name != "Local one" {
		t.Fatal("stored entity was mutated through a returned copy")
	}

	if n := s.Count(); n != 1 {
		t.Fatalf("Count = %d, want 1", n)
	}
	if list := s.List(); len(list) != 1 {
		t.Fatalf("List len = %d, want 1", len(list))
	}
}

func TestEntityStoreNameUniqueness(t *testing.T) {
	s := newTestRepoStore(t)

	if _, err := s.Create(Repository{Meta: Meta{Name: "Repo"}, BackendType: "Local", LocalPath: "/a", Password: "pw"}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, err := s.Create(Repository{Meta: Meta{Name: "repo"}, BackendType: "Local", LocalPath: "/b", Password: "pw"})
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("duplicate name: want ConflictError, got %v", err)
	}
}

func TestEntityStoreUpdate(t *testing.T) {
	s := newTestRepoStore(t)
	a, _ := s.Create(Repository{Meta: Meta{Name: "A"}, BackendType: "Local", LocalPath: "/a", Password: "pw"})
	b, _ := s.Create(Repository{Meta: Meta{Name: "B"}, BackendType: "Local", LocalPath: "/b", Password: "pw"})

	upd := a
	upd.Name = "A2"
	upd.LocalPath = "/a2"
	out, err := s.Update(a.ID, upd)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if out.Name != "A2" || out.LocalPath != "/a2" {
		t.Fatalf("Update result wrong: %+v", out)
	}
	if !out.CreatedAt.Equal(a.CreatedAt) {
		t.Fatal("Update did not preserve CreatedAt")
	}

	upd2 := out
	upd2.Name = "B"
	if _, err := s.Update(out.ID, upd2); err == nil {
		t.Fatal("Update to a taken name should conflict")
	}
	_ = b

	if _, err := s.Update("nope", upd); !errors.As(err, new(*NotFoundError)) {
		t.Fatalf("Update missing: want NotFoundError, got %v", err)
	}
}

func TestEntityStoreDeleteAndPersistence(t *testing.T) {
	s := newTestRepoStore(t)
	a, _ := s.Create(Repository{Meta: Meta{Name: "A"}, BackendType: "Local", LocalPath: "/a", Password: "pw"})
	_, _ = s.Create(Repository{Meta: Meta{Name: "B"}, BackendType: "Local", LocalPath: "/b", Password: "pw"})

	if err := s.Delete(a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if s.Exists(a.ID) {
		t.Fatal("entity still present after Delete")
	}
	if err := s.Delete("missing"); !errors.As(err, new(*NotFoundError)) {
		t.Fatalf("Delete missing: want NotFoundError, got %v", err)
	}

	reloaded := newRepoStore(testPool(t))
	if reloaded.Count() != 1 {
		t.Fatalf("reloaded Count = %d, want 1", reloaded.Count())
	}
	if list := reloaded.List(); list[0].Name != "B" {
		t.Fatalf("reloaded survivor = %q, want B", list[0].Name)
	}
}

func TestEntityStoreEmptyOnOpen(t *testing.T) {
	s := newTestRepoStore(t)
	if s.Count() != 0 {
		t.Fatalf("fresh store Count = %d, want 0", s.Count())
	}
	if list := s.List(); len(list) != 0 {
		t.Fatalf("fresh store List len = %d, want 0", len(list))
	}
}

func TestFolderAndJobStores(t *testing.T) {
	pool := testPool(t)
	rs := newRepoStore(pool)
	repo, err := rs.Create(Repository{Meta: Meta{Name: "R"}, BackendType: "Local", LocalPath: "/tmp/r", Password: "pw"})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}

	fs := newFolderStore(pool)
	f, err := fs.Create(Folder{Meta: Meta{Name: "Docs"}, Path: "/home/me/docs"})
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	if f.Path != "/home/me/docs" {
		t.Fatalf("folder path wrong: %+v", f)
	}

	js := newJobStore(pool)
	j, err := js.Create(Job{Meta: Meta{Name: "Nightly"}, FolderID: f.ID, RepositoryID: repo.ID})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if j.FolderID != f.ID || j.RepositoryID != repo.ID {
		t.Fatalf("job references wrong: %+v", j)
	}
	if j.ResticTag() != "resticweb-job:"+j.ID {
		t.Fatalf("ResticTag = %q", j.ResticTag())
	}
}
