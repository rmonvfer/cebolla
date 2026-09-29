-- Results of the periodic graph/entity analysis (see internal/analyze).
-- Recomputed in full each run; rows cascade away when a site is purged.
CREATE TABLE site_analysis (
    site_id    bigint PRIMARY KEY REFERENCES sites ON DELETE CASCADE,
    pagerank   double precision NOT NULL DEFAULT 0,
    component  integer,   -- weakly-connected component id (0 = largest)
    operator   integer,   -- operator cluster id (sites sharing payment addrs/keys), NULL if singleton
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX site_analysis_rank ON site_analysis (pagerank DESC);
CREATE INDEX site_analysis_component ON site_analysis (component);
CREATE INDEX site_analysis_operator ON site_analysis (operator) WHERE operator IS NOT NULL;
