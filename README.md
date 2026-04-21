# confluence-to-outline

A CLI tool for migrating [Confluence](https://www.atlassian.com/software/confluence) spaces into [Outline](https://www.getoutline.com/) collections.

It walks a Confluence space recursively, exports each page, uploads embedded attachments to Outline, rewrites internal links, and imports the result into an Outline collection — preserving the original page hierarchy.

## Features

- Recursive migration of a Confluence space into an Outline collection, preserving the page tree.
- Downloads inline images and re-uploads them as Outline attachments.
- Rewrites Confluence page links inside migrated documents to their new Outline URLs.
- Converts Confluence code panels (`brush: lang`) into fenced `<pre><code class="language-...">` blocks.
- Client-side rate limiting so you don't trip Outline's `429 Too Many Requests`.
- Optional regex marker that flags migrated pages for manual review.
- `clean` command to wipe a collection (useful when iterating on a migration).

## Requirements

- Go 1.24 or newer
- A Confluence account with API access
- An Outline instance (self-hosted or cloud) and an API token with write access to the target collection

## Installation

Install the latest tagged release with `go install`:

```bash
go install github.com/oskarspakers/confluence-to-outline@latest
```

Or build from source:

```bash
git clone https://github.com/oskarspakers/confluence-to-outline.git
cd confluence-to-outline
go build -o confluence-to-outline
```

Or run directly without building:

```bash
go run . migrate --from SPACEKEY --to COLLECTION_ID
```

## Configuration

The CLI reads credentials from environment variables. For convenience it also loads an `.env` file in the working directory if present. Copy the example:

```bash
cp .env.example .env
```

Then fill in the values:

| Variable | Description |
| --- | --- |
| `CONFLUENCE_BASE_URL` | Base URL of your Confluence instance, e.g. `https://your-company.atlassian.net/wiki`. |
| `CONFLUENCE_USERNAME` | Username or email for Basic auth. Leave empty for public/anonymous access. |
| `CONFLUENCE_API_TOKEN` | Atlassian API token (or password for Confluence Server). |
| `OUTLINE_BASE_URL` | Outline API base URL, e.g. `https://your-outline.com/api`. Must end in `/api`. |
| `OUTLINE_API_TOKEN` | Outline API token (Outline → Settings → API Tokens). |

## Usage

### Migrate a space into a collection

```bash
confluence-to-outline migrate --from SPACEKEY --to COLLECTION_ID [--mark REGEX]
```

- `--from` — Confluence **space key** (the all-caps segment in `/display/SPACEKEY/...`).
- `--to` — Outline **collection ID** (a UUID).
- `--mark` — optional regex. Any migrated page whose body matches it is listed in `Marked.json` for later manual review.

The command also writes:

- `urlMap.json` — mapping from Confluence URLs to the new Outline URLs.
- `checkURLs.json` — pages that contain link shapes the rewriter couldn't fix cleanly.

### Clean a collection

Removes every document (including drafts) from the given Outline collection. Useful when re-running a migration.

```bash
confluence-to-outline clean --collection COLLECTION_ID
```

### Global flags

| Flag | Default | Description |
| --- | --- | --- |
| `--log` | `info` | Log level (`debug`, `info`, `warn`, `error`). |
| `--outline-rate-limit` | `1000` | Maximum Outline API requests per `--outline-rate-window`. Set to `0` to disable throttling. |
| `--outline-rate-window` | `60` | Rate-limit window in seconds. |

### Rate limiting

Outline's server enforces `RATE_LIMITER_REQUESTS` requests per `RATE_LIMITER_DURATION_WINDOW` seconds (defaults: 1000 / 60) and returns `429 Too Many Requests` once exceeded. This CLI paces every outbound request (including attachment uploads) to stay under that budget.

The flag defaults match Outline's defaults. If your server is configured with stricter limits, lower them to match:

```bash
confluence-to-outline migrate --from WIKI --to <uuid> \
  --outline-rate-limit 500 --outline-rate-window 60
```

If you're still seeing `429`s with the defaults, your instance or an intermediate proxy is likely stricter than the advertised global limit — bisect downwards.

## Finding the IDs you need

**Confluence space key** — the upper-case segment in the URL: `.../display/ENG/Engineering+Home` → `ENG`.

**Outline collection ID** — open the collection, open the browser devtools Network tab, and trigger any action on the collection (for example, starring it). Look for a request body containing `"collectionId": "..."`.

## How it works

1. Fetches the root pages of the Confluence space and walks the children recursively.
2. For each page: exports HTML via Confluence's `body.export_view`, rewrites inline `<img>` sources by downloading the binary and re-uploading it to Outline's attachment endpoint, and normalises Confluence code panels into fenced code blocks.
3. Imports the rewritten HTML into Outline using the documents.import endpoint, preserving parent-child relationships.
4. After all pages are imported, re-reads each document and rewrites intra-space links from the old Confluence URLs to the newly-assigned Outline URLs, using the URL map built during step 3.

## Generating the Outline API client

Outline does not publish a Go client, so the one in `outline/outline.gen.go` is generated from the OpenAPI spec using [`oapi-codegen`](https://github.com/oapi-codegen/oapi-codegen) v2:

```bash
go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
```

To regenerate after updating `outline_openapi_spec3.yml`:

```bash
cd outline
oapi-codegen -package outline -config outline_codegen_config.yml outline_openapi_spec3.yml > outline.gen.go
```

The spec can be downloaded from <https://raw.githubusercontent.com/outline/openapi/main/spec3.yml>.

### Known caveat

The config sets `compatibility.old-merge-schemas: true` because the current Outline spec has `allOf` blocks whose sub-schemas disagree on `nullable`, which v2's new merge path rejects. A side-effect of the v1 merge path is that inline enum types inside `allOf` blocks are not emitted by the generator. The four such types currently used by the spec are hand-declared in [`outline/outline_enums.go`](outline/outline_enums.go). If you regenerate and the list of missing types changes (look for `undefined: …` build errors), update that file.

## Contributing

Bug reports and pull requests are welcome. Please open an issue to discuss larger changes first.

## License

[MIT](LICENSE)
