CREATE TABLE bets (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                 UUID NOT NULL,
    wallet_id               UUID NOT NULL,
    stake_cents             BIGINT NOT NULL,
    total_odds_milli        BIGINT NOT NULL,
    potential_return_cents  BIGINT NOT NULL,
    status                  TEXT NOT NULL DEFAULT 'pending',
    idempotency_key         TEXT NOT NULL,
    placed_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    settled_at              TIMESTAMPTZ,

    CONSTRAINT bets_user_fk             FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,
    CONSTRAINT bets_wallet_fk           FOREIGN KEY (wallet_id) REFERENCES wallets (id) ON DELETE RESTRICT,
    CONSTRAINT bets_stake_check         CHECK (stake_cents > 0),
    CONSTRAINT bets_odds_check          CHECK (total_odds_milli > 1000),
    CONSTRAINT bets_return_check        CHECK (potential_return_cents >= stake_cents),
    CONSTRAINT bets_status_check        CHECK (status IN ('pending', 'won', 'lost', 'void')),
    CONSTRAINT bets_settled_check       CHECK ((status = 'pending') = (settled_at IS NULL)),
    CONSTRAINT bets_idempotency_unique  UNIQUE (user_id, idempotency_key)
);

CREATE INDEX bets_user_placed_idx ON bets (user_id, placed_at DESC, id DESC);

CREATE TABLE bet_selections (
    bet_id        UUID NOT NULL,
    selection_id  UUID NOT NULL,
    odds_milli    INTEGER NOT NULL,
    result        TEXT NOT NULL DEFAULT 'pending',

    CONSTRAINT bet_selections_pkey          PRIMARY KEY (bet_id, selection_id),
    CONSTRAINT bet_selections_bet_fk        FOREIGN KEY (bet_id) REFERENCES bets (id) ON DELETE RESTRICT,
    CONSTRAINT bet_selections_selection_fk  FOREIGN KEY (selection_id) REFERENCES selections (id) ON DELETE RESTRICT,
    CONSTRAINT bet_selections_odds_check    CHECK (odds_milli > 1000),
    CONSTRAINT bet_selections_result_check  CHECK (result IN ('pending', 'won', 'lost', 'void'))
);

CREATE INDEX bet_selections_selection_idx ON bet_selections (selection_id);