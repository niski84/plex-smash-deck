/* movie-grid.js — Alpine.js data component for the /beta movie grid
 *
 * Features matched against the original web/plex-dashboard/index.html:
 *   - Multi-select genre filter with OR logic
 *   - Exclude genres (right-click chip)
 *   - More/Less expand for long genre lists
 *   - Persistence to localStorage[plexdash.genreBar.prefs.v1]
 *   - All 12 sort modes from the original
 *   - Search scope (all / actor / director) with rank re-ordering
 *   - File-size on cards with stream-warn / stream-risk colour bands
 */
const PD_GENRE_PREFS_KEY = 'plexdash.genreBar.prefs.v1';
const PD_SORT_PREFS_KEY  = 'plexdash.movieSort.v1';

function genrePrefKey(name) {
  if (!name) return '';
  return String(name).toLowerCase().trim()
    .replace(/sci[-\s]?fi/g, 'science fiction')
    .replace(/\s+/g, ' ');
}

function loadGenrePrefs() {
  try {
    const raw = localStorage.getItem(PD_GENRE_PREFS_KEY);
    if (!raw) return { included: [], excluded: [], pinned: [], hidden: [] };
    const obj = JSON.parse(raw);
    return {
      included: Array.isArray(obj.included) ? obj.included : [],
      excluded: Array.isArray(obj.excluded) ? obj.excluded : [],
      pinned:   Array.isArray(obj.pinned)   ? obj.pinned   : [],
      hidden:   Array.isArray(obj.hidden)   ? obj.hidden   : [],
    };
  } catch (e) {
    return { included: [], excluded: [], pinned: [], hidden: [] };
  }
}

function saveGenrePrefs(prefs) {
  try { localStorage.setItem(PD_GENRE_PREFS_KEY, JSON.stringify(prefs)); }
  catch (e) {}
}

function loadSortPrefs() {
  try {
    const raw = localStorage.getItem(PD_SORT_PREFS_KEY);
    if (!raw) return null;
    return JSON.parse(raw);
  } catch (e) { return null; }
}

function saveSortPrefs(p) {
  try { localStorage.setItem(PD_SORT_PREFS_KEY, JSON.stringify(p)); }
  catch (e) {}
}

function fmtGB(bytes) {
  if (!bytes || bytes <= 0) return '';
  const gb = bytes / 1024 / 1024 / 1024;
  return gb >= 10 ? gb.toFixed(1) + ' GB' : gb.toFixed(2) + ' GB';
}

function streamHintClass(bytes, durationMs) {
  if (!bytes || !durationMs) return '';
  // Estimate average bitrate Mb/s = bytes*8 / (duration_seconds * 1_000_000)
  const seconds = durationMs / 1000;
  const mbps = (bytes * 8) / (seconds * 1000000);
  if (mbps > 25) return 'mc-stream-risk';   // very large file, may buffer
  if (mbps > 15) return 'mc-stream-warn';
  return '';
}

// MPAA / TV content rating ordering, used by sort modes
const CONTENT_RATING_TIER = {
  'g': 0, 'tv-y': 0, 'tv-y7': 0,
  'pg': 1, 'tv-g': 1,
  'pg-13': 2, 'tv-pg': 2, 'tv-14': 3,
  'r': 4, 'tv-ma': 5,
  'nc-17': 6, 'x': 6,
};
function contentRatingTier(s) {
  if (!s) return -1;
  const k = String(s).toLowerCase().trim();
  return CONTENT_RATING_TIER[k] !== undefined ? CONTENT_RATING_TIER[k] : -1;
}

