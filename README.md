# onion-crawler

A text-only crawler and search index for Tor onion services, for studying how
the onion ecosystem works: which sites exist, how they link, how long they
live, which are clones of each other. It runs only inside an isolated,
encrypted VM on `aleph`.

## Guarantees (and where they are enforced)

| Requirement | Enforced by |
|---|---|
| Text only | `fetch`: Content-Type must be `text/html`/`xhtml`, first 512 bytes sniffed, 2 MB cap after decompression (defuses gzip bombs), download aborted on any mismatch. `sanitize`: strips `img`, `svg`, `picture`, `video`, `object`, `iframe`, `style`, every `data:` URI, `src`/`srcset`/`style`/`background` attributes **before anything is stored**. |
| Blocklist before fetch | Ahmia's MD5 list (synced daily by `seedsync`) + local list, checked (both `md5(addr.onion)` and `md5(addr)`) at enqueue, before every fetch, and on every redirect hop. The crawler does not start until the Ahmia list is loaded. Blocked addresses are not even stored as link targets. |
| Content filter | `filter/terms.txt`, matched against title, text, URL and every link + anchor. One hit: the site is deleted from Postgres (cascade) and OpenSearch (delete + expunge), added to the local blocklist, and its address, **encrypted to a key on the host**, goes to the report queue. Also checked on every URL before queueing/fetching. The crawler refuses to start with an empty term list. |
| Read-only | GET only, no cookies, no forms (form `action`s are stripped, never followed), no JavaScript. Client-auth services are marked `auth_gated` and never retried. |
| Per-site rate limit | Enforced in Postgres (site lease + `next_allowed_at`), so it holds across all workers: one request at a time per site, 8 s apart, 4x back-off on errors. |

## Architecture

```
seedsync ──(clearnet: Ahmia lists)──┐
                                     ▼
            ┌──────────── Postgres (frontier, sites, pages, versions, links, entities, fetch_log, blocklist)
            │                        ▲
   crawler workers ── guards ── tor ×N (SOCKS5, sticky per onion, IsolateSOCKSAuth, ExtendedErrors) ── Tor
            │  sanitize → filter → store → index
            ▼
        OpenSearch (full-text)     Prometheus ← crawler + tor MetricsPort → Grafana
```

- **Fetching**: custom SOCKS5 dialer so tor's extended error codes (descriptor
  missing, intro/rendezvous failure, auth required) reach the liveness logic.
  Each onion is pinned to one tor instance (rendezvous hashing), so its circuit
  is built once and reused. Timeouts: 60 s connect, 30 s headers, 20 s idle,
  120 s total. Transient failures retry 3x (30 min, 1 h, 2 h).
- **Discovery**: v3 addresses are checksum-validated (junk and typo-phishing
  addresses are dropped). Priority: new sites' homepages, then rechecks, then
  shallow pages. Depth ≤ 3, ≤ 200 pages per site, session params stripped.
- **Storage**: sanitized HTML (zstd, deduplicated by hash) + extracted text,
  one `page_versions` row per distinct content (keep everything). SimHash per
  version for near-duplicate/clone analysis; entities (BTC/XMR/ETH, emails,
  PGP keys, mentioned onions) for cross-site correlation.
- **Liveness**: every fetch moves the site through `up` (recheck daily),
  `flaky` (6 h), `down` (1 h doubling to 7 d), `dead` (no answer for 30 days,
  monthly), `auth_gated`.
- **Networks** (compose): `internal` has no route out; only `tor` and
  `seedsync` also join `egress`. The crawler cannot reach anything but tor
  and the databases. The VM itself can reach only the internet (host
  firewall `/etc/onion-iso.nft` on aleph), never the host, LAN or tailnet.

## Operating it

From `aleph`:

```sh
# First time only (after the OS install, see "VM setup")
vm/finalize.sh                    # boot from disk, autostart
sudo virsh console onion          # type the disk passphrase, Ctrl+] to leave
vm/deploy.sh                      # copy project, generate secrets, start stack

# After every host reboot: the VM waits for its passphrase
sudo virsh console onion

# Search
ssh onion 'cd onion-crawler && docker compose run --rm crawler search "query"'

# Dashboards (SSH tunnel; nothing is exposed on the LAN)
ssh -N -L 3000:127.0.0.1:3000 -L 9090:127.0.0.1:9090 onion
# Grafana http://localhost:3000 (admin, password in ~/onion-crawler/.env on the VM)

# Filter-hit addresses for manual reporting to a hotline (decrypted on the host)
vm/fetch-reports.sh

# Logs / scale
ssh onion 'cd onion-crawler && docker compose logs -f crawler'
ssh onion 'cd onion-crawler && sed -i s/^TOR_INSTANCES=.*/TOR_INSTANCES=10/ .env && docker compose up -d'
```

Analysis: query Postgres directly (`ssh onion 'cd onion-crawler && docker
compose exec postgres psql -U crawler'`). Useful starting points: `site_edges`
(materialized view, `REFRESH MATERIALIZED VIEW site_edges`), `fetch_log` for
uptime/lifespans (right-censored: use survival curves), `page_versions.simhash`
for clone clusters, `entities` for shared payment addresses.

## VM setup (one time)

The VM `onion` is defined on aleph (6 vCPU, 24 GB locked RAM, 1 TB qcow2,
CPU weight 50, disk I/O capped), on bridge `virbr-onion` (10.66.0.10).
`vm/prepare-install.sh` stages an Ubuntu 24.04 autoinstall that configures
everything except storage. Then:

1. `sudo virsh start onion --console` (or `sudo virsh console onion` if it
   is already running at the storage screen).
2. Storage screen: **Use an entire disk**, tick **Set up this disk as an LVM
   group** and **Encrypt the LVM group with LUKS**, type the passphrase. Done.
3. The installer powers the VM off when finished. Run `vm/finalize.sh`.

`vm/deploy.sh` refuses to deploy unless the VM root is on LUKS.
Emergency console login: user `ramon`, password in `~/.ssh/onion-console-password`.

## Development

```sh
crawler/go.sh test ./...          # unit tests (Go runs in a container)
scripts/integration-test.sh       # real Postgres + OpenSearch, fake tor and onion sites
```

## Known limitations

- **Purges are logical first.** Postgres and OpenSearch mark deleted data and
  reuse the space later (the crawler runs `VACUUM` and an expunge merge after
  each purge), but bytes can linger in free pages or WAL until overwritten.
  The VM disk is LUKS-encrypted; for a hard guarantee run `VACUUM FULL` periodically.
- **The term list is a starter list.** Extend `filter/terms.txt` from a
  maintained source before crawling at scale.
- **Tor Browser fingerprint is partial.** The User-Agent and Accept headers
  match Tor Browser; header order and the absence of sub-resource requests do not.
- `robots.txt` is ignored by choice; politeness relies on the per-site limits.
