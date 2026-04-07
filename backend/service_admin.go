package main

import "strings"

func (s *Service) CreateColony(token string, req CreateColonyRequest) (ColonyView, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return ColonyView{}, badRequest("colony name is required")
	}
	return StoreUpdate(s.store, func(state *AppState) (ColonyView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return ColonyView{}, err
		}
		if !hasRole(actor, "super_admin") {
			return ColonyView{}, forbidden("only super_admin can create colonies")
		}
		for _, c := range state.Colonies {
			if strings.EqualFold(c.Name, name) {
				return ColonyView{}, conflict("colony name already exists")
			}
		}
		colonyID, err := newID("col")
		if err != nil {
			return ColonyView{}, internalErr(err.Error())
		}
		pub, priv, err := generateEd25519Keypair()
		if err != nil {
			return ColonyView{}, internalErr(err.Error())
		}
		colony := &Colony{ID: colonyID, Name: name, PublicKey: pub, PrivateKey: priv, CreatedAt: nowUTC(), TrustedColonies: map[string]string{colonyID: pub}, Accounts: map[string]*Account{}, ProcessedEnvelopes: map[string]bool{}, Outbox: []*Envelope{}, Ledger: []*LedgerEntry{}, NetObligations: map[string]int64{}, NetClaims: map[string]int64{}}
		state.Colonies[colony.ID] = colony
		if err := appendLedger(colony, "colony_created", actor.ID, map[string]any{"colony_id": colony.ID, "name": colony.Name}); err != nil {
			return ColonyView{}, internalErr(err.Error())
		}
		return toColonyView(colony), nil
	})
}

func (s *Service) TrustPeer(token, colonyID string, req TrustPeerRequest) (ColonyView, error) {
	if strings.TrimSpace(req.PeerColonyID) == "" {
		return ColonyView{}, badRequest("peer_colony_id is required")
	}
	return StoreUpdate(s.store, func(state *AppState) (ColonyView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return ColonyView{}, err
		}
		colony := state.Colonies[colonyID]
		if colony == nil {
			return ColonyView{}, notFound("colony not found")
		}
		peer := state.Colonies[req.PeerColonyID]
		if peer == nil {
			return ColonyView{}, notFound("peer colony not found")
		}
		if !canManageColony(actor, colonyID) {
			return ColonyView{}, forbidden("not allowed to trust peers for this colony")
		}
		colony.TrustedColonies[peer.ID] = peer.PublicKey
		if err := appendLedger(colony, "peer_trusted", actor.ID, map[string]any{"peer_colony_id": peer.ID}); err != nil {
			return ColonyView{}, internalErr(err.Error())
		}
		return toColonyView(colony), nil
	})
}

func (s *Service) CreateUser(token string, req CreateUserRequest) (UserView, error) {
	username := normalizeUsername(req.Username)
	if username == "" || len(req.Password) < 8 {
		return UserView{}, badRequest("username and password of at least 8 characters are required")
	}
	roles := normalizeRoles(req.Roles)
	if len(roles) == 0 {
		roles = []string{"trader"}
	}
	return StoreUpdate(s.store, func(state *AppState) (UserView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return UserView{}, err
		}
		if userExistsByUsername(state, username) {
			return UserView{}, conflict("username already exists")
		}
		colony := state.Colonies[req.ColonyID]
		if colony == nil {
			return UserView{}, notFound("colony not found")
		}
		if !canManageColony(actor, req.ColonyID) {
			return UserView{}, forbidden("not allowed to create users in this colony")
		}
		if containsRole(roles, "super_admin") && !hasRole(actor, "super_admin") {
			return UserView{}, forbidden("only super_admin can assign super_admin")
		}
		display := strings.TrimSpace(req.DisplayName)
		if display == "" {
			display = username
		}
		user, err := newUser(username, display, req.Password, req.ColonyID, roles)
		if err != nil {
			return UserView{}, err
		}
		state.Users[user.ID] = user
		colony.Accounts[user.ID] = &Account{UserID: user.ID, ColonyID: req.ColonyID, Balance: 0}
		if err := appendLedger(colony, "user_created", actor.ID, map[string]any{"user_id": user.ID, "username": user.Username, "roles": user.Roles}); err != nil {
			return UserView{}, internalErr(err.Error())
		}
		return toUserView(user), nil
	})
}

func (s *Service) Mint(token string, req MintRequest) (AccountView, error) {
	if req.Amount <= 0 {
		return AccountView{}, badRequest("amount must be positive")
	}
	return StoreUpdate(s.store, func(state *AppState) (AccountView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return AccountView{}, err
		}
		if !canManageColony(actor, req.ColonyID) {
			return AccountView{}, forbidden("not allowed to mint in this colony")
		}
		colony := state.Colonies[req.ColonyID]
		if colony == nil {
			return AccountView{}, notFound("colony not found")
		}
		acc := colony.Accounts[req.UserID]
		if acc == nil {
			return AccountView{}, notFound("account not found")
		}
		user := state.Users[req.UserID]
		if user == nil {
			return AccountView{}, notFound("user not found")
		}
		acc.Balance += req.Amount
		if err := appendLedger(colony, "funds_minted", actor.ID, map[string]any{"user_id": user.ID, "amount": req.Amount, "new_balance": acc.Balance}); err != nil {
			return AccountView{}, internalErr(err.Error())
		}
		return AccountView{UserID: user.ID, Username: user.Username, DisplayName: user.DisplayName, ColonyID: colony.ID, ColonyName: colony.Name, Balance: acc.Balance}, nil
	})
}
