CREATE TABLE IF NOT EXISTS projects (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    created     TEXT NOT NULL,
    workspace   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects(id),
    created     TEXT NOT NULL,
    updated     TEXT NOT NULL,
    model       TEXT NOT NULL,
    provider    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
    id            TEXT PRIMARY KEY,
    session_id    TEXT NOT NULL REFERENCES sessions(id),
    role          TEXT NOT NULL,
    content       TEXT NOT NULL,
    tool_calls    TEXT,
    tool_call_id  TEXT,
    created       TEXT NOT NULL,
    tokens        INTEGER NOT NULL DEFAULT 0,
    cost_usd      REAL NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, created);

CREATE TABLE IF NOT EXISTS plans (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects(id),
    created     TEXT NOT NULL,
    body        TEXT NOT NULL,
    approved    INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS designs (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects(id),
    plan_id     TEXT REFERENCES plans(id),
    created     TEXT NOT NULL,
    body        TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_designs_project ON designs(project_id, created);

CREATE TABLE IF NOT EXISTS jobs (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects(id),
    kind        TEXT NOT NULL,
    tool        TEXT NOT NULL,
    status      TEXT NOT NULL,
    created     TEXT NOT NULL,
    started     TEXT,
    finished    TEXT,
    progress    REAL NOT NULL DEFAULT 0,
    backend     TEXT NOT NULL,
    cost_usd    REAL NOT NULL DEFAULT 0,
    input       TEXT NOT NULL,
    output      TEXT,
    error       TEXT,
    log_file    TEXT
);

CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status, created);
CREATE INDEX IF NOT EXISTS idx_jobs_project ON jobs(project_id, created);

CREATE TABLE IF NOT EXISTS experiments (
    id           TEXT PRIMARY KEY,
    project_id   TEXT NOT NULL REFERENCES projects(id),
    backend      TEXT NOT NULL,
    external_id  TEXT NOT NULL,
    assay_type   TEXT NOT NULL,
    target_id    TEXT NOT NULL,
    target_name  TEXT NOT NULL,
    submitted    TEXT NOT NULL,
    status       TEXT NOT NULL,
    cost_usd     REAL NOT NULL DEFAULT 0,
    body         TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_experiments_project ON experiments(project_id, submitted);

CREATE TABLE IF NOT EXISTS webhook_events (
    id           TEXT PRIMARY KEY,
    received     TEXT NOT NULL,
    source       TEXT NOT NULL,
    signature    TEXT,
    payload      TEXT NOT NULL,
    processed    INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS corpus_papers (
    id           TEXT PRIMARY KEY,
    project_id   TEXT NOT NULL REFERENCES projects(id),
    title        TEXT NOT NULL,
    authors      TEXT,
    year         INTEGER,
    source       TEXT NOT NULL,
    full_text    TEXT,
    metadata     TEXT NOT NULL,
    added        TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_corpus_project ON corpus_papers(project_id);
