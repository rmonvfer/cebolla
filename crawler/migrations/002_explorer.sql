-- Lookups used by the explorer: a site's entities, and homepages (depth 0).
CREATE INDEX entities_site ON entities (site_id);
CREATE INDEX pages_home ON pages (site_id) WHERE depth = 0;
