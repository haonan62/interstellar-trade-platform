package main

import (
	"strings"
	"time"
)

func (s *Service) NeedsBootstrap() (bool, error) {
	return StoreReadScoped(s.store, Scope{Counts: true}, func(state *AppState) (bool, error) { return userCount(state) == 0, nil })
}

func (s *Service) Bootstrap(req BootstrapRequest) (AuthResponse, error) {
	username := normalizeUsername(req.Username)
	if username == "" || len(req.Password) < 8 || len(req.Password) > maxPasswordLen {
		return AuthResponse{}, badRequest("username and password of at least 8 characters are required")
	}
	display := strings.TrimSpace(req.DisplayName)
	if display == "" {
		display = username
	}
	// Cheap rejection first so a bootstrapped system cannot be made to burn
	// CPU on hashing by anonymous callers.
	if needs, err := s.NeedsBootstrap(); err != nil {
		return AuthResponse{}, err
	} else if !needs {
		return AuthResponse{}, conflict("bootstrap has already been completed")
	}
	hash, err := hashPasswordArgon2id(req.Password) // outside any lock
	if err != nil {
		return AuthResponse{}, internalErr(err.Error())
	}
	return StoreUpdateScoped(s.store, Scope{Lock: "bootstrap", AllUsers: true, UserLimit: 1, Usernames: []string{username}}, func(state *AppState) (AuthResponse, error) {
		if len(state.Users) > 0 {
			return AuthResponse{}, conflict("bootstrap has already been completed")
		}
		if userExistsByUsername(state, username) {
			return AuthResponse{}, conflict("username already exists")
		}
		user, err := newUser(username, display, hash, "", []string{"super_admin"})
		if err != nil {
			return AuthResponse{}, err
		}
		state.Users[user.ID] = user
		return createSessionForUser(state, user)
	})
}

// Login verifies the password WITHOUT holding the store lock (hashing is slow
// by design), then takes the write lock only for the short session insert.
//
// Failure handling (Phase 4): every failure answers the same 401 "invalid
// credentials" (AppError.Reason carries the real cause for the audit log only).
// After maxFailedLogins consecutive failures the account is locked for
// lockoutDuration; while locked the password is not even checked. The counter
// is stored with the user, so it holds across replicas and restarts. Trade-off:
// an attacker who knows a username can keep it locked (denial of service);
// admins can clear it with a password reset, and nginx limits per-IP rate.
func (s *Service) Login(req LoginRequest) (AuthResponse, error) {
	username := normalizeUsername(req.Username)
	if username == "" || req.Password == "" {
		return AuthResponse{}, badRequest("username and password are required")
	}
	if len(req.Password) > maxPasswordLen {
		return AuthResponse{}, invalidCreds("oversized_password")
	}
	type cred struct {
		id, salt, hash, secret string
		lastStep               int64
		ok, locked, mfa        bool
	}
	c, _ := StoreReadScoped(s.store, Scope{Usernames: []string{username}}, func(state *AppState) (cred, error) {
		u := findUserByUsername(state, username)
		if u == nil || !u.Active {
			return cred{}, nil
		}
		return cred{id: u.ID, salt: u.PasswordSalt, hash: u.PasswordHash, ok: true, locked: isLocked(u), mfa: u.MFAEnabled, secret: u.MFASecret, lastStep: u.MFALastStep}, nil
	})
	if !c.ok {
		burnPasswordCheck(req.Password)
		return AuthResponse{}, invalidCreds("unknown_or_inactive")
	}
	if c.locked {
		burnPasswordCheck(req.Password)
		return AuthResponse{}, invalidCreds("locked")
	}
	matched, legacy := verifyPassword(req.Password, c.salt, c.hash)
	if !matched {
		reason := "bad_password"
		if s.noteLoginFailure(c.id) {
			reason = "bad_password_lockout_started"
		}
		return AuthResponse{}, invalidCreds(reason)
	}
	if c.mfa {
		if strings.TrimSpace(req.TOTPCode) == "" {
			e := unauthorized("a one-time code is required")
			e.Code, e.Reason = "mfa_required", "mfa_code_missing"
			return AuthResponse{}, e
		}
		if verifyTOTP(c.secret, req.TOTPCode, c.lastStep) == 0 {
			reason := "bad_totp"
			if s.noteLoginFailure(c.id) {
				reason = "bad_totp_lockout_started"
			}
			return AuthResponse{}, invalidCreds(reason)
		}
	}
	upgraded := ""
	if legacy { // migration path: PBKDF2 -> argon2id on successful login
		h, err := hashPasswordArgon2id(req.Password)
		if err != nil {
			return AuthResponse{}, internalErr(err.Error())
		}
		upgraded = h
	}
	return StoreUpdateScoped(s.store, Scope{Users: []string{c.id}, UserSessions: []string{c.id}, PruneSessions: true}, func(state *AppState) (AuthResponse, error) {
		user := state.Users[c.id]
		// Re-validate: the account may have been deactivated or its password
		// changed while we were hashing.
		if user == nil || !user.Active || user.PasswordHash != c.hash || isLocked(user) {
			return AuthResponse{}, invalidCreds("changed_during_login")
		}
		if user.MFAEnabled {
			// Re-verify inside the transaction so one code cannot be used twice
			// by two concurrent logins.
			step := verifyTOTP(user.MFASecret, req.TOTPCode, user.MFALastStep)
			if step == 0 {
				return AuthResponse{}, invalidCreds("totp_replay")
			}
			user.MFALastStep = step
		}
		if upgraded != "" {
			user.PasswordHash, user.PasswordSalt = upgraded, ""
		}
		user.FailedLogins, user.LockedUntil = 0, ""
		return createSessionForUser(state, user)
	})
}

func invalidCreds(reason string) *AppError {
	e := unauthorized("invalid credentials")
	e.Reason = reason
	return e
}

// noteLoginFailure counts a failed attempt and reports whether it started a
// lockout. Errors are ignored on purpose: a failed login must stay a 401.
func (s *Service) noteLoginFailure(userID string) bool {
	locked, _ := StoreUpdateScoped(s.store, Scope{Users: []string{userID}}, func(state *AppState) (bool, error) {
		u := state.Users[userID]
		if u == nil {
			return false, nil
		}
		u.FailedLogins++
		if u.FailedLogins >= maxFailedLogins {
			u.FailedLogins = 0
			u.LockedUntil = lockClock().UTC().Add(lockoutDuration).Format(time.RFC3339)
			return true, nil
		}
		return false, nil
	})
	return locked
}

func (s *Service) Logout(token string) error {
	_, err := StoreUpdateScoped(s.store, Scope{Tokens: []string{token}}, func(state *AppState) (struct{}, error) { delete(state.Sessions, token); return struct{}{}, nil })
	return err
}
func (s *Service) Me(token string) (UserView, error) {
	return StoreReadScoped(s.store, Scope{Tokens: []string{token}}, func(state *AppState) (UserView, error) {
		// No MFA gate here: the UI needs /auth/me to learn that enrolment is due.
		actor, err := actorFromTokenNoMFA(state, token)
		if err != nil {
			return UserView{}, err
		}
		return toUserView(actor), nil
	})
}
