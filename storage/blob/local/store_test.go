package local_test

import (
	"context"
	"testing"

	"github.com/ratrektlabs/rakit/storage/blob/local"
)

func TestLocalStoreReadWriteDelete(t *testing.T) {
	ctx := context.Background()
	s, err := local.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Write(ctx, "foo/bar.txt", []byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := s.Read(ctx, "foo/bar.txt")
	if err != nil || string(got) != "hello" {
		t.Fatalf("Read: err=%v got=%q", err, got)
	}

	// Missing file returns nil, nil.
	got, err = s.Read(ctx, "does-not-exist")
	if err != nil {
		t.Fatalf("Read(missing): err=%v", err)
	}
	if got != nil {
		t.Fatalf("Read(missing) got=%v want nil", got)
	}

	if err := s.Delete(ctx, "foo/bar.txt"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Read(ctx, "foo/bar.txt")
	if got != nil {
		t.Fatal("delete failed")
	}
}

func TestLocalStoreList(t *testing.T) {
	ctx := context.Background()
	s, err := local.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Write(ctx, "a/one.txt", []byte("1"))
	_ = s.Write(ctx, "a/two.txt", []byte("2"))
	_ = s.Write(ctx, "b/other.txt", []byte("x"))

	all, err := s.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("List(\"\") len=%d want 3: %v", len(all), all)
	}

	a, err := s.List(ctx, "a/")
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 2 {
		t.Fatalf("List(\"a/\") len=%d want 2: %v", len(a), a)
	}
}

func TestLocalStoreRejectsPathEscape(t *testing.T) {
	ctx := context.Background()
	s, err := local.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Write(ctx, "../evil.txt", []byte("x")); err == nil {
		t.Fatal("expected path escape error")
	}
}
