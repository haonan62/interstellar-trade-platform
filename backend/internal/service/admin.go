package service

import (
	"strings"

	"interstellar-trade-platform/backend/internal/domain"
	"interstellar-trade-platform/backend/internal/storage"
)

func (s *Service) CreateColony(token string, req CreateColonyRequest) (domain.ColonyView, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return domain.ColonyView{}, domain.BadRequest("colony name is required")
	}
	return storage.StoreUpdateScoped(s.store, storage.Scope{Tokens: []string{token}}, func(state *domain.AppState) (domain.ColonyView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return domain.ColonyView{}, err
		}
		if !domain.HasRole(actor, "super_admin") {
			return domain.ColonyView{}, domain.Forbidden("only super_admin can create colonies")
		}
		for _, c := range state.Colonies {
			if strings.EqualFold(c.Name, name) {
				return domain.ColonyView{}, domain.Conflict("colony name already exists")
			}
		}
		colonyID, err := domain.NewID("col")
		if err != nil {
			return domain.ColonyView{}, domain.InternalErr(err.Error())
		}
		pub, priv, err := generateEd25519Keypair()
		if err != nil {
			return domain.ColonyView{}, domain.InternalErr(err.Error())
		}
		colony := &domain.Colony{ID: colonyID, Name: name, PublicKey: pub, PrivateKey: priv, CreatedAt: domain.NowUTC(), TrustedColonies: map[string]string{colonyID: pub}, Accounts: map[string]*domain.Account{}, ProcessedEnvelopes: map[string]bool{}, Outbox: []*domain.Envelope{}, Ledger: []*domain.LedgerEntry{}, NetObligations: map[string]int64{}, NetClaims: map[string]int64{}}
		state.Colonies[colony.ID] = colony
		if err := appendLedger(colony, "colony_created", actor.ID, map[string]any{"colony_id": colony.ID, "name": colony.Name}); err != nil {
			return domain.ColonyView{}, domain.InternalErr(err.Error())
		}
		return toColonyView(colony), nil
	})
}

func (s *Service) TrustPeer(token, colonyID string, req TrustPeerRequest) (domain.ColonyView, error) {
	if strings.TrimSpace(req.PeerColonyID) == "" {
		return domain.ColonyView{}, domain.BadRequest("peer_colony_id is required")
	}
	return storage.StoreUpdateScoped(s.store, storage.Scope{Tokens: []string{token}, Colonies: []string{colonyID, req.PeerColonyID}}, func(state *domain.AppState) (domain.ColonyView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return domain.ColonyView{}, err
		}
		colony := state.Colonies[colonyID]
		if colony == nil {
			return domain.ColonyView{}, domain.NotFound("colony not found")
		}
		peer := state.Colonies[req.PeerColonyID]
		if peer == nil {
			return domain.ColonyView{}, domain.NotFound("peer colony not found")
		}
		if !canManageColony(actor, colonyID) {
			return domain.ColonyView{}, domain.Forbidden("not allowed to trust peers for this colony")
		}
		colony.TrustedColonies[peer.ID] = peer.PublicKey
		if err := appendLedger(colony, "peer_trusted", actor.ID, map[string]any{"peer_colony_id": peer.ID}); err != nil {
			return domain.ColonyView{}, domain.InternalErr(err.Error())
		}
		return toColonyView(colony), nil
	})
}

func (s *Service) CreateUser(token string, req CreateUserRequest) (domain.UserView, error) {
	username := NormalizeUsername(req.Username)
	if username == "" || len(req.Password) < 8 || len(req.Password) > domain.MaxPasswordLen {
		return domain.UserView{}, domain.BadRequest("username and password of at least 8 characters are required")
	}
	roles := domain.NormalizeRoles(req.Roles)
	if len(roles) == 0 {
		roles = []string{"trader"}
	}
	// Authorise BEFORE hashing so unauthorised callers cannot burn CPU, and hash
	// BEFORE taking the write lock so other requests are never blocked by it.
	if _, err := storage.StoreReadScoped(s.store, storage.Scope{Tokens: []string{token}, Colonies: []string{req.ColonyID}}, func(state *domain.AppState) (struct{}, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return struct{}{}, err
		}
		if state.Colonies[req.ColonyID] != nil && !canManageColony(actor, req.ColonyID) {
			return struct{}{}, domain.Forbidden("not allowed to create users in this colony")
		}
		return struct{}{}, nil
	}); err != nil {
		return domain.UserView{}, err
	}
	passwordHash, err := domain.HashPasswordArgon2id(req.Password)
	if err != nil {
		return domain.UserView{}, domain.InternalErr(err.Error())
	}
	return storage.StoreUpdateScoped(s.store, storage.Scope{Tokens: []string{token}, Colonies: []string{req.ColonyID}, Usernames: []string{username}}, func(state *domain.AppState) (domain.UserView, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return domain.UserView{}, err
		}
		if userExistsByUsername(state, username) {
			return domain.UserView{}, domain.Conflict("username already exists")
		}
		colony := state.Colonies[req.ColonyID]
		if colony == nil {
			return domain.UserView{}, domain.NotFound("colony not found")
		}
		if !canManageColony(actor, req.ColonyID) {
			return domain.UserView{}, domain.Forbidden("not allowed to create users in this colony")
		}
		if containsRole(roles, "super_admin") && !domain.HasRole(actor, "super_admin") {
			return domain.UserView{}, domain.Forbidden("only super_admin can assign super_admin")
		}
		display := strings.TrimSpace(req.DisplayName)
		if display == "" {
			display = username
		}
		user, err := newUser(username, display, passwordHash, req.ColonyID, roles)
		if err != nil {
			return domain.UserView{}, err
		}
		state.Users[user.ID] = user
		colony.Accounts[user.ID] = &domain.Account{UserID: user.ID, ColonyID: req.ColonyID, Balance: 0}
		if err := appendLedger(colony, "user_created", actor.ID, map[string]any{"user_id": user.ID, "username": user.Username, "roles": user.Roles}); err != nil {
			return domain.UserView{}, domain.InternalErr(err.Error())
		}
		return toUserView(user), nil
	})
}

