/* show-grid.js — Alpine.js component for TV Shows tab (show grid → season tabs → episode grid) */
const _TV_CACHE_KEY = 'plexdash.tv.shows.v1';
const _TV_CACHE_TTL = 30 * 60 * 1000; // 30 minutes

function _tvCacheRead() {
  try {
    const raw = localStorage.getItem(_TV_CACHE_KEY);
    if (!raw) return null;
    const obj = JSON.parse(raw);
    if (!obj?.shows || !obj.ts) return null;
    if (Date.now() - obj.ts > _TV_CACHE_TTL) return null;
    return obj.shows;
  } catch { return null; }
}

function _tvCacheWrite(shows) {
  try { localStorage.setItem(_TV_CACHE_KEY, JSON.stringify({ shows, ts: Date.now() })); } catch {}
}

function showGrid() {
  return {
    // Shows list
    shows: [],
    filteredShows: [],
    searchQuery: '',
    sortMode: 'titleAsc',
    loading: false,
    error: '',

    // Filters (parallel to movie-grid)
    genreFilter: '',
    minRating: 0,
    watchedFilter: 'all',     // all | unwatched | watched
    contentRatingFilter: '',  // '' = all
    minYear: '',
    maxYear: '',

    // Drill-down state
    currentShow: null,
    seasons: [],
    currentSeason: null,
    episodes: [],
    loadingSeasons: false,
    loadingEpisodes: false,

    async init() {
      // Show cached shows immediately while fresh data loads in background.
      const cached = _tvCacheRead();
      if (cached && cached.length > 0) {
        this.shows = cached;
        this.applySort();
      }
      await this.load(false, !!cached);
    },

    async load(nocache, silent) {
      if (!silent) this.loading = true;
      this.error = '';
      try {
        const url = '/api/shows' + (nocache ? '?nocache=1' : '');
        const r = await fetch(url);
        const j = await r.json();
        const loaded = j.data?.shows || j.shows || [];
        if (loaded.length > 0) {
          this.shows = loaded;
          _tvCacheWrite(loaded);
          this.applySort();
        } else if (this.shows.length === 0) {
          // No cache and API returned nothing — keep empty (shows "not configured" message)
          this.applySort();
        }
      } catch (e) {
        if (!silent) this.error = e.message;
      }
      this.loading = false;
    },

    // ── Computed filter options derived from loaded shows ─────────────────────

    get availableGenres() {
      const seen = new Set();
      for (const s of this.shows) for (const g of (s.Genres || [])) seen.add(g);
      return [...seen].sort();
    },

    get availableContentRatings() {
      const seen = new Set();
      for (const s of this.shows) if (s.ContentRating) seen.add(s.ContentRating);
      return [...seen].sort();
    },

    // ── Filter + sort ─────────────────────────────────────────────────────────

    applySort() {
      let list = [...this.shows];

      // Search
      const q = this.searchQuery.trim().toLowerCase();
      if (q) list = list.filter(s =>
        s.Title.toLowerCase().includes(q) ||
        (s.Actors || []).some(a => a.toLowerCase().includes(q))
      );

      // Genre
      if (this.genreFilter) list = list.filter(s => (s.Genres || []).includes(this.genreFilter));

      // Min rating
      const minR = Number(this.minRating) || 0;
      if (minR > 0) list = list.filter(s => s.Rating >= minR);

      // Watched filter
      if (this.watchedFilter === 'unwatched') list = list.filter(s => !s.ViewCount || s.ViewCount === 0);
      else if (this.watchedFilter === 'watched') list = list.filter(s => s.ViewCount > 0);

      // Content rating
      if (this.contentRatingFilter) list = list.filter(s => s.ContentRating === this.contentRatingFilter);

      // Year range
      if (this.minYear) list = list.filter(s => !s.Year || s.Year >= Number(this.minYear));
      if (this.maxYear) list = list.filter(s => !s.Year || s.Year <= Number(this.maxYear));

      // Sort
      if (this.sortMode === 'titleAsc')           list.sort((a, b) => a.Title.localeCompare(b.Title));
      else if (this.sortMode === 'titleDesc')      list.sort((a, b) => b.Title.localeCompare(a.Title));
      else if (this.sortMode === 'plexAddedDesc')  list.sort((a, b) => (b.AddedAtEpoch || 0) - (a.AddedAtEpoch || 0));
      else if (this.sortMode === 'ratingDesc')     list.sort((a, b) => (b.Rating || 0) - (a.Rating || 0));
      else if (this.sortMode === 'seasonCountDesc') list.sort((a, b) => (b.SeasonCount || 0) - (a.SeasonCount || 0));
      else if (this.sortMode === 'yearDesc')       list.sort((a, b) => (b.Year || 0) - (a.Year || 0));
      else if (this.sortMode === 'yearAsc')        list.sort((a, b) => (a.Year || 0) - (b.Year || 0));
      else if (this.sortMode === 'lastViewedDesc') list.sort((a, b) => (b.LastViewedAtEpoch || 0) - (a.LastViewedAtEpoch || 0));

      this.filteredShows = list;
    },

    clearFilters() {
      this.searchQuery = '';
      this.genreFilter = '';
      this.minRating = 0;
      this.watchedFilter = 'all';
      this.contentRatingFilter = '';
      this.minYear = '';
      this.maxYear = '';
      this.applySort();
    },

    get isFiltered() {
      return this.searchQuery || this.genreFilter || Number(this.minRating) > 0 ||
             this.watchedFilter !== 'all' || this.contentRatingFilter || this.minYear || this.maxYear;
    },

    get showCount() { return this.filteredShows.length; },

    // ── Hover popup ───────────────────────────────────────────────────────────

    normalizeShowForPopup(show) {
      return {
        thumbUrl: this.thumbUrl(show.RatingKey),
        title: show.Title,
        year: show.Year,
        rating: show.Rating,
        contentRating: show.ContentRating,
        genres: show.Genres || [],
        summary: show.Summary,
        actors: show.Actors || [],
        directors: [],
        viewCount: show.ViewCount || 0,
        ratingKey: '',
        partKey: '',
        container: 'mkv',
        partSize: 0,
        tmdbId: show.TMDBID || 0,
        imdbId: '',
        mediaType: 'tv',
      };
    },

    onCardMouseenter(event, show) {
      Alpine.store('moviePopup').show(this.normalizeShowForPopup(show), event.currentTarget, { showPlay: false });
    },

    onCardMouseleave() {
      Alpine.store('moviePopup').hide();
    },

    // ── Drill-down ────────────────────────────────────────────────────────────

    async drillIn(show) {
      Alpine.store('moviePopup').hide();
      this.currentShow = show;
      this.seasons = [];
      this.currentSeason = null;
      this.episodes = [];
      this.loadingSeasons = true;
      try {
        const r = await fetch('/api/seasons?showKey=' + encodeURIComponent(show.RatingKey));
        const j = await r.json();
        const all = j.data?.seasons || j.seasons || [];
        const regular = all.filter(s => s.Index > 0);
        const specials = all.filter(s => s.Index === 0);
        this.seasons = [...regular, ...specials];
        if (this.seasons.length > 0) await this.selectSeason(this.seasons[0]);
      } catch (e) {}
      this.loadingSeasons = false;
    },

    back() {
      this.currentShow = null;
      this.seasons = [];
      this.currentSeason = null;
      this.episodes = [];
    },

    async selectSeason(season) {
      this.currentSeason = season;
      this.loadingEpisodes = true;
      this.episodes = [];
      try {
        const r = await fetch('/api/episodes?seasonKey=' + encodeURIComponent(season.RatingKey));
        const j = await r.json();
        this.episodes = j.data?.episodes || j.episodes || [];
      } catch (e) {}
      this.loadingEpisodes = false;
    },

    // ── Helpers ───────────────────────────────────────────────────────────────

    thumbUrl(ratingKey) {
      return '/api/plex/thumb?ratingKey=' + encodeURIComponent(ratingKey);
    },

    episodeThumbUrl(thumbKey) {
      if (!thumbKey) return '';
      return '/api/plex/episode-thumb?key=' + encodeURIComponent(thumbKey);
    },

    showMeta(show) {
      const parts = [];
      if (show.Year) parts.push(show.Year);
      if (show.SeasonCount) parts.push('S' + show.SeasonCount);
      if (show.EpisodeCount) parts.push(show.EpisodeCount + ' ep');
      if (show.Rating) parts.push('★ ' + show.Rating.toFixed(1));
      return parts.join(' · ');
    },

    fmtDuration(ms) {
      if (!ms) return '';
      const m = Math.round(ms / 60000);
      if (m >= 60) {
        const h = Math.floor(m / 60);
        const r = m % 60;
        return r > 0 ? h + 'h ' + r + 'm' : h + 'h';
      }
      return m + 'm';
    },

    fmtAirYear(epochSec) {
      if (!epochSec) return '';
      return String(new Date(epochSec * 1000).getFullYear());
    },

    progressPct(ep) {
      if (!ep.ViewOffset || !ep.DurationMillis) return 0;
      return Math.round(100 * ep.ViewOffset / ep.DurationMillis);
    },

    async playEpisode(ep) {
      try {
        const r = await fetch('/api/movies/play', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            items: [{
              ratingKey: ep.RatingKey,
              partKey: ep.PartKey || '',
              container: ep.FileContainer || 'mkv',
              title: (this.currentShow?.Title || '') + ' — ' + ep.Title,
              partSize: ep.PartSize || 0,
            }],
            shuffle: false,
          }),
        });
        const j = await r.json();
        if (!j.success) throw new Error(j.error || 'play failed');
      } catch (e) {
        alert('Play failed: ' + e.message);
      }
    },

    seasonLabel(season) {
      return season.Index === 0 ? 'Specials' : 'Season ' + season.Index;
    },
  };
}
