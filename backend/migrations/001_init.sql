-- 001_init.sql — transactional schema for the interstellar trade platform.
--
-- Design rules
--  * Money lives in typed columns with CHECK constraints (balance >= 0), never
--    only inside JSON.
--  * Idempotency is enforced by constraints, not by application memory:
--    processed_envelopes PK, ledger_entries PK (colony_id, seq), bundles PK.
--  * Row ownership: every mutation of a trade/account/ledger/outbox row happens
--    in a transaction that first locks the owning colony rows in ID order, so
--    different colony pairs run in parallel and deadlocks cannot occur.
--  * Entities keep a JSONB "doc" for fields that are not queried.

CREATE TABLE colonies (
    id          text PRIMARY KEY,
    name_lower  text NOT NULL UNIQUE,
    doc         jsonb NOT NULL
);

CREATE TABLE users (
    id              text PRIMARY KEY,
    username_lower  text NOT NULL UNIQUE,
    colony_id       text NOT NULL DEFAULT '',
    doc             jsonb NOT NULL
);

CREATE TABLE sessions (
    token       text PRIMARY KEY,
    user_id     text NOT NULL,
    expires_at  timestamptz NOT NULL,
    doc         jsonb NOT NULL
);
CREATE INDEX sessions_user_idx ON sessions (user_id);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

CREATE TABLE accounts (
    colony_id  text   NOT NULL REFERENCES colonies (id),
    user_id    text   NOT NULL REFERENCES users (id),
    balance    bigint NOT NULL CHECK (balance >= 0),
    PRIMARY KEY (colony_id, user_id)
);

CREATE TABLE trades (
    id               text PRIMARY KEY,
    seller_user_id   text NOT NULL,
    buyer_user_id    text NOT NULL,
    seller_colony_id text NOT NULL,
    buyer_colony_id  text NOT NULL,
    status           text NOT NULL,
    created_at       text NOT NULL,
    expires_at       timestamptz,
    doc              jsonb NOT NULL
);
CREATE INDEX trades_seller_user_idx ON trades (seller_user_id);
CREATE INDEX trades_buyer_user_idx ON trades (buyer_user_id);
CREATE INDEX trades_seller_colony_idx ON trades (seller_colony_id, created_at DESC);
CREATE INDEX trades_buyer_colony_idx ON trades (buyer_colony_id, created_at DESC);
CREATE INDEX trades_due_idx ON trades (expires_at) WHERE status IN ('offer_pending_export', 'offer_received');

-- NOTE: columns that carry raw signed payloads (ledger entries, envelopes,
-- bundles) are `json`, NOT `jsonb`: jsonb re-orders keys and re-spaces text,
-- which would change the bytes that signatures and hash chains cover.
CREATE TABLE ledger_entries (
    colony_id  text   NOT NULL REFERENCES colonies (id),
    seq        bigint NOT NULL,
    doc        json   NOT NULL,
    PRIMARY KEY (colony_id, seq)
);

-- Relay outbox: written in the same transaction as the state change that
-- produced the message, so a message is never lost or phantom.
CREATE TABLE outbox (
    ord          bigserial PRIMARY KEY,
    colony_id    text NOT NULL REFERENCES colonies (id),
    envelope_id  text NOT NULL UNIQUE,
    to_colony_id text NOT NULL,
    doc          json NOT NULL
);
CREATE INDEX outbox_colony_idx ON outbox (colony_id, ord);

CREATE TABLE processed_envelopes (
    colony_id    text NOT NULL REFERENCES colonies (id),
    envelope_id  text NOT NULL,
    PRIMARY KEY (colony_id, envelope_id)
);

-- First writer wins; an imported bundle can never overwrite an existing one.
CREATE TABLE bundles (
    id   text PRIMARY KEY,
    doc  json NOT NULL
);

CREATE TABLE pending_actions (
    id         text PRIMARY KEY,
    colony_id  text NOT NULL,
    trade_id   text NOT NULL DEFAULT '',
    user_id    text NOT NULL DEFAULT '',
    status     text NOT NULL,
    proposed_at text NOT NULL,
    doc        jsonb NOT NULL
);
CREATE INDEX pending_actions_colony_idx ON pending_actions (colony_id, proposed_at DESC);



