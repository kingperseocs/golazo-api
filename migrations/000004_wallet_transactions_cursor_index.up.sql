CREATE INDEX wallet_transactions_wallet_created_id_idx
    ON wallet_transactions (wallet_id, created_at DESC, id DESC);

DROP INDEX IF EXISTS wallet_transactions_wallet_created_idx;