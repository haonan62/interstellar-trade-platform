package main

import "strings"

func (s *Service) CreateColony(token string, req CreateColonyRequest) (ColonyView, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return ColonyView{}, badRequest("colony name is required")
	}
	return StoreUpdateScoped(s.store, Scope{Tokens: []string{token}}, func(state *AppState) (ColonyView, error) {
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
	return StoreUpdateScoped(s.store, Scope{Tokens: []string{token}, Colonies: []string{colonyID, req.PeerColonyID}}, func(state *AppState) (ColonyView, error) {
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
	if username == "" || len(req.Password) < 8 || len(req.Password) > maxPasswordLen {
		return UserView{}, badRequest("username and password of at least 8 characters are required")
	}
	roles := normalizeRoles(req.Roles)
	if len(roles) == 0 {
		roles = []string{"trader"}
	}
	// Authorise BEFORE hashing so unauthorised callers cannot burn CPU, and hash
	// BEFORE taking the write lock so other requests are never blocked by it.
	if _, err := StoreReadScoped(s.store, Scope{Tokens: []string{token}, Colonies: []string{req.ColonyID}}, func(state *AppState) (struct{}, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return struct{}{}, err
		}
		if state.Colonies[req.ColonyID] != nil && !canManageColony(actor, req.ColonyID) {
			return struct{}{}, forbidden("not allowed to create users in this colony")
		}
		return struct{}{}, nil
	}); err != nil {
		return UserView{}, err
	}
	passwordHash, err := hashPasswordArgon2id(req.Password)
	if err != nil {
		return UserView{}, internalErr(err.Error())
	}
	return StoreUpdateScoped(s.store, Scope{Tokens: []string{token}, Colonies: []string{req.ColonyID}, Usernames: []string{username}}, func(state *AppState) (UserView, error) {
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
		user, err := newUser(username, display, passwordHash, req.ColonyID, roles)
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

// MintOutcome is either an applied mint (Account) or a dual-control proposal.
type MintOutcome struct {
	Account *AccountView   `json:"account,omitempty"`
	Pending *PendingAction `json:"pending,omitempty"`
}

// Mint applies a mint immediately. It fails for colonies that require dual
// control; use MintOrPropose for those.
func (s *Service) Mint(token string, req MintRequest) (AccountView, error) {
	out, err := s.mint(token, req, false)
	if err != nil {
		return AccountView{}, err
	}
	return *out.Account, nil
}

// MintOrPropose mints immediately, or records a pending action when the colony
// requires dual control.
func (s *Service) MintOrPropose(token string, req MintRequest) (MintOutcome, error) {
	return s.mint(token, req, true)
}

func (s *Service) mint(token string, req MintRequest, allowPropose bool) (MintOutcome, error) {
	if !validAmount(req.Amount) {
		return MintOutcome{}, badRequest("amount must be positive and at most the per-mint limit")
	}
	return StoreUpdateScoped(s.store, Scope{Tokens: []string{token}, Colonies: []string{req.ColonyID}, Users: []string{req.UserID}}, func(state *AppState) (MintOutcome, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return MintOutcome{}, err
		}
		if !canManageColony(actor, req.ColonyID) {
			return MintOutcome{}, forbidden("not allowed to mint in this colony")
		}
		colony := state.Colonies[req.ColonyID]
		if colony == nil {
			return MintOutcome{}, notFound("colony not found")
		}
		if colony.Accounts[req.UserID] == nil {
			return MintOutcome{}, notFound("account not found")
		}
		if state.Users[req.UserID] == nil {
			return MintOutcome{}, notFound("user not found")
		}
		if colony.RequireDualControl {
			if !allowPropose {
				return MintOutcome{}, conflict("colony requires dual control; propose the mint via the approvals flow")
			}
			// Fail early if the mint could never be applied.
			if _, err := addChecked(colony.MintedTotal, req.Amount); err != nil || colony.MintedTotal+req.Amount > effectiveMintCap(colony) {
				return MintOutcome{}, conflict("mint cap exceeded")
			}
			pa, err := newPendingAction(state, actor, "mint", colony.ID)
			if err != nil {
				return MintOutcome{}, err
			}
			pa.UserID, pa.Amount = req.UserID, req.Amount
			if err := appendLedger(colony, "mint_proposed", actor.ID, map[string]any{"action_id": pa.ID, "user_id": req.UserID, "amount": req.Amount}); err != nil {
				return MintOutcome{}, internalErr(err.Error())
			}
			return MintOutcome{Pending: pa}, nil
		}
		view, err := applyMint(state, colony, req.UserID, req.Amount, actor.ID, "")
		if err != nil {
			return MintOutcome{}, err
		}
		return MintOutcome{Account: &view}, nil
	})
}

// applyMint is the only place supply is created. It enforces the per-colony
// cap and checked arithmetic.
func applyMint(state *AppState, colony *Colony, userID string, amount int64, actorID, approvedBy string) (AccountView, error) {
	acc, user := colony.Accounts[userID], state.Users[userID]
	if acc == nil || user == nil {
		return AccountView{}, notFound("account not found")
	}
	newTotal, err := addChecked(colony.MintedTotal, amount)
	if err != nil {
		return AccountView{}, err
	}
	if newTotal > effectiveMintCap(colony) {
		return AccountView{}, conflict("mint cap exceeded")
	}
	newBalance, err := addChecked(acc.Balance, amount)
	if err != nil {
		return AccountView{}, err
	}
	colony.MintedTotal = newTotal
	acc.Balance = newBalance
	payload := map[string]any{"user_id": user.ID, "amount": amount, "new_balance": acc.Balance, "minted_total": newTotal}
	if approvedBy != "" {
		payload["approved_by"] = approvedBy
	}
	if err := appendLedger(colony, "funds_minted", actorID, payload); err != nil {
		return AccountView{}, internalErr(err.Error())
	}
	return AccountView{UserID: user.ID, Username: user.Username, DisplayName: user.DisplayName, ColonyID: colony.ID, ColonyName: colony.Name, Balance: acc.Balance}, nil
}