// MintOutcome is either an applied mint (Account) or a dual-control proposal.
type MintOutcome struct {
	Account *domain.AccountView   `json:"account,omitempty"`
	Pending *domain.PendingAction `json:"pending,omitempty"`
}

// Mint applies a mint immediately. It fails for colonies that require dual
// control; use MintOrPropose for those.
func (s *Service) Mint(token string, req MintRequest) (domain.AccountView, error) {
	out, err := s.mint(token, req, false)
	if err != nil {
		return domain.AccountView{}, err
	}
	return *out.Account, nil
}

// MintOrPropose mints immediately, or records a pending action when the colony
// requires dual control.
func (s *Service) MintOrPropose(token string, req MintRequest) (MintOutcome, error) {
	return s.mint(token, req, true)
}

func (s *Service) mint(token string, req MintRequest, allowPropose bool) (MintOutcome, error) {
	if !domain.ValidAmount(req.Amount) {
		return MintOutcome{}, domain.BadRequest("amount must be positive and at most the per-mint limit")
	}
	return storage.StoreUpdateScoped(s.store, storage.Scope{Tokens: []string{token}, Colonies: []string{req.ColonyID}, Users: []string{req.UserID}}, func(state *domain.AppState) (MintOutcome, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return MintOutcome{}, err
		}
		if !canManageColony(actor, req.ColonyID) {
			return MintOutcome{}, domain.Forbidden("not allowed to mint in this colony")
		}
		colony := state.Colonies[req.ColonyID]
		if colony == nil {
			return MintOutcome{}, domain.NotFound("colony not found")
		}
		if colony.Accounts[req.UserID] == nil {
			return MintOutcome{}, domain.NotFound("account not found")
		}
		if state.Users[req.UserID] == nil {
			return MintOutcome{}, domain.NotFound("user not found")
		}
		if colony.RequireDualControl {
			if !allowPropose {
				return MintOutcome{}, domain.Conflict("colony requires dual control; propose the mint via the approvals flow")
			}
			// Fail early if the mint could never be applied.
			if _, err := domain.AddChecked(colony.MintedTotal, req.Amount); err != nil || colony.MintedTotal+req.Amount > domain.EffectiveMintCap(colony) {
				return MintOutcome{}, domain.Conflict("mint cap exceeded")
			}
			pa, err := newPendingAction(state, actor, "mint", colony.ID)
			if err != nil {
				return MintOutcome{}, err
			}
			pa.UserID, pa.Amount = req.UserID, req.Amount
			if err := appendLedger(colony, "mint_proposed", actor.ID, map[string]any{"action_id": pa.ID, "user_id": req.UserID, "amount": req.Amount}); err != nil {
				return MintOutcome{}, domain.InternalErr(err.Error())
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
func applyMint(state *domain.AppState, colony *domain.Colony, userID string, amount int64, actorID, approvedBy string) (domain.AccountView, error) {
	acc, user := colony.Accounts[userID], state.Users[userID]
	if acc == nil || user == nil {
		return domain.AccountView{}, domain.NotFound("account not found")
	}
	newTotal, err := domain.AddChecked(colony.MintedTotal, amount)
	if err != nil {
		return domain.AccountView{}, err
	}
	if newTotal > domain.EffectiveMintCap(colony) {
		return domain.AccountView{}, domain.Conflict("mint cap exceeded")
	}
	newBalance, err := domain.AddChecked(acc.Balance, amount)
	if err != nil {
		return domain.AccountView{}, err
	}
	colony.MintedTotal = newTotal
	acc.Balance = newBalance
	payload := map[string]any{"user_id": user.ID, "amount": amount, "new_balance": acc.Balance, "minted_total": newTotal}
	if approvedBy != "" {
		payload["approved_by"] = approvedBy
	}
	if err := appendLedger(colony, "funds_minted", actorID, payload); err != nil {
		return domain.AccountView{}, domain.InternalErr(err.Error())
	}
	return domain.AccountView{UserID: user.ID, Username: user.Username, DisplayName: user.DisplayName, ColonyID: colony.ID, ColonyName: colony.Name, Balance: acc.Balance}, nil
}
