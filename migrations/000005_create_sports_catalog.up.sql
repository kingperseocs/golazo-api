CREATE TABLE teams (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    short_name  TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT teams_name_unique UNIQUE (name)
);

CREATE TABLE events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    home_team_id  UUID NOT NULL,
    away_team_id  UUID NOT NULL,
    starts_at     TIMESTAMPTZ NOT NULL,
    status        TEXT NOT NULL DEFAULT 'scheduled',
    home_score    INTEGER,
    away_score    INTEGER,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT events_home_team_fk  FOREIGN KEY (home_team_id) REFERENCES teams (id) ON DELETE RESTRICT,
    CONSTRAINT events_away_team_fk  FOREIGN KEY (away_team_id) REFERENCES teams (id) ON DELETE RESTRICT,
    CONSTRAINT events_teams_check   CHECK (home_team_id <> away_team_id),
    CONSTRAINT events_status_check  CHECK (status IN ('scheduled', 'live', 'finished', 'cancelled')),
    CONSTRAINT events_scores_non_negative CHECK (home_score >= 0 AND away_score >= 0),
    CONSTRAINT events_finished_has_score  CHECK (
        status <> 'finished' OR (home_score IS NOT NULL AND away_score IS NOT NULL)
    )
);

CREATE INDEX events_starts_at_idx ON events (starts_at);

CREATE TABLE markets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id    UUID NOT NULL,
    type        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'open',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT markets_event_fk      FOREIGN KEY (event_id) REFERENCES events (id) ON DELETE RESTRICT,
    CONSTRAINT markets_type_check    CHECK (type IN ('match_result', 'total_goals_2_5')),
    CONSTRAINT markets_status_check  CHECK (status IN ('open', 'suspended', 'closed', 'settled')),
    CONSTRAINT markets_event_type_unique UNIQUE (event_id, type)
);

CREATE TABLE selections (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    market_id   UUID NOT NULL,
    code        TEXT NOT NULL,
    odds_milli  INTEGER NOT NULL,
    result      TEXT NOT NULL DEFAULT 'pending',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT selections_market_fk        FOREIGN KEY (market_id) REFERENCES markets (id) ON DELETE RESTRICT,
    CONSTRAINT selections_code_check       CHECK (code IN ('home', 'draw', 'away', 'over', 'under')),
    CONSTRAINT selections_odds_check       CHECK (odds_milli > 1000),
    CONSTRAINT selections_result_check     CHECK (result IN ('pending', 'won', 'lost', 'void')),
    CONSTRAINT selections_market_code_unique UNIQUE (market_id, code)
);