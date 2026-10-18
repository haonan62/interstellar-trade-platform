package main

import (
	"crypto/subtle"
	"time"
)

// ---- views / requests ----

type MFAEnrollment struct {
	Secret     string `json:"secret"`
	OTPAuthURL string `json:"otpauth_url"`
}
type MFAConfirmRequest struct {
	Code string `json:"code"`
}
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}
type ResetTokenView struct {
	UserID     string `json:"user_id"`
	ResetToken string `json:"reset_token"` // shown once; only its hash is stored
	ExpiresAt  string `json:"expires_at"`
}
type CompleteResetRequest struct {
	ResetToken  string `json:"reset_token"`
	NewPassword string `json:"new_password"`
}
type SetActiveRequest struct {
	Active bool `json:"active"`
}
type SetRolesRequest struct {
	Roles []string `json:"roles"`
}

// ActorID resolves a token to a user ID for the audit log. It does not enforce
// the MFA gate: auditing must still name a user who is not yet enrolled.
func (s *Service) ActorID(token string) string {
	if token == "" {
		return ""
	}
	id, _ := StoreReadScoped(s.store, Scope{Tokens: []string{token}}, func(state *AppState) (string, error) {
		u, err := actorFromTokenNoMFA(state, token)
		if err != nil {
			return "", nil
		}
		return u.ID, nil
	})
	return id
}

// ---- MFA ----

// EnrollMFA starts TOTP enrolment: it stores a pending secret that only
// becomes active after ConfirmMFA proves the authenticator works.
func (s *Service) EnrollMFA(token string) (MFAEnrollment, error) {
	return StoreUpdateScoped(s.store, Scope{Tokens: []string{token}}, func(state *AppState) (MFAEnrollment, error) {
		actor, err := actorFromTokenNoMFA(state, token)
		if err != nil {
			return MFAEnrollment{}, err
		}
		if actor.MFAEnabled {
			return MFAEnrollment{}, conflict("MFA is already enabled; ask an administrator to reset it first")
		}
		secret, err := newTOTPSecret()
		if err != nil {
			return MFAEnrollment{}, internalErr(err.Error())
		}
		actor.MFAPendingSecret = secret
		return MFAEnrollment{Secret: secret, OTPAuthURL: otpauthURL(actor.Username, secret)}, nil
	})
}

func (s *Service) ConfirmMFA(token string, req MFAConfirmRequest) (UserView, error) {
	return StoreUpdateScoped(s.store, Scope{Tokens: []string{token}}, func(state *AppState) (UserView, error) {
		actor, err := actorFromTokenNoMFA(state, token)
		if err != nil {
			return UserView{}, err
		}
		if actor.MFAPendingSecret == "" {
			return UserView{}, conflict("no MFA enrolment in progress")
		}
		step := verifyTOTP(actor.MFAPendingSecret, req.Code, 0)
		if step == 0 {
			return UserView{}, badRequest("invalid code")
		}
		actor.MFASecret, actor.MFAPendingSecret, actor.MFAEnabled, actor.MFALastStep = actor.MFAPendingSecret, "", true, step
		return toUserView(actor), nil
	})
}

// ---- administration of other users ----

// authorizeUserAdmin decides whether actor may administer target (reset
// password/MFA, deactivate, change roles). Self-service goes through the
// dedicated endpoints, so an admin cannot lock themselves out or edit their
// own privileges. Because only a super_admin can touch a super_admin and
// nobody can touch themselves, at least one active super_admin always remains.
func authorizeUserAdmin(actor, target *User) error {
	if target == nil {
		return notFound("user not found")
	}
	if actor.ID == target.ID {
		return forbidden("administrators cannot modify their own account here")
	}
	if hasRole(target, "super_admin") && !hasRole(actor, "super_admin") {
		return forbidden("only super_admin can administer a super_admin")
	}
	if !canManageColony(actor, target.ColonyID) {
		return forbidden("not allowed to administer this user")
	}
	return nil
}

