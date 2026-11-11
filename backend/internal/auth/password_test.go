package auth

import (
	"strings"
	"testing"
)

func TestHashVerifyRoundtrip(t *testing.T) {
	h, err := Hash("correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$") {
		t.Fatalf("hash = %q", h)
	}
	if err := Verify(h, "correct-horse"); err != nil {
		t.Fatalf("verify own hash: %v", err)
	}
	if err := Verify(h, "wrong"); err == nil {
		t.Fatal("wrong password accepted")
	}
}

func TestHashSaltsUniquely(t *testing.T) {
	a, _ := Hash("same")
	b, _ := Hash("same")
	if a == b {
		t.Fatal("identical hashes: salt not random")
	}
}

func TestHashRejectsEmpty(t *testing.T) {
	if _, err := Hash(""); err != ErrEmptyPassword {
		t.Fatalf("err = %v", err)
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "plaintext", "$argon2id$v=19$m=1,t=1,p=1$!!!$???", "$bcrypt$10$abc"} {
		if err := Verify(bad, "x"); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
