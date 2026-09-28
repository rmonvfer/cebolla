-- Onion crawler schema. Postgres is the source of truth; OpenSearch is a
-- rebuildable index of it.
--
-- Purging a site (content filter hit or blocklist) is one DELETE FROM sites:
-- everything below cascades from it, except blobs, which are shared across
-- sites by content hash and collected separately (see store.PurgeSite).

CREATE TYPE site_status AS ENUM ('unknown', 'up', 'flaky', 'down', 'dead', 'auth_gated');

CREATE TABLE sites (
    id                   bigserial PRIMARY KEY,
    onion                text NOT NULL UNIQUE CHECK (onion ~ '^[a-z2-7]{56}$'),
    discovered_via       text NOT NULL,
    first_seen           timestamptz NOT NULL DEFAULT now(),
    last_seen            timestamptz,           -- last fetch attempt
    last_ok              timestamptz,           -- last time the service answered HTTP
    status               site_status NOT NULL DEFAULT 'unknown',
    consecutive_failures int NOT NULL DEFAULT 0,
    next_check_at        timestamptz NOT NULL DEFAULT now() + interval '1 day', -- set properly after each fetch
    title                text,
    server               text,
    pages_fetched        int NOT NULL DEFAULT 0,
    -- Politeness: one in-flight request per site, enforced here, not per worker.
    next_allowed_at      timestamptz NOT NULL DEFAULT now(),
    lease_until          timestamptz NOT NULL DEFAULT 'epoch'
);
CREATE INDEX sites_due ON sites (next_allowed_at) WHERE status <> 'auth_gated';
CREATE INDEX sites_check ON sites (next_check_at);

CREATE TABLE frontier (
    url_hash       bytea PRIMARY KEY,
    url            text NOT NULL,
    site_id        bigint NOT NULL REFERENCES sites ON DELETE CASCADE,
    depth          int NOT NULL,
    priority       smallint NOT NULL,         -- lower runs first: 0 homepage of new site, 1 recheck, 2+depth
    next_fetch_at  timestamptz NOT NULL DEFAULT now(),
    attempts       int NOT NULL DEFAULT 0,
    added_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX frontier_site ON frontier (site_id, priority, next_fetch_at);

CREATE TABLE blobs (
    hash  bytea PRIMARY KEY,                  -- sha256 of sanitized HTML
    zstd  bytea NOT NULL                      -- sanitized HTML (no images, no data: URIs), zstd-compressed
);

CREATE TABLE pages (
    id                 bigserial PRIMARY KEY,
    site_id            bigint NOT NULL REFERENCES sites ON DELETE CASCADE,
    url                text NOT NULL,
    url_hash           bytea NOT NULL UNIQUE,
    depth              int NOT NULL,
    first_fetched      timestamptz NOT NULL DEFAULT now(),
    last_fetched       timestamptz NOT NULL DEFAULT now(),
    last_status        int NOT NULL,
    current_version_id bigint,
    indexed_version_id bigint
);
CREATE INDEX pages_site ON pages (site_id);
CREATE INDEX pages_unindexed ON pages (id) WHERE current_version_id IS DISTINCT FROM indexed_version_id;

-- Every distinct content a page has had (keep-everything retention).
CREATE TABLE page_versions (
    id           bigserial PRIMARY KEY,
    page_id      bigint NOT NULL REFERENCES pages ON DELETE CASCADE,
    fetched_at   timestamptz NOT NULL DEFAULT now(),
    http_status  int NOT NULL,
    content_type text,
    text_hash    bytea NOT NULL,              -- sha256 of extracted text: exact duplicates
    simhash      bigint NOT NULL,             -- near duplicates (Hamming distance <= 3)
    title        text,
    text         text NOT NULL,
    html_hash    bytea NOT NULL REFERENCES blobs
);
CREATE INDEX page_versions_page ON page_versions (page_id);
CREATE INDEX page_versions_text_hash ON page_versions (text_hash);
CREATE INDEX page_versions_simhash ON page_versions (simhash);
ALTER TABLE pages ADD FOREIGN KEY (current_version_id) REFERENCES page_versions ON DELETE SET NULL;

-- Outgoing links of the current version of a page.
CREATE TABLE links (
    src_page_id  bigint NOT NULL REFERENCES pages ON DELETE CASCADE,
    dst_site_id  bigint REFERENCES sites ON DELETE CASCADE,   -- NULL: clearnet or invalid (recorded, never fetched)
    dst_url      text NOT NULL,
    anchor_text  text,
    is_onion     boolean NOT NULL
);
CREATE INDEX links_src ON links (src_page_id);
CREATE INDEX links_dst ON links (dst_site_id);

CREATE TABLE entities (
    page_version_id bigint NOT NULL REFERENCES page_versions ON DELETE CASCADE,
    site_id         bigint NOT NULL REFERENCES sites ON DELETE CASCADE,
    kind            text NOT NULL,            -- btc, xmr, eth, email, pgp, onion
    value           text NOT NULL,
    PRIMARY KEY (page_version_id, kind, value)
);
CREATE INDEX entities_value ON entities (kind, value);

-- One row per fetch attempt: liveness, latency and error analysis.
CREATE TABLE fetch_log (
    ts          timestamptz NOT NULL DEFAULT now(),
    site_id     bigint NOT NULL REFERENCES sites ON DELETE CASCADE,
    url_hash    bytea NOT NULL,
    outcome     text NOT NULL,                -- ok, http_error, desc_not_found, intro_failed, timeout, content_type, ...
    http_status int,
    latency_ms  int,
    bytes       int,
    proxy       text
);
CREATE INDEX fetch_log_site_ts ON fetch_log (site_id, ts);
CREATE INDEX fetch_log_ts ON fetch_log (ts);

-- MD5 hashes only: the crawler never needs the plaintext of a blocked address.
CREATE TABLE blocklist (
    md5       text PRIMARY KEY CHECK (md5 ~ '^[0-9a-f]{32}$'),
    source    text NOT NULL,                  -- ahmia | local
    added_at  timestamptz NOT NULL DEFAULT now(),
    reason    text
);

-- Site-level link graph for analysis (REFRESH MATERIALIZED VIEW site_edges).
CREATE MATERIALIZED VIEW site_edges AS
SELECT p.site_id AS src_site_id, l.dst_site_id, count(*) AS n_links
FROM links l JOIN pages p ON p.id = l.src_page_id
WHERE l.dst_site_id IS NOT NULL AND l.dst_site_id <> p.site_id
GROUP BY 1, 2;
CREATE UNIQUE INDEX site_edges_pk ON site_edges (src_site_id, dst_site_id);
