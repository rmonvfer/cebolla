# cebolla

Text-only crawler and search index for Tor onion services. Runs inside the
`onion` VM on aleph.

## Components

- `crawler/`: Go crawler. `run` crawls, `seedsync` pulls Ahmia's blocklist and
  onion list once a day, `search` queries the index.
- `tor/`: tor client image. Compose runs 5 replicas as SOCKS5 proxies.
- Postgres stores everything. OpenSearch is the full-text index and can be
  rebuilt from Postgres.
- Prometheus and Grafana for metrics.
- `vm/`: scripts to install, finalize and deploy to the VM.

## Crawl rules

- Only `text/html` and `application/xhtml+xml` are downloaded. The first 512
  bytes are sniffed, bodies are capped at 2 MB after decompression, and the
  download is aborted on any mismatch.
- Before storage, the sanitizer removes media, embedding and script
  elements (including `img`, `svg`, `video`, `object`, `iframe`, `script` and
  `style`), the `src`, `srcset`, `style`, `background` and `poster`
  attributes, and any attribute containing a `data:` URI.
- Sites on Ahmia's MD5 blocklist or the local blocklist are skipped at
  enqueue, before fetch and on every redirect. The crawler does not start
  until the Ahmia list is loaded.
- `filter/terms.txt` is matched against title, text, URLs and anchor text.
  On a match the site is deleted from Postgres and OpenSearch and added to the
  local blocklist, and its address is written to the report queue, encrypted
  to a key on the host. The crawler does not start with an empty term list.
- GET only, no cookies, no forms, no JavaScript.
- Each site gets one request at a time, 8 s apart, and 32 s apart after a
  failed fetch. The limit is kept in Postgres, so it applies across all
  workers.
- Max depth 3, max 200 pages per site.

Each onion is pinned to one tor instance so its circuit is reused. Tor's
extended SOCKS errors distinguish an offline service from an overloaded one.
Site status is `up` (rechecked daily), `flaky` (6 h), `down` (1 h, doubling
up to 7 days), `dead` (no response for 30 days, monthly) or `auth_gated`
(needs client authorization, not crawled).

## Network

The compose `internal` network has no route out. Only `tor` and `seedsync`
are also on `egress`. On the host, `/etc/onion-iso.nft` allows the VM to reach
the internet and drops its traffic to the host, LAN and tailnet. The host can
reach the VM on port 22 only.

## Usage

Run from aleph.

```sh
# after a host reboot the VM waits for its disk passphrase
sudo virsh console onion

# deploy or update
vm/deploy.sh

# search
ssh onion 'cd onion-crawler && docker compose run --rm crawler search "query"'

# Grafana on http://localhost:3000, Prometheus on :9090
ssh -N -L 3000:127.0.0.1:3000 -L 9090:127.0.0.1:9090 onion

# logs
ssh onion 'cd onion-crawler && docker compose logs -f crawler'

# decrypt the report queue
vm/fetch-reports.sh

# psql
ssh onion 'cd onion-crawler && docker compose exec postgres psql -U crawler'
```

Settings (tor instances, workers, delays, depth, page cap) are in `.env` on
the VM; see `.env.example`.

## VM install

1. `vm/prepare-install.sh`, then `sudo virsh start onion --console`.
2. At the storage screen choose "Use an entire disk" with LVM and "Encrypt
   the LVM group with LUKS", and set the passphrase. The rest is automated.
3. When the VM powers off, run `vm/finalize.sh`, unlock it on the console,
   then run `vm/deploy.sh`.

`deploy.sh` refuses to run if the VM's root filesystem is not on LUKS.
Console password for user `ramon` is in `~/.ssh/onion-console-password`.

## Tests

```sh
crawler/go.sh test ./...
scripts/integration-test.sh   # Postgres, OpenSearch, fake tor and sites
```

## Limitations

- Deleted rows and documents stay on disk until the space is reused.
  The crawler runs `VACUUM` and an OpenSearch expunge after each purge.
  `VACUUM FULL` rewrites the tables completely.
- `filter/terms.txt` is a short starter list. Extend it before crawling at scale.
- Only the User-Agent and Accept headers match Tor Browser.
- `robots.txt` is ignored.
