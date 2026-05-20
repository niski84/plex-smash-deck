# plex-dashboard — Claude Rules

## UI Files — Know Which One You're In

There are TWO frontends. The user works on the **main dashboard only**:

| Frontend | Path | URL | Status |
|----------|------|-----|--------|
| **Main dashboard** ✅ | `web/plex-dashboard/index.html` | `http://localhost:8081/` | Active — this is the one |
| Beta/next (v2) | `web/plex-dashboard-next/` | `http://localhost:8081/beta` | Not in use — ignore unless explicitly asked |

**Never touch `web/plex-dashboard-next/` unless the user says "beta" or "v2".**

## Debugging UI Elements — Do This First

Before changing ANY hover, popup, tooltip, or animation:

1. Get the actual element from the user (class name, id, or HTML snippet)
2. `grep -n "the-class-or-id" web/plex-dashboard/index.html` to find it
3. Trace the event handler from there — don't assume which JS file owns it

The main dashboard is a **single large file** (`index.html`) containing all HTML, CSS, and JS inline. There is no separate JS file for it.

## Hover Popup System

The movie info popup uses `mip-*` CSS classes (`mip-visible`, `mip-title`, etc.).

- Delay is controlled by `plexdashHoverPopupDelayMs()` (returns ms, default 950 → SHOW_MS = 1000ms)
- `_showMovieInfo(cardEl, movie, opts)` — pass `{ immediate: true }` to skip delay (avoid this)
- The now-playing card (`#npCardEntry`, `#npCardPoster`) triggers via mouseenter → `_showMovieInfo`
- Movie grid cards trigger via `syncMovieInfoHotZone` → `_showMovieInfo`

## Static File Serving

`index.html` is served from disk (hot-reloadable). No binary rebuild needed for HTML/CSS/JS changes in the main dashboard — just `POST http://localhost:8100/api/reload?project=plex-dashboard`.