func dropSessions(state *AppState, userID, except string) {
	for tok, sess := range state.Sessions {
		if sess.UserID == userID && tok != except {
			delete(state.Sessions, tok)
		}
	}
}

// IssueResetToken creates a one-time password-reset secret for another user.
// There is no e-mail channel in this system, so the administrator hands it over
// out of band; it is valid for resetTokenTTL and only its hash is stored.
func (s *Service) IssueResetToken(token, userID string) (ResetTokenView, error) {
	return StoreUpdateScoped(s.store, Scope{Tokens: []string{token}, Users: []string{userID}}, func(state *AppState) (ResetTokenView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return ResetTokenView{}, err
		}
		target := state.Users[userID]
		if err := authorizeUserAdmin(actor, target); err != nil {
			return ResetTokenView{}, err
		}
		tok, hash, err := newResetToken(target.ID)
		if err != nil {
			return ResetTokenView{}, internalErr(err.Error())
		}
		exp := lockClock().UTC().Add(resetTokenTTL).Format(time.RFC3339)
		target.ResetTokenHash, target.ResetExpiresAt = hash, exp
		return ResetTokenView{UserID: target.ID, ResetToken: tok, ExpiresAt: exp}, nil
	})
}

// CompleteReset consumes a reset token and sets a new password. All of the
// user's sessions are revoked and any lockout is cleared; MFA stays enrolled.
func (s *Service) CompleteReset(req CompleteResetRequest) error {
	if err := validNewPassword(req.NewPassword); err != nil {
		return err
	}
	bad := func(reason string) error {
		e := badRequest("invalid or expired reset token")
		e.Reason = reason
		return e
	}
	userID, hash, ok := splitResetToken(req.ResetToken)
	if !ok {
		return bad("malformed")
	}
	valid := func(u *User) bool {
		if u == nil || !u.Active || u.ResetTokenHash == "" || subtle.ConstantTimeCompare([]byte(u.ResetTokenHash), []byte(hash)) != 1 {
			return false
		}
		exp, err := time.Parse(time.RFC3339, u.ResetExpiresAt)
		return err == nil && lockClock().Before(exp)
	}
	// Check before hashing so unauthenticated callers cannot burn CPU.
	okToken, _ := StoreReadScoped(s.store, Scope{Users: []string{userID}}, func(state *AppState) (bool, error) { return valid(state.Users[userID]), nil })
	if !okToken {
		return bad("unknown_or_expired")
	}
	newHash, err := hashPasswordArgon2id(req.NewPassword)
	if err != nil {
		return internalErr(err.Error())
	}
	_, err = StoreUpdateScoped(s.store, Scope{Users: []string{userID}, UserSessions: []string{userID}}, func(state *AppState) (struct{}, error) {
		u := state.Users[userID]
		if !valid(u) { // single use, even under concurrent submissions
			return struct{}{}, bad("consumed_or_expired")
		}
		u.PasswordHash, u.PasswordSalt = newHash, ""
		u.ResetTokenHash, u.ResetExpiresAt = "", ""
		u.FailedLogins, u.LockedUntil = 0, ""
		u.PasswordChangedAt = nowUTC()
		dropSessions(state, userID, "")
		return struct{}{}, nil
	})
	return err
}

