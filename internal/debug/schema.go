package debug

// Schema for the unified glass_debug.db.
// Consolidates fingerprint.db + thinking_audit.db into one source of truth.

const schemaSQL = `
-- Unified request events: one row per API call with ALL debug dimensions.
-- Merges request_events_v2 + audit_samples.
CREATE TABLE IF NOT EXISTS requests (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp       TEXT NOT NULL,
    session_id      TEXT,
    conversation_id TEXT,
    request_id      TEXT,
    request_lane    TEXT,
    request_transport TEXT,

    -- Model
    model_requested TEXT,
    model_response  TEXT,
    model_match     INTEGER,
    is_subagent     INTEGER DEFAULT 0,
    subagent_type   TEXT,
    has_tool_use    INTEGER DEFAULT 0,

    -- Thinking
    thinking_enabled        INTEGER DEFAULT 0,
    thinking_budget         INTEGER DEFAULT 0,
    thinking_tier           TEXT,
    thinking_chunk_count    INTEGER DEFAULT 0,
    thinking_tokens_used    INTEGER DEFAULT 0,
    thinking_utilization    REAL DEFAULT 0,
    thinking_duration_ms    REAL DEFAULT 0,
    thinking_itt_mean_ms    REAL DEFAULT 0,
    thinking_itt_std_ms     REAL DEFAULT 0,

    -- Text generation
    text_chunk_count    INTEGER DEFAULT 0,
    text_duration_ms    REAL DEFAULT 0,
    text_itt_mean_ms    REAL DEFAULT 0,
    text_itt_std_ms     REAL DEFAULT 0,

    -- Tokens
    input_tokens    INTEGER DEFAULT 0,
    output_tokens   INTEGER DEFAULT 0,

    -- Cache
    cache_creation_tokens   INTEGER DEFAULT 0,
    cache_read_tokens       INTEGER DEFAULT 0,
    cache_efficiency        REAL DEFAULT 0,

    -- ITT (inter-token timing)
    ttft_ms         REAL DEFAULT 0,
    total_time_ms   REAL DEFAULT 0,
    itt_mean_ms     REAL DEFAULT 0,
    itt_std_ms      REAL DEFAULT 0,
    itt_min_ms      REAL DEFAULT 0,
    itt_max_ms      REAL DEFAULT 0,
    itt_p50_ms      REAL DEFAULT 0,
    itt_p90_ms      REAL DEFAULT 0,
    itt_p99_ms      REAL DEFAULT 0,
    tokens_per_sec  REAL DEFAULT 0,
    variance_coef   REAL DEFAULT 0,
    num_chunks      INTEGER DEFAULT 0,

    -- Backend classification
    classified_backend  TEXT,
    confidence          REAL DEFAULT 0,
    backend_evidence    TEXT,
    location            TEXT,
    cf_edge_location    TEXT,

    -- Speculative decoding
    speculative_decoding    INTEGER DEFAULT 0,
    speculative_type        TEXT,

    -- Context
    context_api_tokens  INTEGER DEFAULT 0,
    context_api_pct     REAL DEFAULT 0,
    context_cc_pct      REAL DEFAULT 0,
    context_mismatch    INTEGER DEFAULT 0,

    -- Rate limits
    rl_binding_window   TEXT,
    rl_5h_utilization   REAL DEFAULT 0,
    rl_5h_status        TEXT,
    rl_7d_utilization   REAL DEFAULT 0,
    rl_7d_status        TEXT,
    rl_overall_status   TEXT,

    -- Sycophancy
    sycophancy_score        REAL DEFAULT 0,
    sycophancy_signals      TEXT,
    sycophancy_divergence   REAL DEFAULT 0,

    -- Glass pipeline
    glass_evicted_count     INTEGER DEFAULT 0,
    glass_stripped_count    INTEGER DEFAULT 0,
    glass_orphans_fixed    INTEGER DEFAULT 0,
    glass_tokens_saved     INTEGER DEFAULT 0,
    glass_shadow_batch     INTEGER DEFAULT 0,
    glass_prefix_change_kind TEXT,
    glass_prefix_divergence TEXT,
    glass_prefix_system_changed INTEGER DEFAULT 0,
    glass_prefix_tools_changed INTEGER DEFAULT 0,
    glass_prefix_anchor INTEGER DEFAULT 0,
    glass_prefix_prev_anchor INTEGER DEFAULT 0,
    glass_prefix_measured_msgs INTEGER DEFAULT 0,
    glass_compression_watermark INTEGER DEFAULT 0,
    glass_prefix_hash TEXT,
    glass_active_prefix_count INTEGER DEFAULT 0,
    glass_eviction_detected INTEGER DEFAULT 0,
    glass_tail_change_kind TEXT,
    glass_tail_divergence TEXT,
    glass_tail_anchor INTEGER DEFAULT 0,
    glass_tail_prev_anchor INTEGER DEFAULT 0,
    glass_tail_measured_msgs INTEGER DEFAULT 0,
    glass_tail_tokens INTEGER DEFAULT 0,
    glass_tail_hash TEXT,

    -- Content (truncated for debugging)
    stop_reason     TEXT,
    output_preview  TEXT,
    user_preview    TEXT,

    created_at TEXT NOT NULL DEFAULT (strftime('%%Y-%%m-%%dT%%H:%%M:%%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_req_ts ON requests(timestamp);
CREATE INDEX IF NOT EXISTS idx_req_conv ON requests(conversation_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_req_session ON requests(session_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_req_backend ON requests(classified_backend, timestamp);

-- Lightweight subagent events for requests blocked before they enter the main request recorder.
CREATE TABLE IF NOT EXISTS subagent_events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp       TEXT NOT NULL,
    session_id      TEXT,
    conversation_id TEXT,
    request_id      TEXT,
    request_lane    TEXT,
    request_transport TEXT,
    model_requested TEXT,
    subagent_type   TEXT,
    blocked_by_model INTEGER DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%%Y-%%m-%%dT%%H:%%M:%%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_subagent_ts ON subagent_events(timestamp);
CREATE INDEX IF NOT EXISTS idx_subagent_session ON subagent_events(session_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_subagent_conv ON subagent_events(conversation_id, timestamp);

-- Raw ITT samples (per-token timing for deep analysis)
CREATE TABLE IF NOT EXISTS itt_samples (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id  TEXT NOT NULL,
    seq         INTEGER NOT NULL,
    delta_ms    REAL NOT NULL,
    token       TEXT,
    phase       TEXT DEFAULT 'text',
    timestamp   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_itt_req ON itt_samples(request_id);

-- Glass eviction events
CREATE TABLE IF NOT EXISTS eviction_events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp       TEXT NOT NULL,
    conversation_id TEXT NOT NULL,
    batch_number    INTEGER,
    messages_evicted INTEGER,
    tokens_before   INTEGER,
    tokens_after    INTEGER,
    trigger_reason  TEXT,
    shadow_path     TEXT
);

CREATE INDEX IF NOT EXISTS idx_evict_conv ON eviction_events(conversation_id, timestamp);

-- Cache events (per-request cache breakdown)
CREATE TABLE IF NOT EXISTS cache_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id  TEXT NOT NULL,
    timestamp   TEXT NOT NULL,
    cache_read  INTEGER DEFAULT 0,
    cache_create INTEGER DEFAULT 0,
    input_tokens INTEGER DEFAULT 0,
    hit_ratio   REAL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_cache_req ON cache_events(request_id);

-- Quota snapshots (periodic burn rate tracking)
CREATE TABLE IF NOT EXISTS quota_snapshots (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp       TEXT NOT NULL,
    binding_window  TEXT,
    utilization_5h  REAL DEFAULT 0,
    utilization_7d  REAL DEFAULT 0,
    status_5h       TEXT,
    status_7d       TEXT,
    overall_status  TEXT,
    statusline_burn_pp_hr REAL,
    statusline_current_5h_pct REAL,
    statusline_hours_left REAL,
    statusline_samples_used INTEGER DEFAULT 0,
    statusline_window_min REAL,
    statusline_reset_detected INTEGER DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_quota_ts ON quota_snapshots(timestamp);

-- Dedup events
CREATE TABLE IF NOT EXISTS dedup_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp   TEXT NOT NULL,
    request_hash TEXT,
    action      TEXT,
    conversation_id TEXT
);

-- Session lifecycle
CREATE TABLE IF NOT EXISTS sessions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id      TEXT NOT NULL UNIQUE,
    started_at      TEXT NOT NULL,
    last_seen_at    TEXT NOT NULL,
    request_count   INTEGER DEFAULT 0,
    dominant_backend TEXT,
    backend_switches INTEGER DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_sessions_id ON sessions(session_id);
`
