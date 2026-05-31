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
const PD_GENRE_PREFS_KEY    = 'plexdash.genreBar.prefs.v1';
const PD_SORT_PREFS_KEY     = 'plexdash.movieSort.v1';
const PD_SEARCH_HISTORY_KEY = 'plexdash.dashboard.searchHistory.v1';
const PD_SEARCH_HISTORY_MAX = 25;

function pdLoadSearchHistory() {
  try { return JSON.parse(localStorage.getItem(PD_SEARCH_HISTORY_KEY) || '[]'); } catch { return []; }
}
function pdSaveSearchToHistory(q) {
  if (!q || q.length < 2) return;
  let h = pdLoadSearchHistory().filter(x => x.toLowerCase() !== q.toLowerCase());
  h.unshift(q);
  try { localStorage.setItem(PD_SEARCH_HISTORY_KEY, JSON.stringify(h.slice(0, PD_SEARCH_HISTORY_MAX))); } catch {}
}

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

    // ── Collection filter (set when user clicks a collection link in the popup) ──
    collectionFilter: null,  // { name: string, ratingKeys: Set<string> } | null

    // ── Render state ────────────────────────────────────────────────────────
    displayedCount: 60,
    selected: new Set(),
    playbackPath: 'direct',
    _suggTimer: null,
    _histSaveTimer: null,
    searchSuggestions: [],
    searchHistory: pdLoadSearchHistory(),

    // ── TMDB resolve (smart search for sequels / alternate titles) ─────────────
    resolveResults: [],
    resolveLoading: false,
    _resolveTimer: null,

    // ── Library sync status ──────────────────────────────────────────────────
    cacheStatus: null,   // data from /api/movies/cache-status
    syncing: false,
    syncMsg: '',
    _cachePollTimer: null,

    get cacheLine() {
      const d = this.cacheStatus;
      if (!d) return '';
      if (!d.plexConfigured) return 'Configure Plex in Settings to enable library sync.';
      if (!d.cachedCount) return 'No movie list loaded — click Refresh to load from Plex.';
      if (!d.cacheKeyMatches) return 'Library key changed — Refresh to resync.';
      const age = d.cachedAtISO ? (() => {
        const secs = Math.round((Date.now() - new Date(d.cachedAtISO).getTime()) / 1000);
        if (secs < 90) return 'just now';
        if (secs < 3600) return Math.round(secs / 60) + 'm ago';
        return Math.round(secs / 3600) + 'h ago';
      })() : '';
      const local = d.cachedCount.toLocaleString() + ' titles in memory' + (age ? ' · cached ' + age : '');
      if (d.remoteCountError) return local + ' · could not read Plex count: ' + d.remoteCountError;
      if (d.plexRemoteCount == null) return local;
      const delta = d.deltaVsCache;
      if (delta == null) return local;
      if (delta > 0) return local + ` · Plex has ${delta} more title${delta===1?'':'s'} — sync or refresh`;
      if (delta < 0) return local + ` · ${Math.abs(delta)} title${Math.abs(delta)===1?'':'s'} removed in Plex — Refresh to resync`;
      return local + ' · in sync with Plex ✓';
    },

    get syncEnabled() {
      const d = this.cacheStatus;
      return !!(d && d.plexConfigured && d.cacheKeyMatches && d.cachedCount > 0 && d.deltaVsCache > 0);
    },

    async fetchCacheStatus() {
      try {
        const r = await fetch('/api/movies/cache-status');
        const j = await r.json();
        this.cacheStatus = j.data || j;
      } catch (e) {}
    },

    startCachePoll() {
      clearInterval(this._cachePollTimer);
      this.fetchCacheStatus();
      this._cachePollTimer = setInterval(() => this.fetchCacheStatus(), 10 * 60 * 1000);
    },

    async syncNewTitles() {
      if (!this.syncEnabled || this.syncing) return;
      this.syncing = true;
      this.syncMsg = 'Merging new titles…';
      try {
        const r = await fetch('/api/movies/sync-recent', { method: 'POST' });
        const j = await r.json();
        const added = j.data?.added ?? 0;
        if (added > 0) {
          await this.load(false);
          this.syncMsg = `Merged ${added} new title${added===1?'':'s'}.`;
        } else {
          this.syncMsg = j.data?.message || 'No new titles to merge.';
        }
        await this.fetchCacheStatus();
      } catch (e) {
        this.syncMsg = 'Sync failed: ' + e.message;
      }
      this.syncing = false;
    },

    init() {
      // Force Alpine to re-evaluate `filtered` by nudging displayedCount even
      // when it was already 60 — avoids Alpine skipping the update as a no-op.
      const reset = () => { this.displayedCount = 0; this.displayedCount = 60; };
      this.$watch('searchQuery', (q) => {
        reset();
        this.scheduleSuggestions();
        this.resolveResults = [];
        clearTimeout(this._resolveTimer);
        if (q.trim().length >= 2) {
          this._resolveTimer = setTimeout(() => this.runResolve(), 600);
        }
        clearTimeout(this._histSaveTimer);
        if (q && q.trim().length >= 2) {
          this._histSaveTimer = setTimeout(() => {
            if (this.filteredMovies.length > 0) {
              pdSaveSearchToHistory(q.trim());
              this.searchHistory = pdLoadSearchHistory();
            }
          }, 800);
        }
      });
      this.$watch('searchScope', () => { reset(); this.scheduleSuggestions(); });
      this.$watch('sortMode', (v) => {
        reset();
        saveSortPrefs({ sort: v, decade: this.decade });
      });
      this.$watch('minRating', reset);
      this.$watch('decade', (v) => {
        reset();
        saveSortPrefs({ sort: this.sortMode, decade: v });
      });
      this.load();
      this.startCachePoll();
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
        this.fetchCacheStatus();
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
    filterByCollection(name, ratingKeys) {
      this.collectionFilter = { name, ratingKeys: new Set(ratingKeys) };
      this.searchQuery = '';
      this.displayedCount = 60;
    },

    clearCollectionFilter() {
      this.collectionFilter = null;
    },

    get filtered() {
      let list = this.movies;

      // Collection filter (from popup series link)
      if (this.collectionFilter?.ratingKeys?.size > 0) {
        list = list.filter(m => this.collectionFilter.ratingKeys.has(m.RatingKey));
        // Sort by release year ascending within the collection
        list = [...list].sort((a, b) => (a.Year || 0) - (b.Year || 0));
        return list;
      }

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
      const transport = this.playbackPath === 'direct' ? '' : this.playbackPath;
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
            shuffle: true,
            transport,
          }),
        });
        const j = await r.json();
        if (!j.success) throw new Error(j.error || 'play failed');
        this.clearSelected();
      } catch (e) {
        alert('Play failed: ' + e.message);
      }
    },

    // ── Hover popup — delegates to shared $store.moviePopup ──────────────────
    normalizeForPopup(m) {
      return {
        thumbUrl: this.thumbUrl(m.RatingKey),
        title: m.Title,
        year: m.Year,
        rating: m.Rating,
        contentRating: m.contentRating,
        durationMs: m.DurationMillis,
        viewCount: m.ViewCount,
        fileContainer: m.FileContainer,
        genres: m.Genres || [],
        summary: m.Summary,
        directors: m.Directors || [],
        actors: m.Actors || [],
        ratingKey: m.RatingKey,
        partKey: m.PartKey || '',
        container: m.FileContainer || 'mp4',
        partSize: m.PartSize || 0,
        tmdbId: m.TMDBID || 0,
        imdbId: m.IMDbID || '',
        mediaType: 'movie',
      };
    },
    onCardMouseenter(event, movie) {
      Alpine.store('moviePopup').show(this.normalizeForPopup(movie), event.currentTarget, { showPlay: true });
    },
    onCardMouseleave() {
      Alpine.store('moviePopup').hide();
    },

    // ── Single-movie play ──────────────────────────────────────────────────
    async playMovie(movie) {
      if (!movie) return;
      const transport = this.playbackPath === 'direct' ? '' : this.playbackPath;
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
            transport,
          }),
        });
        const j = await r.json();
        if (!j.success) throw new Error(j.error || 'play failed');
        this.popupVisible = false;
      } catch (e) {
        alert('Play failed: ' + e.message);
      }
    },

    // ── TMDB smart resolve ───────────────────────────────────────────────────
    async runResolve() {
      const q = (this.searchQuery || '').trim();
      if (!q) { this.resolveResults = []; return; }
      this.resolveLoading = true;
      try {
        const r = await fetch('/api/movies/resolve?q=' + encodeURIComponent(q));
        const j = await r.json();
        if ((this.searchQuery || '').trim() !== q) return; // stale
        this.resolveResults = (j.data?.results || []);
      } catch (e) {
        this.resolveResults = [];
      } finally {
        this.resolveLoading = false;
      }
    },

    async playResolvedMovie(result) {
      if (!result.inPlex || !result.ratingKey) return;
      const movie = this.movies.find(m => m.RatingKey === result.ratingKey);
      if (movie) { this.playMovie(movie); return; }
      // Fallback: play by ratingKey directly
      try {
        await fetch('/api/movies/play', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ ratingKeys: [result.ratingKey] }),
        });
      } catch (e) { alert('Play failed: ' + e.message); }
    },

    // ── Search autocomplete ───────────────────────────────────────────────────
    scheduleSuggestions() {
      clearTimeout(this._suggTimer);
      const q = (this.searchQuery || '').trim();
      if (!q) { this.searchSuggestions = this.searchHistory.slice(0, 10); return; }
      this._suggTimer = setTimeout(() => {
        const scope = this.searchScope;
        const qq = q.toLowerCase();
        const seen = new Set();
        const out = [];
        const add = s => {
          const t = String(s).trim();
          if (!t) return;
          const k = t.toLowerCase();
          if (k.includes(qq) && !seen.has(k)) { seen.add(k); out.push(t); }
        };
        const limit = scope === 'all' ? 40 : 36;
        for (const m of this.movies) {
          if (scope === 'all') {
            add(m.Title);
            for (const a of (m.Actors || [])) add(a);
            for (const d of (m.Directors || [])) add(d);
          } else if (scope === 'actor') {
            for (const a of (m.Actors || [])) add(a);
          } else {
            for (const d of (m.Directors || [])) add(d);
          }
          if (out.length >= limit) break;
        }
        out.sort((a, b) => a.localeCompare(b, undefined, { sensitivity: 'base' }));
        this.searchSuggestions = out.slice(0, 25);
      }, 90);
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