function movieGrid() {
  const sortPrefs = loadSortPrefs() || {};
  const genrePrefs = loadGenrePrefs();

  return {
    // ── Data ────────────────────────────────────────────────────────────────
    movies: [],
    loading: false,
    error: null,

    // ── Filters ────────────────────────────────────────────────────────────
    searchQuery: '',
    searchScope: 'all',          // all | actor | director
    selectedGenres: new Set(genrePrefs.included.map(genrePrefKey)),
    excludedGenres: new Set(genrePrefs.excluded.map(genrePrefKey)),
    pinnedGenres:   genrePrefs.pinned.map(genrePrefKey),
    hiddenGenres:   genrePrefs.hidden.map(genrePrefKey),
    showAllGenres: false,
    sortMode: sortPrefs.sort || 'yearDesc',
    minRating: 0,
    decade: sortPrefs.decade || '',

    // ── Render state ────────────────────────────────────────────────────────
    displayedCount: 60,
    selectedMovie: null,
    popupVisible: false,
    popupAnchor: null,
    selected: new Set(),
    playbackPath: 'direct',
    _hoverTimer: null,
    _hideTimer: null,

    init() {
      const reset = () => { this.displayedCount = 60; };
      this.$watch('searchQuery', reset);
      this.$watch('searchScope', reset);
      this.$watch('sortMode', (v) => {
        reset();
        saveSortPrefs({ sort: v, decade: this.decade });
      });
      this.$watch('minRating', reset);
      this.$watch('decade', (v) => {
        reset();
        saveSortPrefs({ sort: this.sortMode, decade: v });
      });
      // Popup positioning is handled directly in onCardMouseenter via rAF.
      this.load();
      this.$nextTick(() => {
        const sentinel = this.$refs.sentinel;
        if (sentinel) {
          new IntersectionObserver(([e]) => {
            if (e.isIntersecting && !this.loading) this.loadMore();
          }, { rootMargin: '200px' }).observe(sentinel);
        }
      });
    },

    async load(nocache = false) {
      this.loading = true;
      this.error = null;
      try {
        const r = await fetch(nocache ? '/api/movies?nocache=1' : '/api/movies');
        const j = await r.json();
        this.movies = j.data?.movies || j.movies || [];
        this.displayedCount = 60;
      } catch (e) {
        this.error = e.message;
      } finally {
        this.loading = false;
      }
    },

    loadMore() {
      const n = this.filtered.length;
      if (this.displayedCount < n) {
        this.displayedCount = Math.min(this.displayedCount + 60, n);
      }
    },

    // ── Genre bar ──────────────────────────────────────────────────────────
    get allGenres() {
      const counts = new Map();
      for (const m of this.movies) {
        for (const g of (m.Genres || [])) {
          counts.set(g, (counts.get(g) || 0) + 1);
        }
      }
      return [...counts.entries()].sort((a, b) => b[1] - a[1]).map(([g]) => g);
    },

    get genres() { return this.allGenres; },

    get visibleGenres() {
      const all = this.allGenres;
      const pinSet = new Set(this.pinnedGenres);
      const hideSet = new Set(this.hiddenGenres);
      // Excluded genres live in their own row — don't show in the include bar
      const exclSet = this.excludedGenres;
      const pinned = this.pinnedGenres.filter(p => all.some(g => genrePrefKey(g) === p));
      const pinnedDisplay = pinned.map(p => all.find(g => genrePrefKey(g) === p)).filter(Boolean);
      const rest = all.filter(g => {
        const k = genrePrefKey(g);
        return !pinSet.has(k) && !hideSet.has(k) && !exclSet.has(k);
      });
      const head = [...pinnedDisplay, ...rest];
      return this.showAllGenres ? head : head.slice(0, 12);
    },

    get hasMoreGenres() {
      const all = this.allGenres;
      const hideSet = new Set(this.hiddenGenres);
      const exclSet = this.excludedGenres;
      return all.filter(g => !hideSet.has(genrePrefKey(g)) && !exclSet.has(genrePrefKey(g))).length > 12;
    },

    // Maps excluded genre keys back to display names for the Exclude row
    get excludedDisplay() {
      const all = this.allGenres;
      return [...this.excludedGenres].map(key => {
        const match = all.find(g => genrePrefKey(g) === key);
        const name = match || key.split(' ').map(w => w.charAt(0).toUpperCase() + w.slice(1)).join(' ');
        return { key, name };
      });
    },

    get decades() {
      const seen = new Set();
      for (const m of this.movies) {
        if (m.Year) seen.add(Math.floor(m.Year / 10) * 10);
      }
      return [...seen].sort((a, b) => b - a).map(d => d + 's');
    },

    isGenreSelected(g) { return this.selectedGenres.has(genrePrefKey(g)); },
    isGenreExcluded(g) { return this.excludedGenres.has(genrePrefKey(g)); },

    toggleGenre(g) {
      const key = genrePrefKey(g);
      const sel = new Set(this.selectedGenres);
      const exc = new Set(this.excludedGenres);
      if (sel.has(key)) {
        sel.delete(key);
      } else {
        sel.add(key);
        exc.delete(key);
      }
      this.selectedGenres = sel;
      this.excludedGenres = exc;
      this.displayedCount = 60;
      this._persistGenres();
    },

    toggleExclude(g) {
      const key = genrePrefKey(g);
      const sel = new Set(this.selectedGenres);
      const exc = new Set(this.excludedGenres);
      if (exc.has(key)) {
        exc.delete(key);
      } else {
        exc.add(key);
        sel.delete(key);
      }
      this.selectedGenres = sel;
      this.excludedGenres = exc;
      this.displayedCount = 60;
      this._persistGenres();
    },

    clearGenres() {
      this.selectedGenres = new Set();
      this.excludedGenres = new Set();
      this.displayedCount = 60;
      this._persistGenres();
    },

    _persistGenres() {
      saveGenrePrefs({
        included: [...this.selectedGenres],
        excluded: [...this.excludedGenres],
        pinned:   this.pinnedGenres,
        hidden:   this.hiddenGenres,
      });
    },

    // ── Search ──────────────────────────────────────────────────────────────
    _movieMatchesSearch(m, qLower) {
      if (!qLower) return true;
      if (this.searchScope === 'actor') {
        return (m.Actors || []).some(a => String(a).toLowerCase().includes(qLower));
      }
      if (this.searchScope === 'director') {
        return (m.Directors || []).some(d => String(d).toLowerCase().includes(qLower));
      }
      // 'all' = title + year + actors + directors
      const blob = [
        m.Title || '',
        m.Year ? String(m.Year) : '',
        ...(m.Actors || []),
        ...(m.Directors || []),
      ].join(' ').toLowerCase();
      return blob.includes(qLower);
    },

    _searchRank(m, qLower) {
      const title = (m.Title || '').toLowerCase();
      if (title.startsWith(qLower)) return 0;
      if (title.includes(qLower)) return 1;
      return 2;
    },

    // ── Filter pipeline ─────────────────────────────────────────────────────
    get filtered() {
      let list = this.movies;

      // Genre filter: exclude takes priority, then OR-include
      if (this.excludedGenres.size > 0) {
        list = list.filter(m => {
          const gs = (m.Genres || []).map(genrePrefKey);
          for (const g of gs) if (this.excludedGenres.has(g)) return false;
          return true;
        });
      }
      if (this.selectedGenres.size > 0) {
        list = list.filter(m => {
          const gs = (m.Genres || []).map(genrePrefKey);
          for (const g of gs) if (this.selectedGenres.has(g)) return true;
          return false;
        });
      }

      // Min rating
      if (Number(this.minRating) > 0) {
        list = list.filter(m => (m.Rating || 0) >= Number(this.minRating));
      }

      // Decade
      if (this.decade) {
        const start = parseInt(this.decade, 10);
        list = list.filter(m => m.Year >= start && m.Year <= start + 9);
      }

      // Search
      const q = this.searchQuery.trim().toLowerCase();
      if (q) {
        list = list.filter(m => this._movieMatchesSearch(m, q));
      }

      // Sort (12 modes from the original)
      list = [...list];
      const titleAsc = (a, b) => (a.Title || '').localeCompare(b.Title || '');
      const yearDesc = (a, b) => (b.Year || 0) - (a.Year || 0);
      const ratingDesc = (a, b) => (b.Rating || 0) - (a.Rating || 0);

      switch (this.sortMode) {
        case 'yearDesc':
          list.sort((a, b) => (b.Year || 0) - (a.Year || 0) || ratingDesc(a, b) || titleAsc(a, b));
          break;
        case 'yearAsc':
          list.sort((a, b) => (a.Year || 0) - (b.Year || 0) || ratingDesc(a, b) || titleAsc(a, b));
          break;
        case 'plexAddedDesc':
          list.sort((a, b) => (b.AddedAtEpoch || b.addedAt || 0) - (a.AddedAtEpoch || a.addedAt || 0) || yearDesc(a, b) || titleAsc(a, b));
          break;
        case 'ratingDesc':
          list.sort((a, b) => (b.Rating || 0) - (a.Rating || 0) || yearDesc(a, b) || titleAsc(a, b));
          break;
        case 'ratingAsc':
          list.sort((a, b) => (a.Rating || 0) - (b.Rating || 0) || yearDesc(a, b) || titleAsc(a, b));
          break;
        case 'playsDesc':
          list.sort((a, b) => (b.ViewCount || 0) - (a.ViewCount || 0) || yearDesc(a, b) || titleAsc(a, b));
          break;
        case 'playsAsc':
          list.sort((a, b) => (a.ViewCount || 0) - (b.ViewCount || 0) || yearDesc(a, b) || titleAsc(a, b));
          break;
        case 'sizeDesc':
          list.sort((a, b) => (b.PartSize || 0) - (a.PartSize || 0) || yearDesc(a, b) || titleAsc(a, b));
          break;
        case 'sizeAsc':
          list.sort((a, b) => (a.PartSize || 0) - (b.PartSize || 0) || yearDesc(a, b) || titleAsc(a, b));
          break;
        case 'contentRatingDesc':
          list.sort((a, b) => contentRatingTier(b.ContentRating) - contentRatingTier(a.ContentRating) || yearDesc(a, b) || titleAsc(a, b));
          break;
        case 'contentRatingAsc':
          list.sort((a, b) => contentRatingTier(a.ContentRating) - contentRatingTier(b.ContentRating) || yearDesc(a, b) || titleAsc(a, b));
          break;
        case 'random':
          // Seeded shuffle, persisted 24h
          list = this._shuffleSeeded(list);
          break;
        case 'title':
          list.sort(titleAsc);
          break;
      }

      // Search rank re-order (overrides sort when query is active)
      if (q) {
        list.sort((a, b) => this._searchRank(a, q) - this._searchRank(b, q));
      }
      return list;
    },

    _shuffleSeeded(list) {
      const out = [...list];
      // 24h persistent seed
      const ttl = 24 * 60 * 60 * 1000;
      let seed;
      try {
        const stored = JSON.parse(localStorage.getItem('plexdash.movieRandomSeed') || 'null');
        if (stored && Date.now() - stored.ts < ttl) seed = stored.seed;
      } catch (e) {}
      if (!seed) {
        seed = Math.floor(Math.random() * 1000000);
        try { localStorage.setItem('plexdash.movieRandomSeed', JSON.stringify({ seed, ts: Date.now() })); } catch (e) {}
      }
      let s = seed;
      for (let i = out.length - 1; i > 0; i--) {
        s = (s * 9301 + 49297) % 233280;
        const j = Math.floor((s / 233280) * (i + 1));
        [out[i], out[j]] = [out[j], out[i]];
      }
      return out;
    },

    get displayed() {
      return this.filtered.slice(0, this.displayedCount);
    },

    // ── Card helpers ────────────────────────────────────────────────────────
    thumbUrl(ratingKey) {
      return '/api/plex/thumb?ratingKey=' + encodeURIComponent(ratingKey);
    },
    fmtDuration(ms) {
      if (!ms) return '';
      const m = Math.round(ms / 60000);
      return m >= 60 ? Math.floor(m / 60) + 'h ' + (m % 60) + 'm' : m + 'm';
    },
    fmtGB(bytes) { return fmtGB(bytes); },
    streamHintClass(m) { return streamHintClass(m.PartSize, m.DurationMillis); },

    // ── Multi-select ─────────────────────────────────────────────────────────
    toggleSelect(ratingKey) {
      const s = new Set(this.selected);
      if (s.has(ratingKey)) s.delete(ratingKey);
      else s.add(ratingKey);
      this.selected = s;
    },
    isSelected(ratingKey) { return this.selected.has(ratingKey); },
    selectAll() {
      const s = new Set(this.selected);
      for (const m of this.filtered) s.add(m.RatingKey);
      this.selected = s;
    },
    clearSelected() { this.selected = new Set(); },
    get selectedCount() { return this.selected.size; },

    async playSelected() {
      const keys = [...this.selected];
      if (keys.length === 0) return;
      const items = this.movies.filter(m => keys.includes(m.RatingKey));
      try {
        const r = await fetch('/api/movies/play', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            items: items.map(m => ({
              ratingKey: m.RatingKey,
              partKey: m.PartKey,
              container: m.FileContainer || 'mp4',
              title: m.Title + (m.Year ? ' (' + m.Year + ')' : ''),
              partSize: m.PartSize || 0,
            })),
            shuffle: true,  // original always shuffles batch play
          }),
        });
        const j = await r.json();
        if (!j.success) throw new Error(j.error || 'play failed');
        this.clearSelected();
      } catch (e) {
        alert('Play failed: ' + e.message);
      }
    },

    // ── Hover popup ──────────────────────────────────────────────────────────
    onCardMouseenter(event, movie) {
      clearTimeout(this._hideTimer);
      clearTimeout(this._hoverTimer);
      const anchor = event.currentTarget;
      this._hoverTimer = setTimeout(() => {
        this.selectedMovie = movie;
        this.popupAnchor = anchor;
        this.popupVisible = true;
        // Double rAF: first rAF is before paint, second ensures layout is complete
        requestAnimationFrame(() => requestAnimationFrame(() => this._positionPopup(anchor)));
      }, 500);
    },
    onCardMouseleave() {
      clearTimeout(this._hoverTimer);
      this._hideTimer = setTimeout(() => {
        this.popupVisible = false;
        this.selectedMovie = null;
        this.popupAnchor = null;
      }, 280);
    },
    onPopupMouseenter() { clearTimeout(this._hideTimer); },
    onPopupMouseleave() {
      this._hideTimer = setTimeout(() => {
        this.popupVisible = false;
        this.selectedMovie = null;
        this.popupAnchor = null;
      }, 280);
    },

    _positionPopup(anchor) {
      const popup = document.getElementById('mg-movie-popup');
      if (!popup || !anchor) return;
      const rect = anchor.getBoundingClientRect();
      const vpw = window.innerWidth;
      const vph = window.innerHeight;
      const pw = popup.offsetWidth || 400;
      const ph = popup.offsetHeight || 320;

      let left = rect.right + 10;
      let top = rect.top;
      if (left + pw > vpw - 10) left = rect.left - pw - 10;
      if (left < 10) left = 10;
      if (top + ph > vph - 10) top = Math.max(10, vph - ph - 10);

      popup.style.left = left + 'px';
      popup.style.top = top + 'px';
    },

    // ── Single-movie play ──────────────────────────────────────────────────
    async playMovie(movie) {
      if (!movie) return;
      try {
        const r = await fetch('/api/movies/play', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            items: [{
              ratingKey: movie.RatingKey,
              partKey: movie.PartKey,
              container: movie.FileContainer || 'mp4',
              title: movie.Title + (movie.Year ? ' (' + movie.Year + ')' : ''),
              partSize: movie.PartSize || 0,
            }],
            shuffle: false,
          }),
        });
        const j = await r.json();
        if (!j.success) throw new Error(j.error || 'play failed');
        this.popupVisible = false;
      } catch (e) {
        alert('Play failed: ' + e.message);
      }
    },

    // Open stream in a new browser tab / native video player
    playInBrowser(movie) {
      if (!movie) return;
      window.open('/api/stream/' + encodeURIComponent(movie.RatingKey), '_blank');
    },

    // Open poster lightbox with optional fanart gallery
    openLightbox(movie) {
      if (!movie) return;
      const src = this.thumbUrl(movie.RatingKey);
      window.lightbox?.open(src, { alt: movie.Title, ratingKey: movie.RatingKey });
    },
  };
}
