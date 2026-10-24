package service

import (
	"sort"
	"time"

	"interstellar-trade-platform/backend/internal/domain"
	"interstellar-trade-platform/backend/internal/storage"
)

// Dual control: some admin actions are only proposed by one admin and take
// effect when a DIFFERENT admin with authority over the colony approves.
//
// Always dual-control: force_refund (it bypasses the seller colony's
// authority, so a second human must confirm out-of-band that the seller did
// not credit). Optional per colony: mint, and policy changes while enabled.

func newPendingAction(state *domain.AppState, proposer *domain.User, typ, colonyID string) (*domain.PendingAction, error) {
	id, err := domain.NewID("act")
	if err != nil {
		return nil, domain.InternalErr(err.Error())
	}
	now := domain.NowFn().UTC()
	pa := &domain.PendingAction{ID: id, Type: typ, ColonyID: colonyID, ProposedBy: proposer.ID, ProposedAt: now.Format(time.RFC3339), ExpiresAt: now.Add(PendingActionTTL).Format(time.RFC3339), Status: "pending"}
	state.PendingActions[id] = pa
	return pa, nil
}

type PolicyRequest struct {
	MintCap            *int64 `json:"mint_cap,omitempty"`
	RequireDualControl *bool  `json:"require_dual_control,omitempty"`
}

// SetPolicy changes a colony's supply policy. super_admin only. While dual
// control is enabled the change is only proposed.
func (s *Service) SetPolicy(token, colonyID string, req PolicyRequest) (PolicyOutcome, error) {
	if req.MintCap == nil && req.RequireDualControl == nil {
		return PolicyOutcome{}, domain.BadRequest("mint_cap or require_dual_control is required")
	}
	return storage.StoreUpdateScoped(s.store, storage.Scope{Tokens: []string{token}, Colonies: []string{colonyID}}, func(state *domain.AppState) (PolicyOutcome, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return PolicyOutcome{}, err
		}
		if !domain.HasRole(actor, "super_admin") {
			return PolicyOutcome{}, domain.Forbidden("only super_admin can change supply policy")
		}
		colony := state.Colonies[colonyID]
		if colony == nil {
			return PolicyOutcome{}, domain.NotFound("colony not found")
		}
		if err := validatePolicy(colony, req); err != nil {
			return PolicyOutcome{}, err
		}
		if colony.RequireDualControl {
			pa, err := newPendingAction(state, actor, "policy", colonyID)
			if err != nil {
				return PolicyOutcome{}, err
			}
			pa.MintCap, pa.RequireDual = req.MintCap, req.RequireDualControl
			if err := appendLedger(colony, "policy_proposed", actor.ID, map[string]any{"action_id": pa.ID}); err != nil {
				return PolicyOutcome{}, domain.InternalErr(err.Error())
			}
			return PolicyOutcome{Pending: pa}, nil
		}
		if err := applyPolicy(colony, req, actor.ID, ""); err != nil {
			return PolicyOutcome{}, err
		}
		view := toColonyView(colony)
		return PolicyOutcome{Colony: &view}, nil
	})
}

// PolicyOutcome is the result of a policy change: applied or proposed.
type PolicyOutcome struct {
	Colony  *domain.ColonyView    `json:"colony,omitempty"`
	Pending *domain.PendingAction `json:"pending,omitempty"`
}

func validatePolicy(colony *domain.Colony, req PolicyRequest) error {
	if req.MintCap != nil && (*req.MintCap <= 0 || *req.MintCap < colony.MintedTotal || *req.MintCap > domain.DefaultMintCap) {
		return domain.BadRequest("mint_cap must be positive, at least the amount already minted, and at most the platform maximum")
	}
	return nil
}

func applyPolicy(colony *domain.Colony, req PolicyRequest, actorID, approvedBy string) error {
	if err := validatePolicy(colony, req); err != nil {
		return err
	}
	if req.MintCap != nil {
		colony.MintCap = *req.MintCap
	}
	if req.RequireDualControl != nil {
		colony.RequireDualControl = *req.RequireDualControl
	}
	payload := map[string]any{"mint_cap": domain.EffectiveMintCap(colony), "require_dual_control": colony.RequireDualControl}
	if approvedBy != "" {
		payload["approved_by"] = approvedBy
	}
	if err := appendLedger(colony, "policy_changed", actorID, payload); err != nil {
		return domain.InternalErr(err.Error())
	}
	return nil
}

