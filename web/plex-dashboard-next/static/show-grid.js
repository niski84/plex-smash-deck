/* show-grid.js — Alpine.js component for TV Shows tab (show grid → season tabs → episode grid) */
function showGrid() {
  return {
    // Shows list
    shows: [],
    filteredShows: [],
    searchQuery: '',
    sortMode: 'titleAsc',
    loading: false,
    error: '',

    // Drill-down state
    currentShow: null,
    seasons: [],
    currentSeason: null,
    episodes: [],
    loadingSeasons: false,
    loadingEpisodes: false,

    async init() {
      await this.load();
    },

    async load(nocache) {
      this.loading = true;
      this.error = '';
      try {
        const url = '/api/shows' + (nocache ? '?nocache=1' : '');
        const r = await fetch(url);
        const j = await r.json();
        this.shows = j.data?.shows || j.shows || [];
        this.applySort();
      } catch (e) {
        this.error = e.message;
      }
      this.loading = false;
    },

    applySort() {
      let list = [...this.shows];
      const q = this.searchQuery.trim().toLowerCase();
      if (q) list = list.filter(s => s.Title.toLowerCase().includes(q));
      if (this.sortMode === 'titleAsc')        list.sort((a, b) => a.Title.localeCompare(b.Title));
      else if (this.sortMode === 'plexAddedDesc') list.sort((a, b) => (b.AddedAtEpoch || 0) - (a.AddedAtEpoch || 0));
      else if (this.sortMode === 'ratingDesc')    list.sort((a, b) => (b.Rating || 0) - (a.Rating || 0));
      else if (this.sortMode === 'seasonCountDesc') list.sort((a, b) => (b.SeasonCount || 0) - (a.SeasonCount || 0));
      this.filteredShows = list;
    },

    get showCount() { return this.filteredShows.length; },

    async drillIn(show) {
      this.currentShow = show;
      this.seasons = [];
      this.currentSeason = null;
      this.episodes = [];
      this.loadingSeasons = true;
      try {
        const r = await fetch('/api/seasons?showKey=' + encodeURIComponent(show.RatingKey));
        const j = await r.json();
        const all = j.data?.seasons || j.seasons || [];
        // Put specials (Index === 0) at the end
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
