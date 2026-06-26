package manager

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/sit/sit/internal/manager/store"
	"github.com/sit/sit/internal/protocol"
	"golang.org/x/crypto/bcrypt"
)

// Auth errors.
var (
	ErrBadCredentials = errors.New("auth: invalid credentials")
	ErrTokenInvalid   = errors.New("auth: token invalid or expired")
	ErrRevoked        = errors.New("auth: credential revoked")
)

// Auth handles the three separate credential systems:
//   - admin sessions (REST bearer tokens)
//   - node long-term credentials (WSS handshake)
//   - one-time enrollment tokens (first node contact)
//
// All secrets are stored hashed only.
type Auth struct {
	store store.Store

	mu       sync.Mutex
	sessions map[string]session // admin token -> session
}

type session struct {
	username  string
	expiresAt int64
}

// NewAuth constructs an Auth backed by store.
func NewAuth(s store.Store) *Auth {
	return &Auth{store: s, sessions: make(map[string]session)}
}

// randToken returns a 256-bit cryptographically-random hex token.
func randToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// hashToken hashes a high-entropy token with SHA-256 (suitable because the
// token itself is random; bcrypt is reserved for low-entropy passwords).
func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// ---- Admin sessions ----

// SeedAdmin creates or updates an admin account with a bcrypt-hashed password.
func (a *Auth) SeedAdmin(ctx context.Context, username, password string) error {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return a.store.PutAdmin(ctx, store.Admin{Username: username, PasswordHash: string(h), CreatedAt: protocol.NowMillis()})
}

// Login verifies the password and issues an admin bearer token valid for ttl.
func (a *Auth) Login(ctx context.Context, username, password string, ttl time.Duration) (token string, expiresAt int64, err error) {
	adm, err := a.store.GetAdmin(ctx, username)
	if err != nil {
		return "", 0, ErrBadCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(adm.PasswordHash), []byte(password)) != nil {
		return "", 0, ErrBadCredentials
	}
	token = randToken()
	expiresAt = time.Now().Add(ttl).UnixMilli()
	a.mu.Lock()
	a.sessions[token] = session{username: username, expiresAt: expiresAt}
	a.mu.Unlock()
	return token, expiresAt, nil
}

// VerifyAdmin returns the username for a valid, unexpired admin token.
func (a *Auth) VerifyAdmin(token string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[token]
	if !ok {
		return "", ErrTokenInvalid
	}
	if protocol.NowMillis() > s.expiresAt {
		delete(a.sessions, token)
		return "", ErrTokenInvalid
	}
	return s.username, nil
}

// Logout invalidates an admin token.
func (a *Auth) Logout(token string) {
	a.mu.Lock()
	delete(a.sessions, token)
	a.mu.Unlock()
}

// ---- Enrollment tokens ----

// MintEnrollToken creates a one-time enrollment token valid for ttl and returns
// the plaintext (shown once). Only the hash is stored.
func (a *Auth) MintEnrollToken(ctx context.Context, ttl time.Duration) (token string, expiresAt int64, err error) {
	token = randToken()
	expiresAt = time.Now().Add(ttl).UnixMilli()
	err = a.store.PutEnrollToken(ctx, store.EnrollToken{
		TokenHash: hashToken(token),
		State:     store.EnrollUnused,
		ExpiresAt: expiresAt,
		CreatedAt: protocol.NowMillis(),
	})
	if err != nil {
		return "", 0, err
	}
	return token, expiresAt, nil
}

// Enroll consumes a one-time enroll token and issues a long-term node
// credential. Returns the node's plaintext secret (shown once) and node_id.
// If nodeID is empty a fresh ULID is generated.
func (a *Auth) Enroll(ctx context.Context, enrollToken, nodeID string) (secret, assignedNodeID string, err error) {
	ok, err := a.store.ConsumeEnrollToken(ctx, hashToken(enrollToken), protocol.NowMillis())
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", "", ErrTokenInvalid
	}
	if nodeID == "" {
		nodeID = protocol.NewID()
	}
	secret = randToken()
	now := protocol.NowMillis()
	if err := a.store.PutCredential(ctx, store.Credential{
		NodeID:     nodeID,
		SecretHash: hashToken(secret),
		State:      store.CredActive,
		IssuedAt:   now,
	}); err != nil {
		return "", "", err
	}
	// Ensure a node row exists so the device is visible immediately.
	if _, err := a.store.GetNode(ctx, nodeID); errors.Is(err, store.ErrNotFound) {
		_ = a.store.UpsertNode(ctx, store.Node{NodeID: nodeID, AddrsJSON: "[]", Status: "offline", CreatedAt: now, LastSeen: now})
	}
	return secret, nodeID, nil
}

// ---- Node credential verification (WSS handshake) ----

// VerifyNode checks a node's long-term secret against the active credential.
// The secret is presented as "<node_id>:<secret>" or the secret with nodeID
// known out-of-band; here we take both explicitly.
func (a *Auth) VerifyNode(ctx context.Context, nodeID, secret string) error {
	cred, err := a.store.GetCredential(ctx, nodeID)
	if err != nil {
		return ErrBadCredentials
	}
	if cred.State == store.CredRevoked {
		return ErrRevoked
	}
	want := hashToken(secret)
	if subtle.ConstantTimeCompare([]byte(want), []byte(cred.SecretHash)) != 1 {
		return ErrBadCredentials
	}
	return nil
}

// RevokeNode revokes a node's credential (kicks + refuses reconnect).
func (a *Auth) RevokeNode(ctx context.Context, nodeID string) error {
	return a.store.RevokeCredential(ctx, nodeID, protocol.NowMillis())
}
