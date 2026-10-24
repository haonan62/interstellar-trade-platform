# Phase 1 — Core correctness (design + status)

## Done (no wire-format / data-model change)

| Item | Decision | Alternative rejected |
|---|---|---|
| Atomic updates | `StoreUpdate` runs the handler on a deep copy (JSON round-trip) and swaps it in only after the copy is durably written. Any handler error or persist error leaves memory and disk untouched. | Hand-ordering "validate then apply" in every handler: fragile, one miss reintroduces the bug. Kept as defence in depth (e.g. `importAcceptance` computes all checked sums before mutating). |
| State machine | `backend/internal/domain/trade_state.go`: explicit transition table; all status changes go through `AdvanceTrade` / `StepTrade`. | Ad-hoc string comparisons. |
| Idempotency | Per trade and step: a message for a step the trade already passed is a no-op counted in `skipped_duplicates`; a message arriving before its predecessor state is a 409. | Rejecting duplicates with an error: poisons the whole (atomic) bundle. |
| Import validation | Payload colony IDs must equal envelope source/target; users must belong to the stated colony; an offer for an existing trade must match its fields; settlement payload must match seller user/colony. | — |
| Arithmetic | `addChecked`, `MaxAmount` (1e12) for prices and each mint. | — |
| Ledger conservation | Property test, random loss/duplication/reordering, invariant checked every step: minted = balances + escrow. | — |

Risks / limits
- Copy-on-write is O(state) per write. Acceptable for the POC; Phase 3 replaces it.
- All colonies still share one `Trade` record, so "predecessor state" is a shared-state approximation. Phase 2 (per-colony records) must re-derive the table per side.
- Messages that arrive too early return 409 and abort the whole bundle; the relay must re-order and retry.

## Implemented after approval (additive wire-format / data-model changes)

Status: A and B are IMPLEMENTED and tested; C is an accepted non-goal.

Compatibility notes
- All new JSON fields are `omitempty` or additive; the offer signable keeps `expires_at` `omitempty`, so signatures of pre-expiry offers stay byte-identical (tested).
- `POST /accounts/mint` is unchanged for colonies without dual control; with dual control it returns 202 and a pending action.
- New endpoints: `POST /trades/{id}/cancel`, `POST /trades/{id}/force-refund`, `POST /colonies/{id}/policy`, `GET /approvals`, `POST /approvals/{id}/approve|reject`.
- New envelope types `cancel_request` (buyer to seller colony) and `refund` (seller colony to buyer colony). A peer running old code rejects them as unsupported types (ASSUMED: not tested against an old build).
- Known limit: trades still share one record across colonies, so "seller colony authority" is modelled by state, not by separate stores (Phase 2).
- Force refund is unsafe if the seller colony really credited; hence always dual-controlled and documented as requiring out-of-band confirmation.
- Time: expiry uses the buyer-attested, signed acceptance time, so light delay does not make a timely acceptance late; it does trust the buyer colony's clock (Phase 2 replaces wall-clock ordering).

## Original proposal (kept for reference)

### A. Expiry, cancel, refund (two-generals tradeoff)
Problem: the buyer is debited at acceptance; if the acceptance is lost, funds are stuck in escrow forever. Neither side can know if the other saw the message.
Proposal (safety over liveness):
1. Offer carries `expires_at`; acceptance must be signed before it, and the seller colony rejects acceptances arriving after it **unless** the trade already moved past offer.
2. Seller colony is the single authority for the trade outcome. Buyer may request `cancel_request` only while the acceptance is unacknowledged; refund happens only on a signed `refund` envelope from the seller colony ("seller rejected / expired, you are refunded").
3. Buyer-side timeout never refunds unilaterally (that would allow double-spend if the seller credited just before). Stuck escrow is visible and resolved by an admin dual-control action. Cost: liveness under partitions; benefit: no double credit.
New statuses: `expired`, `cancelled`, `refunded`; new envelope types: `cancel_request`, `refund`; new fields: `expires_at`.

### B. Mint caps and dual control
Needs per-colony `minted_total` + `mint_cap` fields and a `pending_mints` collection (second admin approves). Per-mint limit is already enforced.

### C. Asset / delivery leg
Proposal: documented **non-goal** for now. Trades are payment-for-label only; ownership and delivery need an asset registry and a delivery-confirmation envelope, which is a separate design.

## Verification (run on 2026-10-14)
See the phase report: `go vet`, `go test -race`, golangci-lint v1.64.8 and the demo flow all pass; issue 3 tests still fail by design (Phase 3).

