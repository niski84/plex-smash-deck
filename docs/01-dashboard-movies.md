# Dashboard: movie grid

**New here?** Start with [Getting started](help:00-getting-started.md). For TV setup and playback paths, see [Connecting your TV](help:connecting-your-tv.md) and [Playback and webOS](help:playback-and-webos.md).

The **Dashboard** tab hosts the Plex movie library grid, filters, hover details, and actions that send playback to your chosen client. Behavior below matches the single-page UI in `web/plex-dashboard/index.html` and the HTTP handlers in `internal/plexdash/server.go`.

## Loading the list

- **Load Movies** / **Refresh Movies** calls `GET /api/movies`. The button label reflects whether the browser already has a saved list (see browser cache below).
- **First click** loads full library metadata into the **server’s in-memory cache** (shared with other features such as Discovery). The in-page status line distinguishes “using server cache if available” from a forced Plex pull.
- **Refresh** (when the button reads “Refresh Movies”) requests `GET /api/movies?nocache=1`, which **invalidates** that server-side movie list cache and repopulates it from Plex. Per the control’s tooltip, this does **not** clear TMDB or poster caches.
- **`POST /api/movies/sync-recent`** (“Sync new titles”) merges recently added titles when the server detects a count mismatch with Plex; the UI then reloads the list without forcing `nocache`.

**Browser cache:** After a successful load, the app stores `{ count, movies, savedAt }` in `localStorage` under `plexdash.movies.cache.v4`. On page load, if that entry exists, the grid is filled immediately from it and the status explains the age in minutes; use Refresh to pull from Plex again.

**Server vs Plex hints:** `GET /api/movies/cache-status` drives the small **library cache** line (server snapshot age, title counts, whether the configured library key still matches, etc.).

## Sorting

Sort applies to the **decade-filtered** slice of the full library, after **Load Movies** has run. Options:

| Value        | Meaning |
|-------------|---------|
| `yearDesc`  | Newest year first |
| `yearAsc`   | Oldest year first |
| `ratingDesc`| Highest Plex aggregate rating first |
| `ratingAsc` | Lowest rating first |
| `playsDesc` | Most played |
| `playsAsc`  | Least played |
| `sizeDesc`  | Largest file (by `PartSize`) |
| `sizeAsc`   | Smallest file |
| `random`    | Stable shuffled order (see below) |

Chosen sort and decade are persisted in `localStorage` as `plexdash.movieSort.v1`.

### Random: stable order (localStorage, TTL, library signature)

Random order is **not** reshuffled on every paint. The UI keeps a record in `localStorage` (`plexdash.movieRandomOrder.v1`) with:

- **`epoch`**: when the shuffle was generated  
- **`order`**: Plex `RatingKey` values in shuffled sequence  
- **`libSig`**: a fingerprint of the **current full library**—every title’s `RatingKey`, sorted and concatenated  

That fingerprint must match the loaded library, and the record must be **no older than 24 hours** (`RANDOM_ORDER_TTL_MS`). If either check fails, the app runs a Fisher–Yates shuffle on all rating keys, saves a new `{ epoch, order, libSig }`, and sorts cards by each title’s index in `order`. Titles missing from the map fall back to title sort.

So in practice: **same browser, same library membership, within 24 hours** → Random looks the same even if you switch to another sort and back. A full reload from Plex that changes which keys exist, or waiting past the TTL, produces a new shuffle.

## Decade filter

The **Decade** dropdown limits titles whose **release year** falls in a ten-year span (e.g. 1990 → 1990–1999 inclusive). Options are rebuilt from the loaded library (from 1970s up through at least 2030 and the latest decade that appears in the data). This filter **rebuilds the displayed list** from `moviesLibraryFull`, not only visibility of already-built cards.

## Genres: include and exclude