// ProposeForceRefund proposes refunding an escrowed buyer without the seller
// colony's confirmation. Risk (documented): if the seller colony really did
// credit, the refund creates money. The approver must confirm out-of-band.
func (s *Service) ProposeForceRefund(token, tradeID string) (domain.PendingAction, error) {
	return storage.StoreUpdateScoped(s.store, storage.Scope{Tokens: []string{token}, Trades: []string{tradeID}}, func(state *domain.AppState) (domain.PendingAction, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return domain.PendingAction{}, err
		}
		trade := state.Trades[tradeID]
		if trade == nil {
			return domain.PendingAction{}, domain.NotFound("trade not found")
		}
		if !canManageColony(actor, trade.BuyerColonyID) {
			return domain.PendingAction{}, domain.Forbidden("only an admin of the buyer colony can propose a force refund")
		}
		if !domain.IsEscrowed(trade.Status) {
			return domain.PendingAction{}, domain.Conflict("trade is not in escrow (" + trade.Status + ")")
		}
		pa, err := newPendingAction(state, actor, "force_refund", trade.BuyerColonyID)
		if err != nil {
			return domain.PendingAction{}, err
		}
		pa.TradeID, pa.Amount = trade.ID, trade.Price
		if colony := state.Colonies[trade.BuyerColonyID]; colony != nil {
			if err := appendLedger(colony, "force_refund_proposed", actor.ID, map[string]any{"action_id": pa.ID, "trade_id": trade.ID}); err != nil {
				return domain.PendingAction{}, domain.InternalErr(err.Error())
			}
		}
		return *pa, nil
	})
}

func (s *Service) ListApprovals(token string) ([]domain.PendingAction, error) {
	return storage.StoreReadScoped(s.store, storage.Scope{Tokens: []string{token}, AllActions: true}, func(state *domain.AppState) ([]domain.PendingAction, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return nil, err
		}
		out := []domain.PendingAction{}
		for _, pa := range state.PendingActions {
			if canManageColony(actor, pa.ColonyID) {
				out = append(out, *pa)
			}
		}
		sort.Slice(out, func(i, j int) bool {
			return out[i].ProposedAt > out[j].ProposedAt || (out[i].ProposedAt == out[j].ProposedAt && out[i].ID > out[j].ID)
		})
		return out, nil
	})
}

// DecideApproval approves (and executes) or rejects a pending action. The
// proposer can never approve their own proposal.
func (s *Service) DecideApproval(token, actionID string, approve bool) (domain.PendingAction, error) {
	return storage.StoreUpdateScoped(s.store, storage.Scope{Tokens: []string{token}, Actions: []string{actionID}}, func(state *domain.AppState) (domain.PendingAction, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return domain.PendingAction{}, err
		}
		pa := state.PendingActions[actionID]
		if pa == nil {
			return domain.PendingAction{}, domain.NotFound("approval not found")
		}
		if !canManageColony(actor, pa.ColonyID) {
			return domain.PendingAction{}, domain.Forbidden("not allowed to decide this approval")
		}
		if actor.ID == pa.ProposedBy {
			return domain.PendingAction{}, domain.Forbidden("the proposer cannot decide their own proposal")
		}
		if pa.Status != "pending" {
			return domain.PendingAction{}, domain.Conflict("approval already " + pa.Status)
		}
		if exp, err := time.Parse(time.RFC3339, pa.ExpiresAt); err != nil || domain.NowFn().After(exp) {
			return domain.PendingAction{}, domain.Conflict("approval has expired")
		}
		colony := state.Colonies[pa.ColonyID]
		if colony == nil {
			return domain.PendingAction{}, domain.NotFound("colony not found")
		}
		if approve {
			switch pa.Type {
			case "mint":
				if _, err := applyMint(state, colony, pa.UserID, pa.Amount, pa.ProposedBy, actor.ID); err != nil {
					return domain.PendingAction{}, err
				}
			case "force_refund":
				trade := state.Trades[pa.TradeID]
				if trade == nil || !domain.IsEscrowed(trade.Status) {
					return domain.PendingAction{}, domain.Conflict("trade is no longer in escrow")
				}
				if err := applyRefund(state, trade, actor.ID, "trade_force_refunded", map[string]any{"proposed_by": pa.ProposedBy, "approved_by": actor.ID}); err != nil {
					return domain.PendingAction{}, err
				}
			case "policy":
				if err := applyPolicy(colony, PolicyRequest{MintCap: pa.MintCap, RequireDualControl: pa.RequireDual}, pa.ProposedBy, actor.ID); err != nil {
					return domain.PendingAction{}, err
				}
			default:
				return domain.PendingAction{}, domain.InternalErr("unknown action type " + pa.Type)
			}
			pa.Status = "approved"
		} else {
			pa.Status = "rejected"
			if err := appendLedger(colony, "approval_rejected", actor.ID, map[string]any{"action_id": pa.ID, "type": pa.Type}); err != nil {
				return domain.PendingAction{}, domain.InternalErr(err.Error())
			}
		}
		pa.DecidedBy, pa.DecidedAt = actor.ID, domain.NowUTC()
		return *pa, nil
	})
}
