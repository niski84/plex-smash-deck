/* discovery-tab.js — Alpine.js data component for the discovery tab */
function discoveryTab() {
  return {
    // ── Mode & inputs ──────────────────────────────────────────────────────────
    mode: 'person',           // person | studio | browse
    personQuery: '',
    role: 'all',              // all | actor | director
    directorFilter: '',
    coActorFilter: '',
    playlistTitle: '',
    studioQuery: '',
    minYear: '',
    maxYear: '',
    minRating: 0,
    selectedGenreIds: [],     // array of TMDB genre IDs (strings)
    excludeNonTheatrical: false,

    // ── State ──────────────────────────────────────────────────────────────────
    jobId: null,
    polling: false,
    results: [],
    status: '',
    cart: JSON.parse(localStorage.getItem('plexdash.discovery.cart.v1') || '[]'),
    tmdbGenres: [],           // [{id, name}] loaded once
    playlists: [],            // titles for compare-against dropdown
    selectedRows: new Set(),  // tmdbIds of selected rows
    sortCol: '',              // 'title' | 'year' | 'inLibrary' | 'voteAverage'
    sortDir: 1,               // 1 = asc, -1 = desc

    // ── Autocomplete ───────────────────────────────────────────────────────────
    personSuggestions: [],
    studioSuggestions: [],
    _personSuggestTimer: null,
    _studioSuggestTimer: null,

    // ── Stream cache indicator ─────────────────────────────────────────────────
    cacheStats: null,
    _cacheStatsPollTimer: null,

    async fetchCacheStats() {
      try {
        const r = await fetch('/api/stream/cache');
        const j = await r.json();
        this.cacheStats = j.data || null;
      } catch(e) {}
    },

    startCacheStatsPoll() {
      this.fetchCacheStats();
      clearInterval(this._cacheStatsPollTimer);
      // Poll faster when something is actively downloading.
      this._cacheStatsPollTimer = setInterval(() => {
        this.fetchCacheStats();
        // Re-schedule: faster when active download detected
        if (this.cacheActiveDownload) {
          clearInterval(this._cacheStatsPollTimer);
          this._cacheStatsPollTimer = setInterval(() => this.fetchCacheStats(), 4_000);
        }
      }, 30_000);
    },

    get cacheActiveDownload() {
      if (!this.cacheStats) return null;
      return (this.cacheStats.items || []).find(i => !i.complete && i.totalSize > 0) || null;
    },

    // Returns 0–5 (6 states). During a download: driven by % of that file.
    // Idle: driven by count of fully-cached files on disk.
    get cacheFillState() {
      if (!this.cacheStats) return 0;
      const items = this.cacheStats.items || [];
      const dl = this.cacheActiveDownload;
      if (dl) {
        const pct = dl.totalSize > 0 ? dl.size / dl.totalSize : 0;
        return Math.max(1, Math.ceil(pct * 5)); // always at least 1 when started
      }
      const n = items.filter(i => i.complete).length;
      if (n === 0)  return 0;
      if (n <= 2)   return 1;
      if (n <= 6)   return 2;
      if (n <= 12)  return 3;
      if (n <= 25)  return 4;
      return 5;
    },

    get cacheTooltip() {
      if (!this.cacheStats) return 'Stream cache: loading…';
      const items = this.cacheStats.items || [];
      const complete = items.filter(i => i.complete);
      const gb = ((this.cacheStats.totalBytes || 0) / 1e9).toFixed(1);
      const dl = this.cacheActiveDownload;
      let tip = `Stream cache: ${complete.length} file${complete.length !== 1 ? 's' : ''} ready · ${gb} GB on disk`;
      if (dl) {
        const pct = dl.totalSize > 0 ? Math.round(dl.size / dl.totalSize * 100) : 0;
        tip += `\nDownloading: ${dl.title || dl.ratingKey} (${pct}%)`;
      }
      return tip;
    },

    // ── Init ───────────────────────────────────────────────────────────────────
    async init() {
      await Promise.all([this.loadGenres(), this.loadPlaylists()]);
      this.startCacheStatsPoll();
    },

    async loadGenres() {
      if (this.tmdbGenres.length) return;
      try {
        const r = await fetch('/api/discovery/tmdb-genres');
        const j = await r.json();
        this.tmdbGenres = j.data?.genres || j.genres || [];
      } catch(e) {}
    },

    async loadPlaylists() {
      try {
        const r = await fetch('/api/playlists');
        const j = await r.json();
        this.playlists = (j.data?.playlists || j.playlists || []).map(p => p.Title || p);
      } catch(e) {}
    },

    // ── Year helpers ───────────────────────────────────────────────────────────
    get years() {
      const end = new Date().getFullYear() + 1;
      const out = [];
      for (let y = end; y >= 1920; y--) out.push(y);
      return out;
    },

    // ── Analysis ───────────────────────────────────────────────────────────────
    async analyze() {
      this.status = 'Starting…';
      this.results = [];
      this.selectedRows = new Set();
      try {
        const body = {
          mode: this.mode,
          minRating: Number(this.minRating),
          excludeNonTheatrical: this.excludeNonTheatrical,
          genreIds: this.selectedGenreIds.map(Number).filter(Boolean),
        };
        if (this.mode === 'person') {
          body.person = this.personQuery;
          body.role = this.role;
          if (this.directorFilter.trim()) body.directorFilter = this.directorFilter.trim();
          if (this.coActorFilter.trim()) body.coActorFilter = this.coActorFilter.trim();
          if (this.playlistTitle) body.playlistTitle = this.playlistTitle;
        } else if (this.mode === 'studio') {
          body.studio = this.studioQuery;
        } else {
          body.minYear = Number(this.minYear) || 0;
          body.maxYear = Number(this.maxYear) || 9999;
        }
        const r = await fetch('/api/discovery/start', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body),
        });
        const j = await r.json();
        if (!j.success) throw new Error(j.error || 'start failed');
        this.jobId = j.data?.jobId || j.jobId;
        this.poll();
      } catch(e) {
        this.status = 'Error: ' + e.message;
      }
    },

    async poll() {
      if (!this.jobId) return;
      this.polling = true;
      let dots = 0;
      for (let i = 0; i < 120; i++) {
        await new Promise(r => setTimeout(r, 1000));
        try {
          const r = await fetch('/api/discovery/poll?jobId=' + this.jobId);
          const j = await r.json();
          const d = j.data || {};
          if (d.state === 'done') {
            this.results = d.result?.items || [];
            const missing = this.results.filter(r => !r.inLibrary).length;
            this.status = 'Found ' + this.results.length + ' titles' +
              (missing ? ', ' + missing + ' not in library' : ' · all in library');
            this.polling = false;
            return;
          }
          dots = (dots + 1) % 4;
          this.status = (d.message || 'Analyzing') + '.'.repeat(dots + 1);
        } catch(e) { break; }
      }
      this.polling = false;
      this.status = 'Timed out';
    },

    // ── Computed ───────────────────────────────────────────────────────────────
    get missing() {
      return this.results.filter(r => !r.inLibrary);
    },

    get sortedResults() {
      if (!this.sortCol) return this.results;
      const col = this.sortCol;
      const dir = this.sortDir;
      return [...this.results].sort((a, b) => {
        let av = a[col], bv = b[col];
        if (col === 'title') {
          av = (av || '').toLowerCase();
          bv = (bv || '').toLowerCase();
          return av < bv ? -dir : av > bv ? dir : 0;
        }
        if (col === 'inLibrary') {
          // true before false when ascending
          av = av ? 1 : 0;
          bv = bv ? 1 : 0;
        }
        return (av - bv) * dir;
      });
    },

    setSort(col) {
      if (this.sortCol === col) {
        this.sortDir = -this.sortDir;
      } else {
        this.sortCol = col;
        this.sortDir = col === 'title' ? 1 : -1; // desc by default for numbers
      }
    },

    sortIcon(col) {
      if (this.sortCol !== col) return '⇅';
      return this.sortDir === 1 ? '↑' : '↓';
    },

    // ── Row selection ──────────────────────────────────────────────────────────
    toggleRow(id) {
      const s = new Set(this.selectedRows);
      if (s.has(id)) s.delete(id); else s.add(id);
      this.selectedRows = s;
    },

    isRowSelected(id) { return this.selectedRows.has(id); },

    selectAllMissing() {
      this.selectedRows = new Set(this.missing.map(r => r.tmdbId));
    },

    // ── Cart ───────────────────────────────────────────────────────────────────
    addToCart(item) {
      if (!this.cart.find(c => c.tmdbId === item.tmdbId)) {
        this.cart.push({ tmdbId: item.tmdbId, title: item.title, year: item.year });
        this._saveCart();
      }
    },

    addSelectedToCart() {
      for (const id of this.selectedRows) {
        const item = this.results.find(r => r.tmdbId === id);
        if (item) this.addToCart(item);
      }
    },

    removeFromCart(tmdbId) {
      this.cart = this.cart.filter(c => c.tmdbId !== tmdbId);
      this._saveCart();
    },

    clearCart() {
      this.cart = [];
      this._saveCart();
    },

    _saveCart() {
      try { localStorage.setItem('plexdash.discovery.cart.v1', JSON.stringify(this.cart)); } catch(e) {}
    },

    // ── Copy actions ───────────────────────────────────────────────────────────
    copyMissing() {
      if (!this.missing.length) return;
      const lines = ['## Movies to add\n', ...this.missing.map(r => `- **${r.title}** (${r.year})${r.voteAverage ? ' — TMDB ' + r.voteAverage : ''}`)];
      navigator.clipboard.writeText(lines.join('\n'));
      this.status = 'Copied ' + this.missing.length + ' missing title(s)';
    },

    copyAllTitles() {
      if (!this.results.length) { this.status = 'Run analysis first'; return; }
      const lines = ['## All titles\n', ...this.results.map(r => `- **${r.title}** (${r.year})`)];
      navigator.clipboard.writeText(lines.join('\n'));
      this.status = 'Copied ' + this.results.length + ' title(s)';
    },

    copyCart() {
      if (!this.cart.length) return;
      const lines = ['## Cart\n', ...this.cart.map(r => `- **${r.title}** (${r.year})`)];
      navigator.clipboard.writeText(lines.join('\n'));
    },

    // ── Cache ──────────────────────────────────────────────────────────────────
    async clearCache() {
      try {
        await fetch('/api/discovery/cache/invalidate', { method: 'POST' });
        this.status = 'TMDB cache cleared';
      } catch(e) { this.status = 'Clear failed: ' + e.message; }
    },

    // ── Poster helpers ─────────────────────────────────────────────────────────
    posterSrc(item) {
      const path = (item.posterPath || '').trim();
      if (path) return '/api/discovery/poster?path=' + encodeURIComponent(path.startsWith('/') ? path : '/' + path);
      return (item.posterUrl || '').trim();
    },

    posterBigSrc(item) {
      const path = (item.posterPath || '').trim();
      if (path) {
        const p = path.startsWith('/') ? path : '/' + path;
        return '/api/discovery/poster?path=' + encodeURIComponent(p) + '&size=w780';
      }
      const url = (item.posterUrl || '').trim();
      if (!url) return '';
      return url.replace(/\/t\/p\/w\d+\//, '/t/p/w780/');
    },

    normalizeForPopup(item) {
      return {
        thumbUrl: this.posterBigSrc(item),
        title: item.title,
        year: item.year,
        rating: item.voteAverage,
        genres: item.genres || [],
        summary: item.overview,
        directors: item.directors || [],
        actors: item.actors || [],
        viewCount: item.plexViewCount || 0,
        ratingKey: item.ratingKey || '',
        partKey: '',
        container: 'mp4',
        partSize: 0,
        tmdbId: item.tmdbId || 0,
        imdbId: item.imdbId || '',
        mediaType: 'movie',
      };
    },

    showItemPopup(event, item) {
      const normalized = this.normalizeForPopup(item);
      Alpine.store('moviePopup').show(normalized, event.currentTarget, { showPlay: !!item.inLibrary && !!item.ratingKey });
      // Lazy-fetch cast if not already in the item (browse/studio results)
      if (normalized.tmdbId > 0 && !normalized.actors.length && !normalized.directors.length) {
        fetch('/api/discovery/movie-credits?tmdbId=' + normalized.tmdbId)
          .then(r => r.json())
          .then(j => {
            const popup = Alpine.store('moviePopup');
            if (popup.visible && popup.item && popup.item.tmdbId === normalized.tmdbId) {
              popup.item.directors = j.data?.directors || [];
              popup.item.actors = j.data?.actors || [];
            }
          })
          .catch(() => {});
      }
    },

    hideItemPopup() {
      Alpine.store('moviePopup').hide();
    },

    // ── Autocomplete ───────────────────────────────────────────────────────────
    fetchPersonSuggestions() {
      clearTimeout(this._personSuggestTimer);
      const q = (this.personQuery || '').trim();
      if (!q) { this.personSuggestions = []; return; }
      this._personSuggestTimer = setTimeout(async () => {
        try {
          const r = await fetch('/api/discovery/person-suggest?q=' + encodeURIComponent(q));
          const j = await r.json();
          this.personSuggestions = j.data?.suggestions || [];
          if (j.data?.suggestedRole) this.role = j.data.suggestedRole;
        } catch(e) {}
      }, 100);
    },

    fetchStudioSuggestions() {
      clearTimeout(this._studioSuggestTimer);
      const q = (this.studioQuery || '').trim();
      if (!q) { this.studioSuggestions = []; return; }
      this._studioSuggestTimer = setTimeout(async () => {
        try {
          const r = await fetch('/api/discovery/studio-suggest?q=' + encodeURIComponent(q));
          const j = await r.json();
          this.studioSuggestions = j.data?.suggestions || [];
        } catch(e) {}
      }, 100);
    },
  };
}
