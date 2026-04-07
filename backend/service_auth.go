package main

import "strings"

func (s *Service) NeedsBootstrap() (bool, error) {
	return StoreRead(s.store, func(state *AppState) (bool, error) { return len(state.Users) == 0, nil })
}

func (s *Service) Bootstrap(req BootstrapRequest) (AuthResponse, error) {
	username := normalizeUsername(req.Username)
	if username == "" || len(req.Password) < 8 {
		return AuthResponse{}, badRequest("username and password of at least 8 characters are required")
	}
	display := strings.TrimSpace(req.DisplayName)
	if display == "" {
		display = username
	}
	return StoreUpdate(s.store, func(state *AppState) (AuthResponse, error) {
		if len(state.Users) > 0 {
			return AuthResponse{}, conflict("bootstrap has already been completed")
		}
		if userExistsByUsername(state, username) {
			return AuthResponse{}, conflict("username already exists")
		}
		user, err := newUser(username, display, req.Password, "", []string{"super_admin"})
		if err != nil {
			return AuthResponse{}, err
		}
		state.Users[user.ID] = user
		return createSessionForUser(state, user)
	})
}

func (s *Service) Login(req LoginRequest) (AuthResponse, error) {
	username := normalizeUsername(req.Username)
	if username == "" || req.Password == "" {
		return AuthResponse{}, badRequest("username and password are required")
	}
	return StoreUpdate(s.store, func(state *AppState) (AuthResponse, error) {
		user := findUserByUsername(state, username)
		if user == nil || !user.Active || !verifyPassword(req.Password, user.PasswordSalt, user.PasswordHash) {
			return AuthResponse{}, unauthorized("invalid credentials")
		}
		return createSessionForUser(state, user)
	})
}

func (s *Service) Logout(token string) error {
	_, err := StoreUpdate(s.store, func(state *AppState) (struct{}, error) { delete(state.Sessions, token); return struct{}{}, nil })
	return err
}
func (s *Service) Me(token string) (UserView, error) {
	return StoreRead(s.store, func(state *AppState) (UserView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return UserView{}, err
		}
		return toUserView(actor), nil
	})
}
