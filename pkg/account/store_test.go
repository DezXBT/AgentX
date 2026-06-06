package account

import (
	"path/filepath"
	"testing"
)

func TestStoreAddListResolvePersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if err := s.Add(&Account{Name: "a", AuthToken: "tok-a", CT0: "ct-a"}); err != nil {
		t.Fatalf("add a: %v", err)
	}
	if err := s.Add(&Account{Name: "b", AuthToken: "tok-b", CT0: "ct-b"}); err != nil {
		t.Fatalf("add b: %v", err)
	}

	if s.Default() != "a" {
		t.Errorf("default = %q, want a (first added)", s.Default())
	}
	if got := len(s.List()); got != 2 {
		t.Errorf("list len = %d, want 2", got)
	}

	// default resolution
	def, err := s.Resolve("")
	if err != nil || def.Name != "a" {
		t.Errorf("resolve default = %v, %v", def, err)
	}

	// reload from disk preserves state
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if s2.Default() != "a" || len(s2.List()) != 2 {
		t.Errorf("reload mismatch: default=%q len=%d", s2.Default(), len(s2.List()))
	}
	b, err := s2.Get("b")
	if err != nil || b.AuthToken != "tok-b" {
		t.Errorf("get b = %v, %v", b, err)
	}

	// remove default reassigns
	if err := s2.Remove("a"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if s2.Default() != "b" {
		t.Errorf("after remove default = %q, want b", s2.Default())
	}
}

func TestAccountValidateAndCookie(t *testing.T) {
	a := &Account{Name: "x", AuthToken: "tok", CT0: "csrf"}
	if err := a.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if got := a.Cookie(); got != "auth_token=tok; ct0=csrf" {
		t.Errorf("cookie = %q", got)
	}
	a.CookieString = "auth_token=tok; ct0=csrf; guest_id=v1"
	if a.Cookie() != a.CookieString {
		t.Error("expected full cookie string to be used verbatim")
	}

	bad := &Account{Name: "x"}
	if err := bad.Validate(); err == nil {
		t.Error("expected validation error for missing tokens")
	}
}
