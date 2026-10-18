package main

import (
	"sort"
	"time"
)

// Dual control: some admin actions are only proposed by one admin and take
// effect when a DIFFERENT admin with authority over the colony approves.
//
// Always dual-control: force_refund (it bypasses the seller colony's
// authority, so a second human must confirm out-of-band that the seller did
// not credit). Optional per colony: mint, and policy changes while enabled.

func newPendingAction(state *AppState, proposer *User, typ, colonyID string) (*PendingAction, error) {
	id, err := newID("act")
	if err != nil {
		return nil, internalErr(err.Error())
	}
	now := nowFn().UTC()
	pa := &PendingAction{ID: id, Type: typ, ColonyID: colonyID, ProposedBy: proposer.ID, ProposedAt: now.Format(time.RFC3339), ExpiresAt: now.Add(PendingActionTTL).Format(time.RFC3339), Status: "pending"}
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
		return PolicyOutcome{}, badRequest("mint_cap or require_dual_control is required")
	}
	return StoreUpdateScoped(s.store, Scope{Tokens: []string{token}, Colonies: []string{colonyID}}, func(state *AppState) (PolicyOutcome, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return PolicyOutcome{}, err
		}
		if !hasRole(actor, "super_admin") {
			return PolicyOutcome{}, forbidden("only super_admin can change supply policy")
		}
		colony := state.Colonies[colonyID]
		if colony == nil {
			return PolicyOutcome{}, notFound("colony not found")
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
				return PolicyOutcome{}, internalErr(err.Error())
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
	Colony  *ColonyView    `json:"colony,omitempty"`
	Pending *PendingAction `json:"pending,omitempty"`
}

func validatePolicy(colony *Colony, req PolicyRequest) error {
	if req.MintCap != nil && (*req.MintCap <= 0 || *req.MintCap < colony.MintedTotal || *req.MintCap > DefaultMintCap) {
		return badRequest("mint_cap must be positive, at least the amount already minted, and at most the platform maximum")
	}
	return nil
}

func applyPolicy(colony *Colony, req PolicyRequest, actorID, approvedBy string) error {
	if err := validatePolicy(colony, req); err != nil {
		return err
	}
	if req.MintCap != nil {
		colony.MintCap = *req.MintCap
	}
	if req.RequireDualControl != nil {
		colony.RequireDualControl = *req.RequireDualControl
	}
	payload := map[string]any{"mint_cap": effectiveMintCap(colony), "require_dual_control": colony.RequireDualControl}
	if approvedBy != "" {
		payload["approved_by"] = approvedBy
	}
	if err := appendLedger(colony, "policy_changed", actorID, payload); err != nil {
		return internalErr(err.Error())
	}
	return nil
}

// ProposeForceRefund proposes refunding an escrowed buyer without the seller
// colony's confirmation. Risk (documented): if the seller colony really did
// credit, the refund creates money. The approver must confirm out-of-band.
func (s *Service) ProposeForceRefund(token, tradeID string) (PendingAction, error) {
	return StoreUpdateScoped(s.store, Scope{Tokens: []string{token}, Trades: []string{tradeID}}, func(state *AppState) (PendingAction, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return PendingAction{}, err
		}
		trade := state.Trades[tradeID]
		if trade == nil {
			return PendingAction{}, notFound("trade not found")
		}
		if !canManageColony(actor, trade.BuyerColonyID) {
			return PendingAction{}, forbidden("only an admin of the buyer colony can propose a force refund")
		}
		if !isEscrowed(trade.Status) {
			return PendingAction{}, conflict("trade is not in escrow (" + trade.Status + ")")
		}
		pa, err := newPendingAction(state, actor, "force_refund", trade.BuyerColonyID)
		if err != nil {
			return PendingAction{}, err
		}
		pa.TradeID, pa.Amount = trade.ID, trade.Price
		if colony := state.Colonies[trade.BuyerColonyID]; colony != nil {
			if err := appendLedger(colony, "force_refund_proposed", actor.ID, map[string]any{"action_id": pa.ID, "trade_id": trade.ID}); err != nil {
				return PendingAction{}, internalErr(err.Error())
			}
		}
		return *pa, nil
	})
}

func (s *Service) ListApprovals(token string) ([]PendingAction, error) {
	return StoreReadScoped(s.store, Scope{Tokens: []string{token}, AllActions: true}, func(state *AppState) ([]PendingAction, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return nil, err
		}
		out := []PendingAction{}
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
func (s *Service) DecideApproval(token, actionID string, approve bool) (PendingAction, error) {
	return StoreUpdateScoped(s.store, Scope{Tokens: []string{token}, Actions: []string{actionID}}, func(state *AppState) (PendingAction, error) {
		actor, err := actorFromToken(state, token)
		if err != nil {
			return PendingAction{}, err
		}
		pa := state.PendingActions[actionID]
		if pa == nil {
			return PendingAction{}, notFound("approval not found")
		}
		if !canManageColony(actor, pa.ColonyID) {
			return PendingAction{}, forbidden("not allowed to decide this approval")
		}
		if actor.ID == pa.ProposedBy {
			return PendingAction{}, forbidden("the proposer cannot decide their own proposal")
		}
		if pa.Status != "pending" {
			return PendingAction{}, conflict("approval already " + pa.Status)
		}
		if exp, err := time.Parse(time.RFC3339, pa.ExpiresAt); err != nil || nowFn().After(exp) {
			return PendingAction{}, conflict("approval has expired")
		}
		colony := state.Colonies[pa.ColonyID]
		if colony == nil {
			return PendingAction{}, notFound("colony not found")
		}
		if approve {
			switch pa.Type {
			case "mint":
				if _, err := applyMint(state, colony, pa.UserID, pa.Amount, pa.ProposedBy, actor.ID); err != nil {
					return PendingAction{}, err
				}
			case "force_refund":
				trade := state.Trades[pa.TradeID]
				if trade == nil || !isEscrowed(trade.Status) {
					return PendingAction{}, conflict("trade is no longer in escrow")
				}
				if err := applyRefund(state, trade, actor.ID, "trade_force_refunded", map[string]any{"proposed_by": pa.ProposedBy, "approved_by": actor.ID}); err != nil {
					return PendingAction{}, err
				}
			case "policy":
				if err := applyPolicy(colony, PolicyRequest{MintCap: pa.MintCap, RequireDualControl: pa.RequireDual}, pa.ProposedBy, actor.ID); err != nil {
					return PendingAction{}, err
				}
			default:
				return PendingAction{}, internalErr("unknown action type " + pa.Type)
			}
			pa.Status = "approved"
		} else {
			pa.Status = "rejected"
			if err := appendLedger(colony, "approval_rejected", actor.ID, map[string]any{"action_id": pa.ID, "type": pa.Type}); err != nil {
				return PendingAction{}, internalErr(err.Error())
			}
		}
		pa.DecidedBy, pa.DecidedAt = actor.ID, nowUTC()
		return *pa, nil
	})
}
