package manager

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/sit/sit/internal/manager/store"
)

func newAuth(t *testing.T) (*Auth, store.Store) {
	t.Helper()
	s, err := store.OpenSQLite(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return NewAuth(s), s
}

func TestAuth_AdminLoginAndVerify(t *testing.T) {
	a, _ := newAuth(t)
	ctx := context.Background()
	if err := a.SeedAdmin(ctx, "admin", "s3cret"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Login(ctx, "admin", "wrong", time.Hour); err != ErrBadCredentials {
		t.Fatalf("wrong pw: got %v want ErrBadCredentials", err)
	}
	tok, exp, err := a.Login(ctx, "admin", "s3cret", time.Hour)
	if err != nil || tok == "" || exp == 0 {
		t.Fatalf("login: tok=%q exp=%d err=%v", tok, exp, err)
	}
	user, err := a.VerifyAdmin(tok)
	if err != nil || user != "admin" {
		t.Fatalf("verify: user=%q err=%v", user, err)
	}
	a.Logout(tok)
	if _, err := a.VerifyAdmin(tok); err != ErrTokenInvalid {
		t.Errorf("after logout: got %v want ErrTokenInvalid", err)
	}
}

func TestAuth_AdminTokenExpiry(t *testing.T) {
	a, _ := newAuth(t)
	ctx := context.Background()
	_ = a.SeedAdmin(ctx, "admin", "pw")
	tok, _, _ := a.Login(ctx, "admin", "pw", -time.Second) // already expired
	if _, err := a.VerifyAdmin(tok); err != ErrTokenInvalid {
		t.Errorf("expired token: got %v want ErrTokenInvalid", err)
	}
}

func TestAuth_EnrollTokenSingleUseAndCredential(t *testing.T) {
	a, _ := newAuth(t)
	ctx := context.Background()
	etok, _, err := a.MintEnrollToken(ctx, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	secret, nodeID, err := a.Enroll(ctx, etok, "")
	if err != nil || secret == "" || nodeID == "" {
		t.Fatalf("enroll: secret=%q node=%q err=%v", secret, nodeID, err)
	}
	// token cannot be reused
	if _, _, err := a.Enroll(ctx, etok, ""); err != ErrTokenInvalid {
		t.Errorf("reuse enroll token: got %v want ErrTokenInvalid", err)
	}
	// the issued credential verifies
	if err := a.VerifyNode(ctx, nodeID, secret); err != nil {
		t.Errorf("VerifyNode good secret: %v", err)
	}
	// wrong secret rejected
	if err := a.VerifyNode(ctx, nodeID, "bogus"); err != ErrBadCredentials {
		t.Errorf("VerifyNode bad secret: got %v", err)
	}
	// node row was created
	if _, err := a.store.GetNode(ctx, nodeID); err != nil {
		t.Errorf("node row missing after enroll: %v", err)
	}
}

func TestAuth_RevokeRejectsHandshake(t *testing.T) {
	a, _ := newAuth(t)
	ctx := context.Background()
	etok, _, _ := a.MintEnrollToken(ctx, time.Hour)
	secret, nodeID, _ := a.Enroll(ctx, etok, "")
	if err := a.VerifyNode(ctx, nodeID, secret); err != nil {
		t.Fatalf("pre-revoke verify: %v", err)
	}
	if err := a.RevokeNode(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	if err := a.VerifyNode(ctx, nodeID, secret); err != ErrRevoked {
		t.Errorf("after revoke: got %v want ErrRevoked", err)
	}
}