// ChangePassword is the self-service path: it needs the current password and
// signs out every other session.
func (s *Service) ChangePassword(token string, req ChangePasswordRequest) error {
	if err := validNewPassword(req.NewPassword); err != nil {
		return err
	}
	type cur struct{ id, salt, hash string }
	c, err := StoreReadScoped(s.store, Scope{Tokens: []string{token}}, func(state *AppState) (cur, error) {
		u, err := actorFromTokenNoMFA(state, token)
		if err != nil {
			return cur{}, err
		}
		if isLocked(u) {
			return cur{}, invalidCreds("locked")
		}
		return cur{u.ID, u.PasswordSalt, u.PasswordHash}, nil
	})
	if err != nil {
		return err
	}
	if ok, _ := verifyPassword(req.CurrentPassword, c.salt, c.hash); !ok {
		s.noteLoginFailure(c.id) // password guessing through this endpoint counts too
		return forbidden("current password is incorrect")
	}
	newHash, err := hashPasswordArgon2id(req.NewPassword)
	if err != nil {
		return internalErr(err.Error())
	}
	_, err = StoreUpdateScoped(s.store, Scope{Tokens: []string{token}, UserSessions: []string{c.id}}, func(state *AppState) (struct{}, error) {
		u, err := actorFromTokenNoMFA(state, token)
		if err != nil {
			return struct{}{}, err
		}
		if u.PasswordHash != c.hash {
			return struct{}{}, conflict("password changed concurrently; retry")
		}
		u.PasswordHash, u.PasswordSalt, u.PasswordChangedAt = newHash, "", nowUTC()
		dropSessions(state, u.ID, token)
		return struct{}{}, nil
	})
	return err
}

// ResetMFA removes a user's authenticator (lost device). The user must enrol again.
func (s *Service) ResetMFA(token, userID string) (UserView, error) {
	return StoreUpdateScoped(s.store, Scope{Tokens: []string{token}, Users: []string{userID}, UserSessions: []string{userID}}, func(state *AppState) (UserView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return UserView{}, err
		}
		target := state.Users[userID]
		if err := authorizeUserAdmin(actor, target); err != nil {
			return UserView{}, err
		}
		target.MFAEnabled, target.MFASecret, target.MFAPendingSecret, target.MFALastStep = false, "", "", 0
		dropSessions(state, target.ID, "")
		return toUserView(target), nil
	})
}

// SetUserActive deactivates or reactivates a user. Deactivation revokes every
// session at once; the user's data and account stay (nothing is deleted).
func (s *Service) SetUserActive(token, userID string, active bool) (UserView, error) {
	return StoreUpdateScoped(s.store, Scope{Tokens: []string{token}, Users: []string{userID}, UserSessions: []string{userID}, UserColonies: true}, func(state *AppState) (UserView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return UserView{}, err
		}
		target := state.Users[userID]
		if err := authorizeUserAdmin(actor, target); err != nil {
			return UserView{}, err
		}
		if target.Active == active {
			return toUserView(target), nil
		}
		target.Active = active
		if !active {
			dropSessions(state, target.ID, "")
		} else {
			target.FailedLogins, target.LockedUntil = 0, ""
		}
		typ := "user_deactivated"
		if active {
			typ = "user_reactivated"
		}
		if colony := state.Colonies[target.ColonyID]; colony != nil {
			if err := appendLedger(colony, typ, actor.ID, map[string]any{"user_id": target.ID}); err != nil {
				return UserView{}, internalErr(err.Error())
			}
		}
		return toUserView(target), nil
	})
}

// SetUserRoles replaces a user's roles.
func (s *Service) SetUserRoles(token, userID string, req SetRolesRequest) (UserView, error) {
	roles := normalizeRoles(req.Roles)
	if len(roles) == 0 {
		return UserView{}, badRequest("at least one valid role is required")
	}
	return StoreUpdateScoped(s.store, Scope{Tokens: []string{token}, Users: []string{userID}, UserColonies: true}, func(state *AppState) (UserView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return UserView{}, err
		}
		target := state.Users[userID]
		if err := authorizeUserAdmin(actor, target); err != nil {
			return UserView{}, err
		}
		if containsRole(roles, "super_admin") && !hasRole(actor, "super_admin") {
			return UserView{}, forbidden("only super_admin can assign super_admin")
		}
		before := append([]string{}, target.Roles...)
		target.Roles = roles
		if colony := state.Colonies[target.ColonyID]; colony != nil {
			if err := appendLedger(colony, "user_roles_changed", actor.ID, map[string]any{"user_id": target.ID, "from": before, "to": roles}); err != nil {
				return UserView{}, internalErr(err.Error())
			}
		}
		return toUserView(target), nil
	})
}
