# 📚 Anna's Archive API

> Programmatic access to Anna's Archive metadata ; powered by torrents.

Anna's Archive doesn't have an API. Most people work around this by scraping, but honestly, I'm not a fan of that approach. What I *do* love is torrents ; so this project takes Anna's public metadata torrents, indexes them into PostgreSQL, and serves everything through a clean REST API.

## How it works

**Anna's Archive Torrents** → **Download & Extract** → **PostgreSQL** → **This API**

On startup, the service looks for the latest metadata torrent from Anna's Archive. If there's something new, it downloads and streams the compressed files, then parses all the records (only ePub files, all others are ignored) with their identifiers (ISBN, DOI, MD5…) and classifications (Dewey, LCC…) into PostgreSQL.

Statistics are cached in memory so API responses stay fast. The whole thing runs again every 24 hours to stay up to date.

## Good to know

Only ePub files, this check can be removed and/or made configurable, feel free to open a PR if this doesn't suits your needs!

On a fresh start (first sync ever), the API will return **503** until the initial sync is done: there's simply no data to serve yet. After that, syncs happen quietly in the background.

## Under the hood

- **Go** with [Huma](https://huma.rocks) for OpenAPI-first routing
- **PostgreSQL** + GORM for storage
- **BitTorrent** for grabbing data without relying on any central server
- **OpenTelemetry** for tracing
