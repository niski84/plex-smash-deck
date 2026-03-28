# Discovery

**New here?** Read [Getting started](help:00-getting-started.md) first. Discovery needs a **TMDB** key (and optional **OMDb** for blended ratings); see **Settings** in [Snapshots, settings & troubleshooting](help:03-snapshots-settings-troubleshooting.md).

Discovery helps you find **TMDB movies** you might care about—by person (actor/director credits), by production company, or by release year window—and **cross-references them with your Plex movie library**. Rows show whether each title is already in the library (and optional playlist state in person mode). TMDB supplies titles, years, ratings, genres, overviews, and posters; Plex is only used as a **snapshot** of what you already have, not as the source of the candidate list (except for collaborator hints, which are derived from your library metadata).

## Modes (high level)

| Mode | What TMDB provides | How it relates to Plex |
|------|--------------------|-------------------------|
| **By Actor / Director** | That person’s movie credits from TMDB (`movie_credits`), optionally filtered by role, year range, min rating, optional director/co-actor filters, optional playlist comparison | Each row is matched to the library via TMDB id, IMDb id (via TMDB), or title/year (with tolerance) |
| **By Studio** | TMDB discover for a resolved production company, with optional year range, genres, min rating, optional US theatrical filter | Same library matching as above |
| **By year & TMDB rating** | TMDB `/discover/movie` for a primary release year range (English original language in the API query); optional genres, min vote average, optional US theatrical filter | Same library matching |

Server-side filtering (shared themes across modes) drops things like documentaries and TV-movie-style categories, very short features, non–English-original titles when known, unreleased (future-dated) titles, and optional non-theatrical titles when that option is enabled. Exact rules are implemented in `internal/plexdash/discovery.go` and summarized in the Discovery tab help text in the UI.

## API keys

- **TMDB** — **Required** for Discovery. Person search, filmography, discover lists, genres, posters, and external ids all go through TMDB. Without a key, analysis cannot run.
- **OMDb** — **Optional**. If **Blend OMDb + TMDB ratings** is enabled in Settings **and** an OMDb API key is set, the **vote average** used for filtering, sorting, and the TMDB column becomes the **average of TMDB’s vote average (0–10) and OMDb’s IMDb-style rating** when OMDb returns a usable score for that title’s IMDb id. If blending is off or OMDb data is missing, the value stays TMDB-only. (The Settings copy also describes Dashboard hover behavior that uses OMDb for extra scores; that is separate from the Discovery table but uses the same key and disk cache.)

## Long-running analysis: start and poll

The Discovery tab avoids holding one HTTP request open for the whole analysis. It uses:

1. **`POST /api/discovery/start`** — Body JSON includes `mode` (`person` default, `studio`, or `browse`), plus the fields that match the form (person, role, company, years, min rating, genre ids, exclude-non-theatrical, etc.). The response is **202** with `{ "jobId": "…" }` (inside the usual API `data` wrapper).
2. **`GET /api/discovery/poll?jobId=…`** — Returns `state` (`running`, `done`, or `error`), a human `message` while running, and on success a `result` object with `items`, counts, and mode-specific fields (`company`, `browseLabel`, `cache` stats for person mode, etc.).

From a user perspective: **start the job, then poll until it finishes**. The web UI polls about **every 2 seconds** and shows the server’s progress `message` until `state` is `done` or `error`.

## Results table: sort, cart, Markdown, Radarr

- **Sort** — Click column headers to sort. Sortable columns include **#**, **Title**, **Year**, **Genres**, **TMDB** (vote average), **In library**, **In playlist**, **Known for**. Toggling the same column flips ascending/descending.
- **Selection** — Checkboxes per row; **Select all missing** checks rows not in the library. Default for new rows tends toward “missing” selected (see UI behavior).
- **Cart** — **Add to cart** (per row or bulk) stores items in **browser local storage** (`plexdash.discovery.cart.v1`). The header shows a count; **Clear cart** empties it. **Copy cart (Markdown)** copies cart lines (includes TMDB id in the Markdown).
- **Copy Markdown (not cart)** — **Copy missing as Markdown** copies titles/years for rows not in the library. **Copy all titles (Markdown)** copies every row in the **current table order** (not necessarily “missing” only).
- **Radarr** — **Add to Radarr** appears on each row’s actions only when **Radarr integration is enabled** in Settings. **Add selected to Radarr** sends all checked rows to **`POST /api/discovery/radarr/add`**. If integration is disabled, the UI blocks the action and the API returns an error. Adding requires configured Radarr URL, API key, and root folder on the server.

## Disk caches and “Clear TMDB cache”

- **TMDB Discovery cache** — Under **`data/tmdb-discovery-cache/`** (relative to the server process). Stores TMDB-backed discovery data (person ids, filmographies, movie details, external ids, genre maps, browse/studio discover pages, company resolution, etc.) with TTLs defined in code.
- **OMDb cache** — Under **`data/omdb-cache/`**. OMDb responses are stored as files prefixed with **`rating-`** and **`full-`** (see `RemoveAllOMDbCache` in `internal/plexdash/omdb.go`).

**Clear TMDB cache** on the Discovery tab calls **`POST /api/discovery/cache/invalidate`**, which removes **everything under** `data/tmdb-discovery-cache/` **and** clears the OMDb cache files described above. It does **not** clear the in-memory Plex movie list; refresh that from the Dashboard tab (or your normal snapshot flow) if you need a fresh library snapshot.

## Hover popup (title and poster)

After you hold the pointer on a row’s **title** or **poster** for about **750 ms** (default), a floating panel shows a larger poster (when available), title, and overview. You can move the pointer onto the panel; clicking the image can open a larger/lightbox view. Automated tests can override the delay via `window.__PLEXDASH_HOVER_POPUP_MS` (or the legacy `__PLEXDASH_DISC_POPUP_HOVER_MS`). Poster images may use **`/api/discovery/poster`** to proxy TMDB paths when needed.

## Related API routes (reference)

Other Discovery-related endpoints used by the UI include **`/api/discovery/person-suggest`**, **`/api/discovery/collaborators`**, **`/api/discovery/tmdb-genres`**, and (synchronous) **`/api/discovery/studio`** / **`/api/discovery/filmography`** for non-background callers. The main Discovery tab analysis flow is **`/api/discovery/start`** + **`/api/discovery/poll`** as above.