- **Include any:** OR semantics—if one or more genre chips are active, a title must have **at least one** of those genres (normalized keys, e.g. “Science Fiction”). If **none** are selected, there is no include filter.
- **Exclude if any:** titles that have **any** excluded genre are hidden. Managed from **More genres** (pin ⊘ exclude, etc.).
- Preferences (pinned, hidden, excluded lists) persist in `localStorage` (`plexdash.genreBar.prefs.v1`).

## Search and rating filter

- **Search** (`#movieSearch`): case-insensitive match against a string built from **title, year, actors, and directors**. Debounced (~160 ms) on input. With an active query, the grid **materializes all cards** first so matches are not limited to the initial virtualized chunk; visible cards are **reordered** so title **starts-with** ranks above title **contains**, which ranks above cast/crew-only matches, then by title.
- **Minimum rating:** optional Plex aggregate rating (0–10). Titles **without** a rating are **hidden** when a minimum is set (`movieRatingFilter`).

## Hover detail panel

- **Delay:** the panel is shown after **`plexdashHoverPopupDelayMs()`**, default **750 ms** (overridable in tests via `window.__PLEXDASH_HOVER_POPUP_MS`, or legacy `window.__PLEXDASH_DISC_POPUP_HOVER_MS`).
- **Size:** fixed overlay (`#movieInfoPopup`), wide layout (up to `min(960px, 92vw)`), styled to align with the Discovery hover panel scale.
- **Placement:** **`positionPopup`** prefers **below** the card (with a gap) so the card’s **poster and play overlay stay reachable**; if there is not enough viewport space below, it tries **above**, then **to the right**, then **to the left**, then a clamped position below.
- **Dismiss:** leaving the card starts a short hide timer; moving onto the panel cancels it. **Scrolling the movie grid** hides the panel immediately. Moving the pointer over **another** card can dismiss the panel when the move is interpreted as leaving the anchor card’s neighborhood (mousemove logic with separation / half-card heuristics).
- **Click:** a **click** on the panel in the **capture** phase **dismisses** the panel unless the target is the **hover poster** (poster opens the lightbox instead).
- **Lightbox:** **Poster on the card** (not only the hover image) opens the poster lightbox (`cursor: zoom-in`). The hover poster uses **`nativePixels`** and can **resume** the hover panel after the lightbox closes.

Extra rating rows (OMDb / blended pills) load asynchronously via **`GET /api/omdb-ratings`** when the title has a TMDB id.

## Multi-select and playback

- Each card has a **checkbox** (“Select for playlist”). **Select All** selects every **currently materialized** card in grid order. The button **Play Selected (N)** is enabled when `N > 0`.
- **Target** is the **TV / Client** dropdown (`#clientName`), filled from **`GET /api/players`** (Refresh Players rescans). Playback uses the configured default when unset.
- **Playback path** (`#moviePlayTransport`):
  - **WebOS direct (multi):** queues multiple titles via the native webOS path.
  - **Plex app companion (1 title):** server remote API—**only one** title per request; multi-select is rejected unless you switch transport or trim selection. This option is **hidden** when the only selected player uses an **`ssap://`** URI (LG SSAP / webOS direct target).

**API:** `POST /api/movies/play` with JSON `{ items, clientName, shuffle, transport }` (`items` are stream descriptors derived from each movie’s `RatingKey`, part key, container, size, etc.).

## File size and connectivity coloring

The card shows **file size** when `PartSize` is known. After **`GET /api/connectivity`** runs, the UI compares each movie’s **average bitrate** (from file size and `DurationMillis`, same idea as Plex overall bitrate) to the latest **Plex stream sample** throughput (`plexStream.mbps`).

- **Red (`mc-stream-risk`):** average bitrate **exceeds** the measured sample—tooltip notes possible buffering; UI copy notes real Wi‑Fi paths may differ.
- **Yellow (`mc-stream-warn`):** average bitrate is above **~72%** of the sample (“tight headroom”).
- **Default:** no extra class when sample or movie data is missing, or when the ratio is lower.

The Dashboard tip text also points users to header **P** bars and **Settings → Connectivity** for the underlying sample.
