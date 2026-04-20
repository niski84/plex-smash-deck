# Beta Feature Parity — Plex Dashboard

> **Purpose.** This document is the spec for bringing the beta UI (`/beta`,
> templ + Alpine) to feature parity with the original (`/`,
> `web/plex-dashboard/index.html`, ~10,800 lines). The original is the source
> of truth — every behavior, animation, and stat described below already exists
> there. Use this as the checklist to close the gap.
>
> **Status legend**
> - ✅ **Done** — beta matches the original
> - 🟡 **Partial** — beta has a stub or simplified version; details in the entry
> - ❌ **Missing** — beta does not have it at all
>
> Where helpful, each entry cites the original element ID, JS function name, or
> CSS class so it can be cross-referenced against `web/plex-dashboard/index.html`.

---

## Table of Contents

1. [Design System](#1-design-system)
2. [Layout & Chrome](#2-layout--chrome)
3. [Dashboard — Hero Banner](#3-dashboard--hero-banner)
4. [Dashboard — Now Playing & Player Card](#4-dashboard--now-playing--player-card)
5. [Dashboard — Movie Grid](#5-dashboard--movie-grid)
6. [Dashboard — Movie Hover Popup](#6-dashboard--movie-hover-popup)
7. [Dashboard — Poster Lightbox](#7-dashboard--poster-lightbox)
7b. [TV Shows Tab](#7b-tv-shows-tab--new-in-beta--not-in-v1) *(new)*
8. [Playlists Tab](#8-playlists-tab)
9. [Discovery Tab](#9-discovery-tab)
10. [Snapshots Tab](#10-snapshots-tab)
11. [Settings Tab](#11-settings-tab)
12. [Help Tab](#12-help-tab)
13. [Cross-Cutting Features](#13-cross-cutting-features)
14. [Implementation Roadmap](#14-implementation-roadmap)

> **Architecture note (2026-04-09):** `index.html` was refactored from a ~1250-line
> monolith into a ~150-line shell. Each tab's HTML lives in
> `web/plex-dashboard-next/static/tabs/<name>.html` and is lazy-loaded on first
> activation via `Alpine.initTree(el)`. New JS components go in
> `web/plex-dashboard-next/static/<name>.js` and are included in the `<head>` of
> `index.html`. This keeps individual files manageable as the feature set grows.

---

## 1. Design System

The original's "look" comes from a small set of patterns repeated everywhere.
Get these right and 80% of the visual gap closes.

### 1.1 Color palette

| Token            | Dark theme | Light theme | Used for                           |
|------------------|------------|-------------|------------------------------------|
| `--bg-1`         | `#1a1e30`  | `#d4dff5`   | radial gradient stop 0% (top)      |
| `--bg-2`         | `#0d101a`  | `#eaf0fb`   | radial gradient stop 45%           |
| `--bg-3`         | `#080a10`  | `#f4f7ff`   | radial gradient stop 100% (bottom) |
| `--text`         | `#eee`     | `#1a1a2e`   | body text                          |
| `--card-bg`      | `#1a1a1a`  | `#ffffff`   | card surface                       |
| `--card-border`  | `#333`     | `#c0cfe0`   | card border                        |
| `--heading`      | `#c9d2f0`  | `#2d4080`   | h1–h5 inside cards                 |
| `--muted-color`  | `#aaa`     | `#666`      | secondary text                     |

**Body background recipe** (applies to both themes via custom-property swap):

```css
body {
  background: radial-gradient(circle at top,
    var(--bg-1) 0%,
    var(--bg-2) 45%,
    var(--bg-3) 100%);
}
```

**Accent / status colors** (hard-coded everywhere):

| Use            | Hex                 |
|----------------|---------------------|
| Success / OK   | `#4ade80`, `#34d399`|
| Warning        | `#facc15`, `#eab308`|
| Error          | `#f87171`, `#fecaca`|
| Skip / muted   | `#64748b`, `#94a3b8`|
| Plex blue      | `#7ca0ff → #4d6ee0` (gradient)|
| Plex blue alt  | `#4f7cff`, `#60a5fa` |
| Cyan           | `#a5f3fc`, `#38bdf8` |
| Gold / orange  | `#ffd24f → #f49a17` (gradient), `#fbbf24`, `#fb923c`|
| Purple         | `#a78bfa`, `#ddd6fe` |
| Teal (TMDB)    | `#01d277`, `#86efac` |

**Status:**
- ✅ Navy palette installed (zinc scale overridden in `web/styles/input.css`)
- ✅ Radial body gradient
- ✅ Custom DaisyUI `plex-dark` theme
- ❌ Light theme support (original toggles via `body.light` class — beta is dark-only)

### 1.2 Typography

| Property        | Value                                |
|-----------------|--------------------------------------|
| Font family     | `"Trebuchet MS", Arial, sans-serif`  |
| Mono            | (none — uses sans for monospace too) |
| Default size    | `13px`                               |
| Card heading    | `14px`, weight `600`                 |
| Tab buttons     | `13px`, weight `700`, uppercase, `letter-spacing: 0.5px` |
| Micro text      | `10px`, `9px`, `8px`                 |
| Settings labels | `letter-spacing: 0.05em–0.14em`, uppercase |

**Status:**
- ❌ Font family — beta uses Inter, original uses Trebuchet MS. **This single
  change has a huge effect** on the "feel" of the UI. Either switch beta to
  Trebuchet MS or accept Inter as a deliberate modernisation.
- ❌ Body default `13px` — beta inherits Tailwind's `16px` base. The original
  feels denser because most text is one notch smaller.

### 1.3 Buttons — the squishy effect

This is the single biggest "feel" difference between original and beta. Every
non-trivial button in the original uses a 3D bevel + press animation. Recipe:

```css
button {
  /* Shape */
  position: relative;
  overflow: hidden;
  border: 2px solid #1a1a1a;
  border-radius: 999px;
  padding: 8px 14px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.5px;

  /* The 3D look = inset highlight + inset shadow + drop shadow */
  background: linear-gradient(180deg, #ffd24f 0%, #f49a17 100%);  /* gold */
  color: #201607;
  box-shadow:
    inset 0 2px 0 rgba(255,255,255,0.30),  /* top highlight */
    inset 0 -3px 0 rgba(0,0,0,0.30),       /* bottom shadow */
    0 4px 0 #9a5f0b;                       /* drop shadow = the "pillar" */

  /* Snap timing */
  transition: transform 0.08s ease, filter 0.12s ease;
}

button:hover { filter: brightness(1.06); }

/* PRESS — this is the squish */
button:active {
  transform: translateY(1px);             /* sink 1px */
  box-shadow:
    inset 0 2px 0 rgba(255,255,255,0.30),
    inset 0 -2px 0 rgba(0,0,0,0.30),
    0 3px 0 #9a5f0b;                      /* pillar shrinks 4→3 */
}
```

**Color variants** (same recipe, different gradient + drop-shadow color):

| Variant       | Background gradient                          | Pillar color |
|---------------|----------------------------------------------|--------------|
| Gold (primary)| `#ffd24f → #f49a17`                          | `#9a5f0b`    |
| Red (tabs)    | `#ff5e5e → #d41919`                          | `#6f0d0d`    |
| Blue (play)   | `#7ca0ff → #4d6ee0`                          | `#344fae`    |
| Green (DL)    | `#34d399 → #059669`                          | `#065f46`    |

**Click ripple animation** (`smash-bounce`):

```css
@keyframes smash-bounce {
  0%   { transform: translateY(0)   scale(1);    }
  40%  { transform: translateY(3px) scale(0.96); }
  75%  { transform: translateY(-1px) scale(1.03);}
  100% { transform: translateY(0)   scale(1);    }
}
@keyframes smash-ring {
  0%   { transform: translate(-50%,-50%) scale(0);   opacity: 0.75; }
  100% { transform: translate(-50%,-50%) scale(5.8); opacity: 0;    }
}

button.smash-active {
  animation: smash-bounce 220ms cubic-bezier(0.2, 0.9, 0.2, 1);
}
button.smash-active::after {
  /* Pseudo-element for the ripple ring */
  content: ""; position: absolute; left: 50%; top: 50%;
  width: 14px; height: 14px; border-radius: 999px;
  background: rgba(255,255,255,0.35);
  transform: translate(-50%,-50%) scale(0);
  pointer-events: none;
  animation: smash-ring 280ms ease-out;
}
```

JS: on `click`, add `.smash-active`, then `setTimeout(() => removeClass, 280)`.

**Status:**
- 🟡 Tab buttons — red gradient added, but `:active` press is missing and
  bottom-shadow pillar uses `box-shadow` not the multi-stop original
- ❌ Gold/blue/green button variants — beta uses DaisyUI flat buttons
- ❌ `smash-bounce` + `smash-ring` animations — not present
- ❌ The `:active` translateY-1px squish on every button

**To implement:**
1. Add `.btn-gold`, `.btn-red`, `.btn-blue`, `.btn-green` classes in
   `web/styles/input.css` using the recipe above
2. Add the `@keyframes smash-bounce` and `@keyframes smash-ring` blocks
3. In `web/plex-dashboard-next/static/buttons.js` (new file), add a global
   click delegate that toggles `.smash-active` for 280ms on every `<button>`
4. Replace `btn btn-primary` calls in the templates with the new classes

### 1.4 Cards & panels

```css
.card {
  background: var(--card-bg);
  border: 1px solid var(--card-border);
  border-radius: 8px;
  padding: 14px;
  margin-bottom: 14px;
}
```

Movie card specifically:
```css
.movie-card {
  background: #12151f;
  border: 2px solid transparent;
  border-radius: 6px;
  transition: border-color 0.12s, box-shadow 0.12s;
  content-visibility: auto;
  contain-intrinsic-size: auto 200px;
}
.movie-card:hover { border-color: #2d3d6a; }
.movie-card.mc-selected {
  border-color: #4f7cff;
  box-shadow: 0 0 12px rgba(79, 124, 255, 0.30);
}
```

**Status:**
- ✅ Movie card hover border (zinc-600 override)
- ✅ Movie card selected glow (`.movie-card-selected` class added)
- ❌ `content-visibility: auto` for grid render perf — beta should add this

### 1.5 Inputs & form elements

Default input/select:
```css
input, select {
  padding: 8px 10px;
  border-radius: 6px;
  border: 1px solid #444;
  background: #1e1e1e;
  color: #eee;
}
```

Settings inputs are tighter and more deliberate:
```css
.settings-input {
  background: #101318;
  border: 1px solid #3d4a63;
  border-radius: 6px;
  padding: 8px 10px;
  font-size: 13px;
}
.settings-input:focus {
  border-color: #5c7ab8;
  box-shadow: 0 0 0 1px rgba(92, 122, 184, 0.35);
}
```

Movie search:
```css
.movies-search {
  padding: 6px 10px;
  background: #0d0f18;
  border: 1px solid #333;
  border-radius: 6px;
}
.movies-search:focus { border-color: #4f7cff; }
```

**Status:**
- 🟡 Beta uses DaisyUI `input input-bordered` everywhere — close enough but
  the focus ring is DaisyUI's default (purple ring), not the blue 1px ring

### 1.6 Badges, chips, pills

**Genre chip** (`.movie-genre-tag`):
```css
.movie-genre-tag {
  border-radius: 999px;
  padding: 3px 9px;
  font-size: 9px;
  font-weight: 600;
  border: 1px solid #3d4558;
  background: linear-gradient(180deg, #1c2130 0%, #141824 100%);
  color: #b8c0d8;
  box-shadow: inset 0 1px 0 rgba(255,255,255,0.06);
  transition: border-color 0.12s, color 0.12s, box-shadow 0.12s, filter 0.12s;
}
.movie-genre-tag:hover { border-color: #5a6ba8; color: #e8ecff; }
.movie-genre-tag.is-active {
  background: linear-gradient(180deg, #7ca0ff 0%, #4d6ee0 100%);
  color: #0f1428;
  border-color: #3d56b8;
  box-shadow: inset 0 1px 0 rgba(255,255,255,0.35), 0 2px 0 #344fae;
}
.movie-genre-tag--exclude {
  border-color: #7f1d1d;
  background: linear-gradient(180deg, #2a1818 0%, #1a1214 100%);
  color: #fca5a5;
}
```

**Status badges** (`.np-card-badge`):
```css
.np-card-badge.playing  { background: rgba(52,211,153,0.18); color: #34d399; }
.np-card-badge.paused   { background: rgba(251,191,36,0.18); color: #fbbf24; }
.np-card-badge.local    { background: rgba(96,165,250,0.18); color: #60a5fa; }
.np-card-badge.finished { background: rgba(148,163,184,0.14); color: #94a3b8; }
```

**Connectivity dots:**
```css
.conn-dot { width: 10px; height: 10px; border-radius: 50%; }
.conn-dot.ok    { background: #4ade80; box-shadow: 0 0 6px rgba(74,222,128,0.45); }
.conn-dot.warn  { background: #facc15; }
.conn-dot.error { background: #f87171; box-shadow: 0 0 6px rgba(248,113,113,0.40); }
.conn-dot.skip  { background: #64748b; opacity: 0.88; }
```

**Status:** ❌ All chip/badge styles need porting to beta. Currently the beta
uses DaisyUI `badge` and `btn-xs` which look generic.

### 1.7 Animations & transitions

Inventory of every named keyframe in the original:

| Name           | Where used                       | Spec |
|----------------|----------------------------------|------|
| `smash-bounce` | every button click               | 220ms cubic-bezier(.2,.9,.2,1) |
| `smash-ring`   | button click (ripple ring)       | 280ms ease-out |
| `pulse-glow`   | download/preload buttons         | 1.5s ease-in-out infinite |
| `lb-in`        | poster lightbox image entry      | 0.18s ease |
| `gridSpinAnim` | grid spinner                     | 1s linear infinite |

Standard transitions:

| Element        | Transition |
|----------------|-----------|
| Buttons        | `transform 0.08s ease, filter 0.12s ease` |
| Cards          | `border-color 0.12s, box-shadow 0.12s`    |
| Chips          | `border-color 0.12s, color 0.12s, filter 0.12s` |
| Theme toggle   | `background 0.2s, color 0.2s`             |
| Inputs         | `border-color 0.15s`                      |

**Status:** ❌ All five keyframes missing in beta.

---

## 2. Layout & Chrome

### 2.1 Header / topbar

Original DOM (lines 2020–2039):

```html
<div class="hero-banner" id="heroBanner">…</div>
<div class="tabs-row">
  <div class="tabs" role="tablist">
    <button class="tab-btn active">Dashboard</button>
    <button class="tab-btn">Playlists</button>
    …
  </div>
  <div id="connectivitySummary" class="connectivity-summary">…</div>
  <button id="themeToggleBtn">🌙 Dark</button>
</div>
<div id="nowPlayingBar" class="now-playing-bar" role="status">
  <span id="nowPlayingText">—</span>
</div>
```

Layout:
- Hero banner is **above** the tabs row, full width
- Tabs row is `display:flex; justify-content:space-between; flex-wrap:wrap`
- Tabs left, connectivity right of tabs (in the same row), theme toggle far right
- Nothing is sticky in the original — it scrolls with the page

**Status:**
- 🟡 Beta has tabs row + connectivity but layout differs (tabs left,
  connectivity far right, no center grouping)
- ❌ Hero banner above the tabs row (beta has no hero banner)
- ❌ Now playing bar below the tabs row (beta has only the player card inside
  the dashboard panel)
- 🟡 Beta header is sticky with backdrop-blur — original is not. (Might be
  worth keeping the sticky as a quality-of-life win.)

### 2.2 Tabs bar

Tab list (in order): **Dashboard, Playlists, Discovery, Snapshots, Settings, Help**.

Active state: `class="active"` on the button. Persisted to
`localStorage['plexdash.mainTab.v1']` via `activateMainTab()`.

Custom event: `document.dispatchEvent(new CustomEvent('tabChanged', {detail:name}))`
fires on every switch — used by snapshots/connectivity to refresh on tab focus.

**Status:**
- ✅ All six tabs present
- ✅ Persistence (beta uses `pd-tab` localStorage key)
- 🟡 Tab buttons have red gradient now but missing the squishy press
- ❌ `tabChanged` custom event for downstream tabs to refresh themselves

### 2.3 Theme toggle

Button `#themeToggleBtn`:
```css
#themeToggleBtn {
  background: transparent;
  border: 1px solid rgba(255,255,255,0.18);
  color: var(--text);
  padding: 5px 12px;
  border-radius: 999px;
  font-size: 13px;
}
#themeToggleBtn:hover { background: rgba(100,140,255,0.12); }
```

Behavior:
- Stores `'light'` or `'dark'` in `localStorage['plexdash.theme']`
- Toggles `body.light` class
- Icon: `🌙 Dark` when in dark, `☀ Light` when in light

**Status:**
- 🟡 Beta has a theme toggle but it only flips the Tailwind `dark` class,
  doesn't affect anything because the beta has no light styles defined

### 2.4 Connectivity widget

This is much more than a single badge in the original. Five letter-coded signal
groups, each with three vertical bars:

```
I•P•T•O•L
^ ^ ^ ^ ^
| | | | LG TV (SSAP probe)
| | | OMDb (optional)
| | TMDB
| Plex (`/identity` endpoint)
Internet
```

Each letter is a 3-bar phone-signal indicator (1–3 bars), color-coded:
- 3 bars green = healthy
- 2 bars yellow = warn
- 1 bar red = error
- 0 bars (gray) = skip / not configured

Hover (or click) opens a tooltip with:
- per-service latency in ms
- error message if any
- last check time
- bandwidth `~7.0 Mb/s to Plex` (when known)

**Original CSS** (signal bars):
```css
.conn-signal-bars { display: flex; flex-direction: column; gap: 1px; }
.conn-signal-bar  { width: 100%; height: 3px; border-radius: 1px; }
```

**Polling:** `pollConnectivity()` every `12 * 1000` ms.
**Endpoint:** `GET /api/connectivity` returns:
```json
{
  "updatedAt": "...",
  "overall": "ok|warn|error",
  "summary": "~7.0 Mb/s to Plex; OMDb skipped",
  "checks": {
    "internet": { "status":"ok", "ms":42, "message":"" },
    "plex":     { "status":"ok", "ms":12, "message":"" },
    "tmdb":     { "status":"ok", "ms":120, "message":"" },
    "omdb":     { "status":"skip", "message":"no api key" },
    "lgtv":     { "status":"warn", "ms":300, "message":"slow" }
  },
  "plexSpeed": { "mbps":7.0, "msTotal":1200, "message":"sample run" }
}
```

**Status:**
- 🟡 Beta polls `/beta/fragment/conn-badge` and shows just one dot + label
  ("Connected" / "Plex offline")
- ❌ Five-service signal bars
- ❌ Tooltip with per-service details
- ❌ Bandwidth display
- ❌ Click → details panel

### 2.5 Back-to-top button

Two variants in original:

**`#backToTop`** — circular blue button, fixed bottom-right:
```css
#backToTop {
  position: fixed; bottom: 28px; right: 28px;
  width: 42px; height: 42px; border-radius: 50%;
  background: #2a3a6a; color: #c8d4ff;
  border: 1px solid #3d52a0;
  opacity: 0; pointer-events: none;
  transition: opacity 0.2s;
}
#backToTop.btt-visible { opacity: 1; pointer-events: auto; }
```

**`.jump-to-top-fab`** — alternate FAB style, always at z-index 9990:
- 48×48 round, blue border, glassy background, slide-up entry animation

Click → `window.scrollTo({top:0, behavior:'smooth'})`. Visibility tied to
`window.scrollY > 400`.

**Status:** ✅ Implemented as `#btt` in `index.html` + `web/styles/input.css`. Shows at `scrollY > 400`, smooth-scrolls to top on click. (Uses the `#backToTop`-style circular FAB — the `.jump-to-top-fab` alternate is not added.)

---

## 3. Dashboard — Hero Banner

`<div class="hero-banner" id="heroBanner">` — large image strip above the tabs.

**Sources** (priority order, in `loadHeroBanner()`):
1. Custom URL from `currentSettings.HeroBannerURL`
2. Fanart.tv via `GET /api/branding/fanart-banner` (if `FanartEnabled`)
3. Fallback: most-watched library title via `GET /api/branding/banner-thumb`
4. Placeholder gradient if all fail

**Configurable fields** (in Settings → Branding):
- `HeroBannerURL` — direct override
- `HeroBannerHeight` — px, range 80–420, default 140
- `HeroBannerHidden` — boolean
- `BannerArtRefresh` — fetch interval: `5m / 10m / 30m / 1h / 3h / 8h / 24h / 48h / 1w / once`
- `BannerRotateInterval` — rotate-to-different-title interval: same options

**DOM:**
```html
<div class="hero-banner" id="heroBanner">
  <img id="heroBannerImg" class="hero-banner-img" />
  <div class="hero-banner-overlay">
    <span id="appDisplayName">plex-smash-deck</span>
    <span id="heroBannerMovieLine">Now playing: …</span>
  </div>
</div>
```

**CSS:**
```css
.hero-banner {
  position: relative;
  margin: 0 0 14px 0;
  border-radius: 12px;
  border: 1px solid #2d3b64;
  background: #0e1322;
  overflow: hidden;
  display: none;
}
.hero-banner.is-visible { display: block; }
.hero-banner.hero-banner--placeholder {
  background: linear-gradient(145deg, #1e2840 0%, #0e1322 45%, #161d32 100%);
}
.hero-banner-overlay {
  position: absolute; inset: auto 0 0 0;
  background: linear-gradient(to bottom, rgba(0,0,0,0.1), rgba(0,0,0,0.4));
  padding: 16px;
  color: #fff;
}
```

**Hover behavior:** When fanart matches a real movie, hovering the banner opens
the movie hover popup (`window._showMovieInfo(movie, anchorRect)`).

**Status:** ❌ Entire hero banner missing in beta.

---

## 4. Dashboard — Now Playing & Player Card

The original has **two** UI surfaces for "what's playing":

1. **`#nowPlayingBar`** — single-line status bar at the top of the dashboard
   ```html
   <div id="nowPlayingBar" class="now-playing-bar" role="status" aria-live="polite">
     <span id="nowPlayingText">▶ Inception (2010)</span>
   </div>
   ```
   - Idle: shows `—`
   - Playing: `▶ Title (Year)`
   - Paused: `⏸ Title (Year)`
   - Sent to TV: `📺 Sent to TV`
   - Finished: `⏹ Finished`

2. **`.np-card-entry`** — mini-card under the Target Player dropdown:
   - Poster thumbnail (46×68 px)
   - Status badge (`.np-card-badge.playing` etc.)
   - Title with year
   - Time `12m / 120m · 108m left`
   - Progress bar `.np-card-progress-bar` (2–3 px tall)

**Polling:** `playbackPollTimer` every `8 * 60 * 1000` ms (8 minutes — yes, that's
long; the original assumes you trigger refreshes manually).

**Endpoint:** `GET /api/playback/status` returns:
```json
{
  "primaryFrom": "plex_session|local_send|none",
  "plexSessions": [{ "title":"...", "player":"...", "state":"playing|paused", "progressPercent":42.5 }],
  "localSend": { "sentAt":"...", "titles":[...], "ratingKeys":[...], "target":"...", "queueLength":3 },
  "stale": false
}
```

**Local send queue:** When you "Send to TV", the server tracks that queue
locally with timestamps. Card shows:
- Elapsed time since send
- Remaining time (estimated from movie runtime)
- "stale" warning if elapsed > 105% of runtime

**Companion controls** (`.companion-controls`):
- Buttons: ⏮ Prev, ⏪ Step back, ⏸ Pause, ▶ Play, ⏹ Stop, ⏩ Step fwd, ⏭ Next
- Seek box: numeric input + "Seek" button
- Hidden when player uses direct protocol (SSAP/Roku) — controlled by
  `updatePlexCompanionUI()`
- API: `POST /api/plex/companion/control` with `{action, offset}`

**Player selector:**
- `<select id="clientName">` populated by `loadPlayers()` (`GET /api/players`)
- "Refresh Players" button next to it
- Persists choice to `currentSettings.TargetClientName` via Save

**Status:**
- ❌ `#nowPlayingBar` (single-line top bar)
- ✅ Player card with target/status/title/progress
- 🟡 Card poster shows correctly but progress bar is 1–2px (original is 3px)
- 🟡 Companion controls present but missing seek input
- 🟡 Player selector present but no "Refresh Players" button
- ❌ Local send queue stale indicator with elapsed/remaining math
- ❌ "Sent to TV" / "Finished" badge variants
- ❌ Hide companion controls when player uses direct protocol
- ❌ Polling interval — beta polls every 60s vs original's 8min (this is fine,
  beta is actually nicer here)

---

## 5. Dashboard — Movie Grid

This is the heart of the dashboard and where the beta is furthest from parity.

### 5.1 Movie card content

**Original `buildMovieCard()`** at line 4092 produces:

```
┌────────────────────┐
│ ☐ ← checkbox       │  ← top-left, always visible
│                    │
│      POSTER        │  aspect 2/3
│       2:3          │
│                    │
│         ▶          │  ← play overlay, opacity 0 → 1 on hover
│                    │
├────────────────────┤
│ Title (ellipsis)   │  11px, color #d8dcef
│ 2010      2.3 GB   │  10px year + GB (color-coded)
└────────────────────┘
```

Fields rendered:
- **Poster** — `/api/plex/thumb?ratingKey=X`, lazy-loaded, `mc-loading` class
  while loading, `display:none` on error
- **Checkbox** — `.movie-card-check` always visible top-left, syncs with
  `selectedMovieKeys` Set
- **Play overlay** — `▶` glyph, fades in on hover, `pointer-events:none` until
  hover so it doesn't block underlying clicks
- **Title** — `movie.Title`, `text-overflow:ellipsis`, `title=...` for tooltip
- **Year** — `movie.Year`
- **File size** — `formatGB(movie.PartSize)` (e.g., "2.45 GB")
  - `.mc-stream-warn` (yellow) when > a threshold
  - `.mc-stream-risk` (red) when very large (likely buffering)
  - `title="File size: 2.45 GB · stream: comfortable"` etc.
  - Computed by `applyStreamHintToMovieSizeEl()`

**Status:**
- ✅ Poster, title, year, basic checkbox
- ✅ Play overlay added (recent commit)
- ❌ **File size with stream-warn/stream-risk colors** — user explicitly
  flagged this as missing
- ❌ Always-visible checkbox (beta has opacity-0 → group-hover)
- ❌ Title attribute with file size + stream hint
- ❌ `mc-loading` opacity-0.4 fallback while image loads
- ❌ Image error fallback (`display:none` on `<img>`)

### 5.2 Filter controls

The original has **eight** filter mechanisms, all combinable (AND across types,
OR within multi-select):

#### A. Search (`#movieSearch`)
- Type: `search` input, placeholder `"Search by title, actor, or director…"`
- **Search scope** (radio group `name="movieSearchScope"`):
  - `all` (default) — searches title + year + actors + directors
  - `actor` — searches only the actors list
  - `director` — searches only the directors list
- Substring match (`.includes(qLower)`)
- When a query is active, results are **re-ranked**:
  - Rank 0: title starts with query
  - Rank 1: title contains query (anywhere)
  - Rank 2: only matched in actors/directors
  - Ties broken by `localeCompare(title)`
- Debounced 160 ms

**Functions:** `filterMovieCards()`, `movieMatchesDashboardSearch()`,
`buildMovieSearchBlob()`, `searchMatchRank()`

**Status:**
- ✅ Basic search input
- ❌ **Search scope radios** (all/actor/director)
- ❌ Search rank re-ordering when query is active
- 🟡 Beta filters on title only (via simple filter), not the title+year+actors+directors blob

#### B. Genre filter — multi-select + exclude

The user explicitly called this out. The original has a sophisticated genre bar
with **three** sections:

```
┌─────────────────────────────────────────────────────────┐
│ Genres                                                  │
│ Include any: [Action] [Comedy] [Thriller] [Adventure]   │
│              [Sci-Fi]   [More genres ▼] [Clear]         │
│                                                         │
│ ─── Exclude if any: [⊘ Horror] [⊘ Documentary] ───      │
└─────────────────────────────────────────────────────────┘
```

**Behavior:**
- **Click a genre chip** → toggle into `selectedDashboardGenres` Set (multi-select)
- **OR logic** — a movie passes if it has *any* selected genre
- **Click − next to chip** → remove from main bar, move to "hidden" list
- **Expand "More genres ▼"** → shows hidden genres + every other library genre
  - Each row has: genre name, **+** (pin to main bar), **⊘** (move to excludes)
- **Excluded genres** appear as separate red-styled chips below
  - Click − on an excluded chip to remove from excludes
- **Filter logic** (`moviePassesGenreFilters()`):
  1. If movie has any excluded genre → hide
  2. Else if no genres selected → show
  3. Else if movie has any selected genre → show
  4. Else hide

**Persistence:** localStorage key `plexdash.genreBar.prefs.v1`:
```json
{
  "pinned":   ["action", "comedy"],
  "hidden":   ["western"],
  "excluded": ["horror", "documentary"],
  "included": ["action"]
}
```
Genre names are normalized via `genrePrefKey()` (lowercase, "sci-fi" → "science fiction").

**Defaults** when no prefs: `["Action", "Comedy", "Thriller", "Adventure", "Science Fiction"]`.

**Functions:** `buildDashboardGenreTags()`, `moviePassesGenreFilters()`,
`onGenreBarPin()`, `onGenreBarExcludeFromResults()`, `removeExcludedGenrePrefKey()`,
`saveGenreBarPrefs()`, `loadGenreBarPrefs()`

**Status:**
- ✅ Multi-select with OR logic
- ✅ Exclude functionality — ⊘ button (always visible, dimmed) on each chip; excluded genres appear in a red "Exclude" row below; click × to remove exclusion
- ✅ More/Less expand (shows top 12, "More ▾" expands all)
- ✅ Persistence to localStorage (`plexdash.genreBar.prefs.v1`)
- ❌ Pin to main bar (original: `+` button in More panel, `−` to remove)
- ❌ Main bar limited to 12 by count; original uses curated default seeds list then shows the rest in More panel

#### C. Decade filter (`#movieDecadeSelect`)
- Dropdown populated dynamically from library years
- Options: "All decades" + each decade present (`1980s`, `1990s`, etc.)
- Logic: `decade ≤ year ≤ decade + 9`

**Status:** ✅ Present in beta (`decade` Alpine field).

#### D. Min rating filter (`#movieRatingFilter`)
- Dropdown: `Any | 9.0+ | 8.0+ | 7.0+ | 6.0+ | 5.0+ | 4.0+`
- Hides titles with no rating or `Rating < min`

**Status:** ✅ Present (`minRating` Alpine field) — beta has more granular options
including 7.5 / 8.5 which is fine.

#### E. Sort dropdown (`#movieSortSelect`)

**Twelve** sort modes:

| Value              | Order               | Tiebreaker chain                 |
|--------------------|---------------------|----------------------------------|
| `yearDesc` (default)| Year newest first   | rating desc → title asc         |
| `plexAddedDesc`    | Most recently added | year desc → title asc           |
| `yearAsc`          | Year oldest first   | rating desc → title asc         |
| `ratingDesc`       | Highest rating      | year desc → title asc           |
| `ratingAsc`        | Lowest rating       | year desc → title asc           |
| `playsDesc`        | Most played         | year desc → title asc           |
| `playsAsc`         | Least played        | year desc → title asc           |
| `sizeDesc`         | Largest file        | year desc → title asc           |
| `sizeAsc`          | Smallest file       | year desc → title asc           |
| `contentRatingDesc`| Strictest first     | unrated last; year desc → title |
| `contentRatingAsc` | Mildest first       | unrated last; year desc → title |
| `random`           | Seeded shuffle      | persists 24h in localStorage    |

Persistence: `localStorage['plexdash.movieSort.v1']` stores `{sort, decade}`.
Function: `sortMoviesList()`, applied inside `renderMovies()`.

**Status:**
- 🟡 Beta has 5 sort modes (added/title/rating/year_desc/year_asc) — missing
  plays, size, contentRating, random

### 5.3 Multi-select & batch actions

- **Selection** — checkbox per card, stored in `selectedMovieKeys` Set
- **Select all** (`#movieSelectAllBtn`):
  - Toggles: if all visible filtered are selected → deselect all
  - Otherwise selects all in `currentDisplayList()` (respects active filters)
- **Play Selected** (`#moviePlaySelectedBtn`):
  - Label: `▶ Play Selected (N)`, disabled when N=0
  - Calls `playMovieItems(items, /*shuffle=*/true)` — **always shuffles**
- **Playback path selector** (`#moviePlayTransport`):
  - `webos` — Direct to TV (LG webOS multi-item queue, Roku deep link)
  - `companion` — Plex companion HTTP, one title at a time
  - `browser` — In-browser player

**Status:**
- ✅ Per-card checkbox + select all + play selected
- ✅ Playback path dropdown
- 🟡 No shuffle on multi-play in beta
- ❌ Hide `companion` option when player uses direct protocol

### 5.4 Stats line

`#movieCount` shows:
- No filters: `"384 movies"`
- Filters active: `"42 of 384 movies"`
- Infinite scroll: `"42 of 384 movies · loaded 100 (scroll for more)"`

`updateMovieCountDisplay()` is called after every filter/sort change.

**Status:** 🟡 Beta has a count line but doesn't show "loaded X (scroll for more)".

### 5.5 Infinite scroll

- Initial render: 100 cards (`MOVIE_GRID_INITIAL`)
- Chunk size: 200 (`MOVIE_GRID_CHUNK`)
- Trigger: scroll-listener on `#movieGrid`
- Background prefetch via `requestIdleCallback()` (60 ms timeout fallback)
- Debounced; cancels on filter/sort change

**Status:** ✅ Beta uses IntersectionObserver — equivalent functionality.

### 5.6 Rendering performance

```css
.movie-card {
  content-visibility: auto;
  contain-intrinsic-size: auto 200px;
}
```

**Status:** ❌ Beta should add this.

### 5.7 Card click behavior

- **Click poster** → opens **poster lightbox** (full-screen, supports TMDB
  fanart gallery + zoom + prev/next)
- **Click play overlay** → `playMovieItems([item], false)`
- **Hover (in hot zone)** → opens hover popup after 750 ms

**Status:**
- ✅ Click poster → lightbox (implemented in `lightbox.js` with fanart gallery)
- ✅ Click play overlay (gold ▶ overlay on hover)
- 🟡 Hover popup (present but content/positioning differ — see §6)

### 5.8 Grid CSS (don't break this)

> ⚠️ **Memory note (`feedback_grid_css.md`):** never remove `max-height` or
> `overflow-y` from `.movies-grid` — past incident broke scroll.

```css
.movies-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(108px, 1fr));
  gap: 8px;
  min-height: 200px;
  padding: 2px;
}
```

**Status:** ✅ Beta matches.

### 5.9 Library sync status

**V1 elements**: `#libraryCacheLine` (status line), `#syncRecentMoviesBtn` ("Sync new titles" button)

**Behavior**:
- `GET /api/movies/cache-status` — polls every 10 minutes; also called on page load and after any load/sync
- Shows: `N titles in memory · cached Xm ago · Plex has Y more titles — sync or refresh`
- Amber text when Plex count is higher (delta > 0), rose when lower, normal when in sync
- **Sync new titles** button — enabled only when `deltaVsCache > 0`; calls `POST /api/movies/sync-recent` to merge recently added titles without a full reload
- After sync: shows "Merged N new title(s)." confirmation message

**Status:** ✅ Implemented in beta — `cacheLine` computed getter + `syncNewTitles()` in `movie-grid.js`; "⬇ Sync new" button in controls row; status line above genre bar.

---

## 6. Dashboard — Movie Hover Popup

**Trigger logic** (`window._showMovieInfo()`, line 6846):
- Hover **center band** of poster (35% inset top/bottom — `__PLEXDASH_MOVIE_POSTER_HOT_INSET`)
- Delay 750 ms (`plexdashHoverPopupDelayMs()`)
- Touch tap → toggle popup immediately
- Pointer leaves card → cancel pending popup
- Pointer enters popup → cancel hide timer
- Pointer leaves popup → 800 ms hide timer
- Page scroll → close immediately
- Click outside popup → close immediately

**Positioning** (`#movieInfoPopup`):
- `position: fixed; z-index: 9998`
- Preferred order: right of card → left → below → above
- 14 px overlap with card to prevent dead zone
- 8 px viewport margin
- Width: `min(540px, calc(100vw - 20px))`
- Max height: `min(90vh, 720px)`

**Content displayed** (rich):
- Full-size poster (left side, ~150 px wide, click → lightbox)
- Title and year, large
- Content rating pill (e.g. "Rated PG-13")
- **Combined rating average** (`Avg ★ 7.4`) — only if 2+ rating sources
  - Calculation: trimmed mean (drop highest+lowest if 4+ scores, median if 3)
- **Per-source rating pills** (only when present):
  - `Plex ★ 7.5` (dark blue border)
  - `IMDb 7.8` (yellow border)
  - `Rotten Tomatoes 92%` (red border)
  - `Metacritic 78` (green border)
  - `TMDB 7.4` (teal border)
- **Attributes row** (`.mip-attrs`):
  - Runtime (`1h 45m`)
  - Play count (`12 plays`) if `ViewCount > 0`
  - File container (`MKV` — yellow if MKV/AVI)
  - Directors: clickable buttons → click runs an actor/director discovery
  - **Plex sample speed** (`Plex sample ~12.4 Mb/s`) — measured speed
  - **Title bitrate** (`Title ~8.3 Mb/s avg`) — calc from size/duration
  - **Stream hint** (color-coded): `comfortable` / `tight headroom` / `may buffer`
- Genres (up to 4, joined by ` · `)
- Cast: up to 8 actors, each clickable button → discovery
- Summary / synopsis: up to ~5 lines, expandable with "Show more" button
- Three play buttons (when `PartKey + RatingKey` exist):
  - **Play on TV** (gold) → `playMovieItems([item], false)`
  - **Play in Browser** (blue) → `openBrowserPlayer(item)`
  - **Cache** (green) → `POST /api/stream/preload`, with `pulse-glow` while
    preloading, turns brighter green on success

**OMDb fetch** — async, abortable:
- `GET /api/omdb-ratings?tmdbId=X` or `?imdbId=tt...`
- Aborted if user moves off card before response

**Movie metadata** — fetched on demand:
- `GET /api/movies/hover-meta?ratingKey=X` (for fields not in initial payload)

**Status:**
- ✅ Beta popup with title/genres/summary/director/cast/play buttons
- ✅ Combined rating average (`Avg ★ X.X`) — trimmed mean, shown when 2+ sources
- ✅ Per-source rating pills: IMDb (yellow), RT (red), Metacritic (green), TMDB (teal)
- ✅ Async fetch via `GET /api/omdb-ratings?tmdbId=X` — aborted on hide
- ✅ TMDB ↗ and IMDb ↗ external link badges
- ✅ Runtime shown (`fmtDuration`)
- ✅ Play count (`ViewCount` pill, shown only when > 0)
- ✅ File container pill (`FileContainer` uppercased, e.g. `MKV`)
- ✅ Poster is 280px wide, click → lightbox
- ❌ Stream-hint color coding (comfortable / tight / may buffer)
- ❌ Plex speed sample / title bitrate
- ❌ "Cache" button with pulse-glow
- ✅ Click-to-trigger-discovery on actor/director names — calls `goToDiscovery(name, role)` which switches tab and auto-runs analysis via retry-poll for lazy-loaded tab
- ❌ "Show more" expand for long summaries
- ❌ OMDb async fetch with abort
- ❌ Smart positioning (right→left→below→above)
- 🟡 Hot-zone hover detection (currently just any pointerenter)

---

## 7. Dashboard — Poster Lightbox

`#posterLightbox` — full-screen modal opened by clicking any poster.

**CSS:**
```css
#posterLightbox {
  display: none;
  position: fixed; inset: 0;
  z-index: 9000;
  background: rgba(0,0,0,0.88);
  align-items: center;
  justify-content: center;
  cursor: zoom-out;
}
#posterLightbox.lb-open { display: flex; }
#posterLightbox img {
  max-width: min(1100px, 96vw);
  max-height: min(96vh, 1600px);
  border-radius: 8px;
  box-shadow: 0 8px 48px rgba(0,0,0,0.8);
  animation: lb-in 0.18s ease;
}
```

**Features:**
- Click poster → opens with that image
- TMDB fanart gallery support (if `tmdbId` known) — fetches multiple posters
- **Prev / Next** arrows (`.poster-lightbox-nav`) — keyboard ←/→
- **Counter** badge top-center: `3 / 12`
- **Zoom**: click image → toggle native pixel display (overflow:auto)
- Click backdrop → close
- Esc key → close

**Function:** `openPosterLightbox(thumbUrl, title, opts)`

**Status:** ✅ Implemented in `web/plex-dashboard-next/static/lightbox.js`. Opens via `window.lightbox.open(src, {alt, ratingKey})`. Fetches fanart gallery from `/api/fanart-movie/prefetch?ratingKey=X` and appends items after the poster. Supports ESC to close, ← → keyboard nav, prev/next arrow buttons, counter badge. Click backdrop closes. Zoom (native pixel) not yet implemented.

---

## 7b. TV Shows Tab  *(new in beta — not in v1)*

The original dashboard (`web/plex-dashboard/index.html`) had no separate TV tab.
Shows were accessible only via playlists or the movie grid. The beta adds a
first-class **TV Shows** tab with a full drill-down pattern.

### Architecture

`web/plex-dashboard-next/static/show-grid.js` — `showGrid()` Alpine component.
`web/plex-dashboard-next/static/tabs/tv.html` — tab partial, loaded lazily.

### 7b.1 Show grid

- Loaded from `GET /api/shows` (paged by `TVLibraryKey`)
- Response: array of show objects with `RatingKey`, `Title`, `Year`, `Summary`,
  `Rating`, `ChildCount` (seasons), `LeafCount` (episodes)
- **Grid**: 2:3 poster, title, `ChildCount seasons · LeafCount eps`, year+rating
- **Search** input filters by title (live)
- **Sort**: default year desc; also title A→Z
- **Reload / clear cache** button

### 7b.2 Season drill-down

- Click a show card → `drillIn(show)`
- Fetches `GET /api/seasons?showKey=X`
- Seasons displayed as pill tabs: `S1 · S2 · S3 … Specials`
- Specials (index=0) sorted to end
- `selectSeason(season)` → fetches `GET /api/episodes?seasonKey=X`

### 7b.3 Episode grid

- 16:9 thumbnail (or placeholder if missing)
- Episode number (`S01E03`), title, air year
- Watched pip (green dot when `ViewCount > 0`)
- Progress bar (amber) when partially watched (`ViewOffset > 0`)
- `fmtDuration(ms)` for runtime
- Click → `playEpisode(ep)` which calls `POST /api/movies/play` (same endpoint
  as movies, works for episodes too)

### 7b.4 Navigation

- `back()` clears `currentShow` and returns to the show grid
- "Back to shows" button visible in drill-down view

### 7b.5 Status

- ✅ Show grid with search + sort
- ✅ Season pill tabs
- ✅ Episode grid with watch state
- ✅ Play episode button
- ❌ Multi-episode select + batch play
- ❌ Episode hover popup (actor/director links)
- ❌ "Mark as watched" per-episode action
- ❌ Next-unwatched episode jump (auto-select first unfinished season/episode)

---

## 8. Playlists Tab

`<section id="tab-playlists">` — three sub-sections.

### 8.1 Play from Plex

- **Dropdown** `#playlistSelect` — populated from `GET /api/playlists`
  - Response shape: `{ playlists: [{ Title }] }`
- **Refresh button** `#refreshPlaylistsBtn` → re-runs `loadPlaylists()`
- **Item preview** `#playlistItemsList` — `<ul>` max-height 180 px, scrollable
  - Loaded when playlist selected, via `GET /api/playlists/items?title=X&limit=250`
  - Response: `{ movies: [{ Title, Year }] }`
  - Per-item: `Title (Year)`
- **Play button** `#playSelectedPlaylistBtn`
  - `POST /api/playlists/play` with `{ title, clientName }`
  - Triggers `loadPlaybackStatus()` after 2.5 s
  - Status display: `#playlistResult`

**Status:**
- ✅ Dropdown
- ✅ Item preview
- ✅ Play button
- 🟡 Beta has no "Refresh playlists" button (uses init only)

### 8.2 Build by Actor / Director

`<details class="dash-playlist-build">` — collapsible.

- Inputs: title (default "People Picks"), count (default 8), actor, director
- Datalist: `personSuggestions` (autocomplete)
- Submit: `POST /api/playlists/by-people` with `{title, count, actor, director}`
- Response: `{ Title, Count }`
- Success: `Created "X" with N movies`

**Functions:** `createByPeople()` (line 5352)

**Status:** ✅ Present in beta. Missing only the autocomplete datalist.

### 8.3 Build by Genre / Rating  ← MISSING

- **Genre select** `#genreSelect` — populated from library
- **Min rating select** `#ratingSelect` — `0, 1.0, 1.5, … 9.5` (0.5 steps)
- **Year range** — fixed `1982 – 2016` in original
- **Create button** → `POST /api/playlists/by-genre-rating` with `{genre, minRating}`
  - Response: `{ playlist: {Title, Count} }`
  - Sort order: `viewCount asc, shuffle within ties`
- **Preview button** `#previewGenrePlaylistBtn`
  - `POST /api/playlists/preview` with `{genre, minRating, minYear:1982, maxYear:2016, limit:75}`
  - Returns `{ suggestedTitle, totalMatched, showing, movies:[…] }`
  - Per-item display: `Title (Year) rating [Rating] · [ViewCount] plays`

**Functions:** `createByGenreRating()` (5764), `previewByGenreRating()` (5777)

**Status:** ❌ Entire "Build by Genre/Rating" section missing in beta.

---

## 9. Discovery Tab

This is the most complex tab in the original.

### 9.1 Modes

`<select id="discoverMode">`:
1. **Person** (default) — by actor/director
2. **Studio** — by production company
3. **Browse** — by year + TMDB rating only

### 9.2 Inputs per mode

#### Person mode
- `#discoverPerson` — name, autocomplete `personSuggestions`
- `#discoverRole` — Actor + Director / Actor only / Director only
- `#discoverDirectorFilter` — collaborator director (optional)
- `#discoverCoActorFilter` — collaborator actor (optional)
- `#discoverPlaylistTitle` — existing playlist to compare against (optional)

#### Studio mode
- `#discoverStudio` — autocomplete `studioSuggestions` ("A24, Pixar, …")

#### Browse mode
- No person/studio inputs — date range only

### 9.3 Common filters (all modes)

- `#discoverMinYear`, `#discoverMaxYear` — text inputs
- `#discoverMinRating` — TMDB rating dropdown (0–9.5+, default Any)
- `#discoverGenreIds` — multi-select (Ctrl/Cmd-click), populated from
  `GET /api/discovery/tmdb-genres` → `{ genres: [{id, name}] }`
- `#discoverExcludeNonTheatrical` — checkbox
  - Browse/Studio: filters to US theatrical (release types 2–3)
  - Person: removes direct-to-video when TMDB flags known
  - All modes: filters out documentaries / TV movies

### 9.4 Execution flow

`runDiscovery()` (line 8112):
1. `POST /api/discovery/start` → `{jobId}`
2. Poll `GET /api/discovery/poll?jobId=X` every 2 s → `{state, message, result}`
3. On `state === 'done'`: render results
4. Live status text with ellipsis animation while polling
5. Results stored in `discoveryData[]`

Result envelope:
```json
{
  "items": [{
    "tmdbId": 27205,
    "title": "Inception",
    "year": 2010,
    "voteAverage": 8.4,
    "genres": ["Action","Sci-Fi"],
    "inLibrary": true,
    "inPlaylist": false,
    "knownFor": "Director: Inception (2010)",
    "posterUrl": "https://image.tmdb.org/t/p/w500/...",
    "overview": "..."
  }],
  "total": 42,
  "missing": 7
}
```

### 9.5 Results table

`#discoveryTableWrap` — scrollable table, `max-height: calc(100vh - 380px)`.

Column order: `☐ | # | Cover | Title | Year | Genres | TMDB | In library | In playlist | Known for | Action`

- **Cover column** — `.disc-poster-cell` (64 px wide): renders a **52×78 px** thumbnail
  - Image source priority:
    1. `discoveryPosterProxyFromPath(item.posterPath)` → `/api/discovery/poster?path=…`
    2. Direct TMDB `posterUrl` (retry on proxy 404)
    3. `.disc-poster-missing` placeholder div (`"—"` / `"No art"`)
  - Error fallback: first retries with direct TMDB URL; if that also fails, replaces img with
    `<div class="disc-poster-missing">No art</div>`
  - CSS:
    ```css
    .disc-poster { width:52px; height:78px; object-fit:cover; border-radius:4px;
                   border:1px solid #333; background:#111; cursor:zoom-in; }
    .disc-poster-missing { width:52px; height:78px; display:flex; align-items:center;
                           justify-content:center; font-size:10px; color:#666;
                           border:1px dashed #444; border-radius:4px; background:#141414; }
    ```
- All headers `class="th-sort"` → click to sort, asc/desc toggle; `aria-sort` updated
- Default sort: recommendation # (asc)
- **Sort function:** `sortDiscoveryByColumn(columnKey)` (line 5982)
- "In playlist" column only shown in Person mode (when `discoverPlaylistTitle` set)

**Beta status for the table:** results table exists but the **Cover column is entirely absent** —
beta renders `#`, Title, Year, In Library, TMDB, Genres, Action only.

### 9.6 Poster hover popup

`wireDiscoveryRowPosterHover(tr, idx)` (line 7681) is called for every row after render.

**Trigger zones:** hover the poster thumbnail OR the center 32% band of the title cell.
Delay: **500 ms** (`window.__PLEXDASH_DISCOVERY_HOVER_MS`, default 500).
Touch: tap immediately (no delay).

**Element:** `<div id="discPosterPopup">` — fixed-position, `z-index: 9999`.

**Content displayed:**
1. **TMDB poster** — `w780` size, `max-width: min(1000px, 88vw)`, `max-height: min(720px, 50vh)`, `object-fit: contain`
   - Source: `discPosterBigSrc(src)` upgrades the thumb URL to `?size=w780`
   - "Loading poster…" spinner shown while image loads
   - Cursor: `zoom-in`
   - Click poster → opens image in a **fullscreen view** at original resolution (not the same lightbox as movie grid)
2. **Title** (large, `font-size: 24px`)
3. **Overview / plot** — synopsis text, `max-height: 76vh`, scrollable, `font-size: 24px`, `color: #b8bdd4`

**No fanart** — the discovery popup is TMDB poster + title + overview only. The fanart gallery (used in the dashboard movie hover popup) is NOT present here.

**Popup CSS:**
```css
#discPosterPopup {
  position: fixed; z-index: 9999; pointer-events: none; display: none;
  background: #0d101a; border: 4px solid #3b4a78; border-radius: 16px;
  box-shadow: 0 24px 80px rgba(0,0,0,0.85); padding: 16px 20px 20px;
  opacity: 0; transition: opacity 0.15s ease;
  max-width: min(1040px, 92vw);
}
#discPosterPopup.pp-visible { opacity: 1; pointer-events: auto; }
```

**Close behaviour:** pointer leaves row → hide timer; pointer enters popup → cancel hide.
`✕` close button also present (`class="disc-popup-close-btn"`).

**Functions:**
- `window._showPosterPopup(anchorEl, thumbSrc, title, overview, opts)` (line 6288)
- `window._cancelPendingPosterPopup()` (line 6274)
- `window._hidePosterPopup()` (line 6386)
- `discoveryPosterDisplayFromItem(item)` (line 6034) — resolves poster source
- `discPosterBigSrc(src)` (line 6148) — upgrades URL to w780

### 9.7 Infinite scroll

- `discoveryAppendMoreIfNeeded()` (line 7912)
- Chunk: 48 rows
- Trigger: scroll within 240 px of bottom
- Auto-fill viewport on initial load

### 9.8 Bulk actions

- **Select all missing** `selectAllMissing()` (8206)
- **Add selected to Radarr** `addSelectedToRadarr()` (8216)
  - Requires `currentSettings.RadarrEnabled`
  - `POST /api/discovery/radarr/add` with `{items:[{tmdbId,title,year}]}`
  - Status: `Radarr added X, failed Y`
- **Add to cart** `addSelectedToCart()` (5880)
  - `localStorage['plexdash.discovery.cart.v1']`
  - Per-item: `{tmdbId, title, year, addedAt}`
  - Dedupes by `tmdbId`
- **Cart count badge** `#discoveryCartCount` — live update

### 9.9 Copy actions (Markdown)

All three produce GitHub-flavored Markdown to clipboard:

**`copyMissingAsMarkdown()`** (8029):
```markdown
## Movies to add — Christopher Nolan

*7 title(s) not in library*

- **Tenet** (2020) — Action · TMDB 7.4
  - A Protagonist...
- **Memento** (2000) — Mystery · TMDB 8.4
  - A man with anterograde amnesia...
```

**`copyAllTitlesMarkdown()`** (5851):
```markdown
## All titles — Christopher Nolan

*42 film(s), current table order*

- **Inception** (2010)
- **Interstellar** (2014)
- ...
```

**`copyDiscoveryCartMarkdown()`** (5902):
```markdown
## Cart — Christopher Nolan

*7 item(s) in cart*

- **Tenet** (2020) — TMDB `577922`
- ...
```

### 9.10 TMDB cache

- **Clear button** `#clearTmdbDiscoveryCacheBtn`
- `POST /api/discovery/cache/clear` (deletes `data/tmdb-discovery-cache/`)
- Cache stats shown in status:
  `Cache: person ID from cache; filmography from cache; TMDB movie details 15/20 from disk; cast/crew credits 12/18 from disk`

### 9.11 Status

- ✅ Mode tabs (person/studio/**browse** — renamed from year)
- ✅ Per-mode inputs
- ✅ Year range — **`<select>` dropdowns** (1920–currentYear+1) in browse mode
- ✅ Min TMDB rating filter
- ✅ Exclude non-theatrical checkbox
- ✅ TMDB genre multi-select (Ctrl/Cmd for multiple, populated from `/api/discovery/tmdb-genres`)
- ✅ Director collaborator filter (person mode)
- ✅ Co-actor collaborator filter (person mode)
- ✅ Compare against existing playlist (person mode, populated from `/api/playlists`)
- ✅ Job start + poll loop with animated status
- ✅ Results table with Cover (poster thumbnail), checkbox, #, Title, Year, In Library, TMDB, Genres, Action
- ✅ Select all missing / Add selected to cart
- ✅ Cart with localStorage persistence
- ✅ Copy missing / Copy all titles / Copy cart as Markdown
- ✅ Clear cart button
- ✅ Poster hover popup (via shared `Alpine.store('moviePopup')`)
- ✅ Clear TMDB cache button
- ❌ Sortable columns (click headers)
- ❌ Infinite scroll (currently shows all rows)
- ❌ Add to Radarr button
- ❌ Cache stats in status message

---

## 10. Snapshots Tab

`<section id="tab-snapshots">` — library state tracking over time.

### 10.1 Take snapshot

- **Button** `#takeSnapshotBtn` 📸
- `POST /api/snapshots` (no body) → `{snapshot: {id, count, capturedAt}}`
- Disables button during op
- Auto-syncs Dashboard movie grid after capture
- Success: `✓ Captured N movies (date)`
- Triggers full refresh: snapshots, latest diff, missing banner

**Function:** `takeSnapshot()` (line 10271)

### 10.2 Refresh button

`#snapRefreshBtn` ↻ → `refreshAll(false)` — reloads everything.

### 10.3 History table

Columns: `# | Captured | Movies | Change vs Prev | Action`

- Newest first (index 0 = latest)
- Delta badges:
  - Green `+N` = added
  - Red `−N` = removed
  - Gray `±0` = no change
  - `baseline` for oldest snapshot
- "View Changes" button per row → loads diff into the diff panel
- Empty state: `No snapshots yet. Click "Take Snapshot" to capture your library now.`

**Function:** `renderSnapshotRows(snaps)` (10199)

### 10.4 Latest drop card

`#snap-last-drop` — newest vs previous snapshot.

- `GET /api/snapshots/latest-diff` → `{diff: {from, to, added, removed}}`
- Header: `[FromDate] → [ToDate] (FromCount → ToCount movies)`
- Two columns:
  - **🆕 New Movies** — list with title (year)
  - **🚫 Went Missing** — list with title (year), header turns red if any
- "Need at least 2 snapshots" message if no diff yet

**Pattern analysis** (`renderPatterns('snapDropPatterns')`) — shown if ≥ 2 added:
- Insights grouped by Director, Studio, Actor, Genre, Decade
- Per insight: icon, label, name, count/total %, film preview list
- Source: `GET /api/snapshots/patterns?from=X&to=Y`

### 10.5 All-time missing banner

`#snap-missing-banner` — movies that disappeared at some point.

- `GET /api/snapshots/missing` → `{missing: [{goneId, lastSeenAt, goneAt, movie}]}`
- Grouped by `goneId` (each disappearance event)
- Per group: `Disappeared between [LastSeenDate] → [GoneDate]`
- Lists all titles in that disappearance
- Subtitle: `N disappearance event(s) across your snapshot history`

### 10.6 Manual compare

- Two selects: `#snapCompareFrom`, `#snapCompareTo`
- `#snapCompareBtn` → `loadManualDiff(fromId, toId)` (10298)
- `GET /api/snapshots/diff?from=X&to=Y` → `{diff: {from, to, added, removed, netChange}}`
- Result panel `#snap-diff-panel`:
  - Metadata header with net change (color-coded)
  - Two columns: Added / Removed
  - Pattern analysis if ≥ 2 added
  - Highlighted row in history table (`snap-active` class)

### 10.7 Settings (in Settings tab)

- `cfgSnapshotEnabled` — enable daily snapshots
- `cfgSnapshotHour` — 0–23 UTC, default 2 (2:00 AM UTC)

### 10.8 Status

- ✅ Snapshots list
- ✅ Take snapshot
- ✅ Latest diff card
- ✅ Missing alert (basic)
- ✅ Manual compare
- ❌ Pattern analysis (Director/Studio/Actor/Genre/Decade insights)
- ❌ All-time missing grouped by `goneId` event
- ❌ "View Changes" per-row button to load that diff
- ❌ Snapshot count in compare select labels
- ❌ Highlighted row when diff is shown

---

## 11. Settings Tab

The original has **fourteen** sections — far more than the beta currently exposes.

### 11.1 Plex section

| Field             | Type     | Default | Notes |
|-------------------|----------|---------|-------|
| `cfgPlexBase`     | text     |         | placeholder `https://your-plex-host:32400` |
| `cfgPlexToken`    | password | secret  | with **Copy** button |
| `cfgLibraryKey`   | text     | `1`     | movie library section ID |
| `cfgTVLibraryKey` | text     |         | optional |
| `cfgTargetClient` | select   |         | populated from `/api/players` |
| `cfgAutoTargetDetected` | checkbox | false | "Prefer detected player as default target" |

**Status:** ✅ All fields present in beta.

### 11.2 Branding section

| Field                  | Type     | Default | Notes |
|------------------------|----------|---------|-------|
| `cfgAppDisplayName`    | text     | `plex-smash-deck` | header app title |
| `cfgHeroBannerURL`     | text     |         | direct URL override |
| `cfgHeroBannerHeight`  | number   | `140`   | range 80–420 |
| `cfgHeroBannerHidden`  | checkbox | false   | "Hide banner image" |

**Status:** ✅ All fields present in beta. ❌ Hero banner not yet rendered.

### 11.3 Fanart.tv section

| Field                    | Type     | Default | Notes |
|--------------------------|----------|---------|-------|
| `cfgFanartEnabled`       | checkbox | false   | master switch |
| `cfgFanartAPIKey`        | password | secret  | with **Copy** button |
| `cfgFanartClientKey`     | password | optional | higher rate limits |
| `cfgFanartCacheMaxMB`    | number   | `200`   | range 32–8192 |
| `cfgBannerArtRefresh`    | select   | `1h`    | `5m / 10m / 30m / 1h / 3h / 8h / 24h / 48h / 1w / once` |
| `cfgBannerRotateInterval`| select   | `30m`   | same options |

**Status:** ✅ Most fields present in beta.

### 11.4 TMDB section

| Field              | Type     | Notes |
|--------------------|----------|-------|
| `cfgTMDBKey`       | password | secret + Copy |
| `cfgTMDBReadToken` | password | secret + Copy |

**Status:** ✅

### 11.5 OMDb section

| Field            | Type     | Notes |
|------------------|----------|-------|
| `cfgOMDbKey`     | password | secret + Copy |
| `cfgOMDbBlend`   | checkbox | "Blend OMDb + TMDB ratings in Discovery" |

**Status:** ✅

### 11.6 Radarr section

| Field              | Type     | Notes |
|--------------------|----------|-------|
| `cfgRadarrEnabled` | checkbox | master switch |
| `cfgRadarrURL`     | text     | conditional, hidden when disabled |
| `cfgRadarrAPIKey`  | password | conditional, secret + Copy |
| `cfgRadarrRoot`    | text     | conditional, default `/movies` |
| `cfgRadarrProfile` | text     | conditional, default `1` |

**Visibility:** `applyRadarrVisibility()` (line 8267) hides/shows fields based
on `cfgRadarrEnabled`.

**Status:** ✅ All fields. ❌ Conditional show/hide on enable toggle.

### 11.7 Snapshots section

| Field                | Type     | Default | Notes |
|----------------------|----------|---------|-------|
| `cfgSnapshotEnabled` | checkbox | true    | enable daily snapshots |
| `cfgSnapshotHour`    | select   | `2`     | 0–23 UTC |

**Status:** ✅

### 11.8 Connectivity / Health (read-only)  ← MISSING

Live connectivity status with full table:
- Last update timestamp `#connDetailUpdated`
- Per-service rows from `/api/connectivity` (status dot, name, message, latency)
- **Plex speed check** card showing `mbps`, latency, sample file title
- Refreshes automatically with the connectivity poll

**Plus a chart panel:**
- Metric selector: `plexMbps | health | internetMs | plexMs | tmdbMs | lgMs`
- View selector: `hour | day | week`
- Date picker
- Buttons: Open calendar, Redraw chart, Clear local history
- Chart: bar chart in `#connHistChart` (`.conn-hist-bars`)
- Summary text + Y-axis label

**Status:** ❌ Entire section missing.

### 11.9 Fanart banner cache  ← MISSING

- Status line: `#fanartCacheStatusLine` from `/api/fanart-banner/cache-status`
- "Clear cache" button → `POST /api/fanart-banner/cache/invalidate`
- Activity log: `<pre id="fanartBannerActivityLog">` auto-refreshed every 8 s
  from `/api/fanart-banner/log`

**Status:** ❌ Missing.

### 11.10 Stream cache  ← MISSING

- Refresh button → reloads from `/api/stream/cache`
- "Clear All" button (with confirmation) — deletes every cached stream
- List of cached files: path, size, age, per-file delete button

**Status:** ❌ Missing.

### 11.11 Maintaining caches table  ← MISSING

Cache info table showing every cache directory:

| Cache | Location | Size | Files | Updated | Notes |

- Refresh button reloads from `/api/settings/caches`
- Body: `#settingsCachesBody`
- Working directory shown above table: `#settingsCachesCwd`

**Status:** ❌ Missing.

### 11.12 Dashboard genre bar prefs  ← MISSING

Three textareas (one genre per line) editing the same prefs as the dashboard:
- Pinned genres `#settingsGenrePinned`
- Hidden default genres `#settingsGenreHidden`
- Excluded genres `#settingsGenreExcluded`
- Apply button → saves to localStorage `plexdash.genreBar.prefs.v1`
- Reset button → restores defaults (Action, Comedy, Thriller, Adventure, Sci-Fi)

**Status:** ❌ Missing.

### 11.13 TV devices  ← MISSING

- Device list `#tvDevicesList`
- Add Device button → reveals form
- Device form fields:
  - Display Name `#tvDeviceName`
  - Manufacturer select `#tvDeviceMfr` (`lg` | `roku`)
  - IP Address `#tvDeviceAddr`
  - **LG-specific** (`#tvDeviceLgFields`):
    - SSAP Client Key (optional, set on first pair)
    - IP Control Key (optional, for volume slider)
  - **Roku note** (`#tvDeviceRokuNote`)
  - Save / Cancel buttons + status
- API: `POST /api/tv-devices`, `DELETE /api/tv-devices/X`

**Status:** ❌ Missing.

### 11.14 Save mechanism

- Single **"Save Settings"** button at the bottom
- Status span next to it (`#settingsStatus`): "Saved settings (persisted)." / "Failed: …"
- No auto-save, no dirty tracking, no unsaved-changes warning
- Loads via `GET /api/settings` on tab open
- Saves via `POST /api/settings` with full config object

**Status:** ✅ Beta has the basic save mechanism.

### 11.15 Service icons & color coding

Each section has a colored heading with a small SVG icon:

| Section   | Color (heading)   | Border line       | Icon |
|-----------|-------------------|-------------------|------|
| Plex      | `#f0d080` gold    | `rgba(229,160,13,0.5)` | 🎬 |
| Branding  | `#ddd6fe` purple  | `rgba(167,139,250,0.4)`| 🎨 |
| Fanart.tv | (cyan)            |                   | 🖼  |
| TMDB      | `#86efac` green   | `rgba(1,210,119,0.45)` | 🎬 |
| OMDb      | `#fde047` yellow  | `rgba(234,179,8,0.4)`  | ⭐ |
| Radarr    | `#a5f3fc` cyan    | `rgba(0,180,216,0.45)` | 📥 |
| Connection| `#7dd3fc` sky     | `rgba(56,189,248,0.35)`| 🔌 |
| Data      | `#cbd5e1` slate   | `rgba(148,163,184,0.4)`| 💾 |
| UI        | `#fdba74` orange  | `rgba(251,146,60,0.35)`| 🎛 |

**Status:**
- ✅ Beta has emoji icons + colored headings (recently added)
- ❌ Original uses inline SVG icons (more polished than emoji)
- ❌ Original draws a colored border-bottom line under each section heading

### 11.16 Secret copy buttons

Each `password`-type field has a sibling `<button class="settings-secret-btn" data-secret-copy="fieldId">` that copies the actual unmasked value to the clipboard.

Functions: `getSettingsSecretValue()`, `setSettingsSecretDisplay()` (shows
masked `●●●●●●●●1234` with last 4 visible).

**Status:** ❌ Beta uses plain `<input type="password">` with no copy button.

---

## 12. Help Tab

Simple but worth getting right.

### 12.1 Doc loading

- `<select id="helpDocSelect">` populated from `GET /api/help/docs`
  - Response: `{docs: [{name, title}]}`
- Reload button `#helpReloadBtn`
- Empty state: `"No docs found"` option + message
  `Create markdown files in docs/*.md to populate this Help page.`
- Auto-loads first doc on init

### 12.2 Doc rendering

- `GET /api/help/doc?name=X` → `{markdown, name}`
- Rendering: **custom tiny Markdown parser** (`renderHelpMarkdown(md)` line 9253)

Supported syntax (subset):
- `# H1`, `## H2`, `### H3`
- `**bold**`
- `` `inline code` ``
- ` ```code block``` `
- `[Text](https://...)` (target=_blank)
- `[Text](help:other-doc.md)` — internal doc link, calls `loadHelpDoc(name)`
- `- list item` → `<ul><li>`
- GitHub-style tables: `| col | col |` with separator row

HTML escape applied to all text except inside code blocks.

### 12.3 Internal links

`<a href="#" class="help-doc-link" data-help-name="other.md">` — wired by event
delegate (line 9755) to call `loadHelpDoc(name)`.

### 12.4 CSS

- `.help-doc` container (font, line-height, link color)
- `.help-doc-table-wrap` for responsive tables
- `.help-doc-table` with alternating row backgrounds

### 12.5 Status

- ✅ Doc list dropdown
- ✅ Doc loader
- ❌ **Markdown rendering** — beta dumps raw markdown into `<pre>`, original
  renders proper HTML with headings, lists, tables, code, links
- ❌ Internal `help:` links between docs
- ❌ Reload button (beta has one but it's labelled differently)

---

## 13. Cross-Cutting Features

### 13.1 localStorage keys

| Key | Stored | Used by |
|-----|--------|---------|
| `plexdash.theme`            | `light`/`dark`              | theme toggle |
| `plexdash.mainTab.v1`       | active tab name             | tab persistence |
| `plexdash.movieSort.v1`     | `{sort, decade}`            | movie grid sort/filter |
| `plexdash.genreBar.prefs.v1`| `{pinned, hidden, excluded, included}` | genre bar |
| `plexdash.discovery.cart.v1`| `[{tmdbId, title, year, addedAt}]` | discovery cart |
| `plexdash.movieRandomSeed`  | shuffle seed (24h TTL)      | random sort |

**Status:**
- ✅ `pd-tab` (beta uses different key but works)
- ❌ All other keys

### 13.2 Tab change event

```js
document.dispatchEvent(new CustomEvent('tabChanged', {detail: name}));
```

Listened to by:
- Snapshots tab (refresh on focus)
- Settings → connectivity history (start chart polling)
- Fanart activity log (start 8 s log polling)

**Status:** ❌ Beta uses Alpine `x-show` toggling but doesn't fire any event.

### 13.3 Visibility change

```js
document.addEventListener('visibilitychange', ...);
```

Pauses **all** timers when tab hidden, resumes when visible. Affects:
- `playbackPollTimer` (8 min)
- `connectivityPollTimer` (12 s)
- `lgVolumePollTimer`
- `fanartBannerPollTimer`

**Status:** ❌ Beta polls regardless of visibility.

### 13.4 Common helpers

- `setStatus(elementId, message, ok)` — color-coded status text (green/red)
- `apiJson(path, options)` — fetch wrapper that throws on non-2xx
- `formatGB(bytes)` — `2.45 GB`
- `formatDurationMillis(ms)` — `1h 45m`
- `formatRelativeTime(ts)` — `4 minutes ago`
- `escapeHtml(s)` — for any user-provided string injected as innerHTML

**Status:** Beta has equivalents inside Alpine components but they're not shared.

### 13.5 Keyboard

- `Esc` — closes lightbox, popup, expanded sections
- `←` `→` — lightbox prev/next
- No grid keyboard navigation
- No `/` to focus search

**Status:** ❌ Beta has none.

### 13.6 Touch

- Tap movie card → toggle hover popup
- Tap discovery row poster → show poster popup
- Long-press not used

**Status:** ❌ Beta has neither.

---

## 14. Implementation Roadmap

The gap is large but organised. Tackle in this order — each phase makes the
biggest visible difference for the least effort.

### Phase A — Visual feel (1 day)
Closes the "looks nothing like the original" complaint immediately.

1. Add the **squishy button system** (§1.3) in `web/styles/input.css`:
   - `.btn-gold`, `.btn-red`, `.btn-blue`, `.btn-green` with the bevel recipe
   - `:active` translateY-1px + shadow shrink
   - `@keyframes smash-bounce` and `smash-ring`
   - Tiny `buttons.js` to toggle `.smash-active` on click
2. Update tab buttons to use `.btn-red` so they get the same press animation
3. Update primary action buttons (Play Selected, Save Settings, Build, etc.)
   to use `.btn-gold`
4. Add `:active` press to genre chips and small buttons
5. Switch the body font to **Trebuchet MS** in `@theme`
6. Drop default font size to **13px** in body or via Tailwind base layer
7. Add `content-visibility: auto` to `.movie-card` rule

### Phase B — Movie grid parity (1–2 days)

8. **Multi-select genre filter** (§5.2.B)
   - Change `activeGenre: ''` → `selectedGenres: new Set()` in `movie-grid.js`
   - Update click handler to toggle in/out of the Set
   - Add OR logic in `get filtered()`
9. **Exclude genres**
   - Add `excludedGenres: new Set()`
   - Render a separate red-styled chip row when not empty
   - Filter logic: `if (any excluded match) hide`
10. **More/Less expansion** with pin/exclude buttons
11. **Persistence** — save Sets to `localStorage['plexdash.genreBar.prefs.v1']`
12. **File size on cards** with `mc-stream-warn`/`mc-stream-risk` colors
13. **Search scope radios** (all / actor / director)
14. **Search rank re-ordering** when query active
15. Expand sort dropdown to all 12 modes

### Phase C — Movie hover popup (1 day)

16. Combined rating average across sources
17. Per-source rating pills with colored borders
18. ✅ Runtime, play count, file container — implemented
19. ✅ "Play in Browser" button — implemented; ❌ "Cache" button still missing
20. ✅ Click-actor / click-director → run a discovery — implemented via `goToDiscovery()`
21. "Show more" expand for long summaries
22. OMDb async fetch with abort
23. Smart positioning (right → left → below → above)
24. ✅ **Poster lightbox** with fanart gallery — implemented (`lightbox.js`)

### Phase D — Chrome (1 day)

24. **Hero banner** above tabs row, plus banner rotation poll (§3)
25. **Now-playing bar** single-line above tabs
26. **Connectivity widget** signal-bar variant (§2.4)
27. ✅ **Back-to-top FAB** — implemented (`#btt`)

### Phase E — Tab gaps (2 days)

28. **Playlists**: Build by Genre/Rating + preview
29. **Discovery**:
    - **Cover column** — add `.disc-poster-cell` with 52×78 img; proxy via
      `/api/discovery/poster?path=…`; error-retry with direct TMDB URL; "No art" fallback
    - **Poster hover popup** — `#discPosterPopup` fixed panel; 500 ms delay on poster/title
      hover; fetch w780 poster via `discPosterBigSrc()`; show title + overview; click → fullscreen;
      touch tap immediately; `_showPosterPopup` / `_hidePosterPopup`
    - TMDB genre multi-select
    - Sortable columns (click headers → `sortDiscoveryByColumn`)
    - Infinite scroll (48-row chunks, `discoveryAppendMoreIfNeeded`)
    - Add to Radarr button
    - "Copy all titles" Markdown
30. **Snapshots**: Pattern analysis, all-time missing grouped by event
31. **Settings**: Connectivity panel, fanart cache panel, stream cache panel,
    cache table, genre bar prefs, TV devices, secret copy buttons
32. **Help**: Real markdown rendering (use `marked` or a tiny custom parser)

### Phase F — Polish (1 day)

33. `tabChanged` custom event + `visibilitychange` polling pause
34. Keyboard shortcuts (Esc, arrows)
35. Touch tap behaviors
36. Light theme support (or formally drop it)

---

## Appendix: Quick Reference

### Key element IDs in original (cross-reference)

| Tab        | IDs to look up in `web/plex-dashboard/index.html` |
|------------|---------------------------------------------------|
| Header     | `#heroBanner`, `.tabs-row`, `#connectivitySummary`, `#themeToggleBtn`, `#nowPlayingBar` |
| Dashboard  | `#movieGrid`, `#movieSearch`, `#movieSortSelect`, `#movieDecadeSelect`, `#movieRatingFilter`, `#movieGenreTags`, `#movieGenreExcludeTags`, `#movieGenreMoreRow`, `#moviePlayTransport`, `#moviePlaySelectedBtn`, `#movieSelectAllBtn`, `#movieCount`, `#movieInfoPopup`, `#posterLightbox` |
| Player     | `#nowPlayingText`, `#npCardPoster`, `#npCardBadge`, `#npCardTitle`, `#npCardTime`, `#clientName` |
| Playlists  | `#playlistSelect`, `#playlistItemsList`, `#playlistResult`, `#peopleResult`, `#genreSelect`, `#ratingSelect`, `#previewResult` |
| Discovery  | `#discoverMode`, `#discoverPerson`, `#discoverStudio`, `#discoverGenreIds`, `#discoveryTableWrap`, `#discoveryCartCount` |
| Snapshots  | `#takeSnapshotBtn`, `#snapRefreshBtn`, `#snap-last-drop`, `#snap-missing-banner`, `#snapCompareFrom`, `#snapCompareTo`, `#snap-diff-panel` |
| Settings   | `#cfgPlexBase`, `#cfgPlexToken`, `#cfgFanartEnabled`, `#cfgRadarrEnabled`, `#saveSettingsBtn`, `#settingsStatus`, `#connHistChart`, `#streamCacheList`, `#tvDevicesList`, `#settingsCachesBody` |
| Help       | `#helpDocSelect`, `#helpDocContent`, `#helpReloadBtn` |

### Key JS function names in original

| Function | Line | Purpose |
|----------|------|---------|
| `buildMovieCard()`        | 4092 | Render a single grid card |
| `filterMovieCards()`      | 4781 | Apply all filters + search |
| `moviePassesGenreFilters()`| 4689 | Genre OR logic + exclude |
| `sortMoviesList()`        | 4429 | All 12 sort modes |
| `renderMovies()`          | 4883 | Sort + filter + paint |
| `materializeMoreMovies()` | 4636 | Infinite scroll chunk |
| `playMovieItems()`        | 4025 | Batch play (TV / companion / browser) |
| `_showMovieInfo()`        | 6846 | Open hover popup |
| `loadHeroBanner()`        | 3334 | Pick + render hero image |
| `loadHelpDocs()`          | 9314 | Help tab init |
| `renderHelpMarkdown()`    | 9253 | Custom MD → HTML |
| `runDiscovery()`          | 8112 | Start + poll discovery job |
| `addSelectedToRadarr()`   | 8216 | Bulk Radarr add |
| `takeSnapshot()`          | 10271 | Snapshot capture |
| `loadManualDiff()`        | 10298 | Compare any two snapshots |
| `saveSettings()`          | 9391 | POST settings form |
| `pollConnectivity()`      | (in 8615 block) | 12 s connectivity poll |
| `applyTheme()`            | 10781 | Light/dark switch |
