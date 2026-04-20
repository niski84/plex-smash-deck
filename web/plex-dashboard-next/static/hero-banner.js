/* hero-banner.js — Alpine.js component for the fanart hero banner */
function heroBanner() {
  return {
    active: false,
    imageUrl: '',
    movie: null,
    loaded: false,

    init() {
      this.load();
      setInterval(() => this.load(), 5 * 60 * 1000);
    },

    async load() {
      try {
        const r = await fetch('/api/branding/fanart-banner');
        if (!r.ok) return;
        const j = await r.json();
        const d = j.data || j;
        this.active = d.active === true;
        if (this.active) {
          this.imageUrl = d.imageUrl || '';
          this.movie = d.movie || null;
        }
        this.loaded = true;
      } catch(e) {
        this.loaded = true;
      }
    },

    normalizeForPopup(m) {
      return {
        thumbUrl: '/api/plex/thumb?ratingKey=' + encodeURIComponent(m.ratingKey),
        title: m.title,
        year: m.year,
        rating: m.rating,
        durationMs: m.durationMs,
        summary: m.summary,
        actors: m.actors || [],
        directors: [],
        genres: [],
        ratingKey: m.ratingKey,
        partKey: m.partKey || '',
        container: m.container || 'mp4',
        partSize: m.partSize || 0,
      };
    },
    onBannerMouseenter() {
      if (!this.movie) return;
      Alpine.store('moviePopup').show(this.normalizeForPopup(this.movie), this.$el, { showPlay: true });
    },
    onBannerMouseleave() {
      Alpine.store('moviePopup').hide();
    },

    thumbUrl() {
      if (!this.movie || !this.movie.ratingKey) return '';
      return '/api/plex/thumb?ratingKey=' + encodeURIComponent(this.movie.ratingKey);
    },

    fmtDuration(ms) {
      if (!ms) return '';
      const h = Math.floor(ms / 3600000);
      const m = Math.floor((ms % 3600000) / 60000);
      return h > 0 ? `${h}h ${m}m` : `${m}m`;
    },

    async playBanner() {
      if (!this.movie) return;
      try {
        const r = await fetch('/api/movies/play', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            items: [{
              ratingKey: this.movie.ratingKey,
              partKey: this.movie.partKey || '',
              container: this.movie.container || 'mp4',
              title: this.movie.title + (this.movie.year ? ' (' + this.movie.year + ')' : ''),
              partSize: this.movie.partSize || 0,
            }],
            shuffle: false,
          }),
        });
        const j = await r.json();
        if (!j.success) throw new Error(j.error || 'play failed');
      } catch(e) {
        alert('Play failed: ' + e.message);
      }
    },
  };
}
