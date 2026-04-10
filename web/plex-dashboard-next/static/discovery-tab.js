/* discovery-tab.js — Alpine.js data component for the discovery tab */
function discoveryTab() {
  return {
    mode: 'person', // person, studio, year
    personQuery: '',
    role: 'all', // all, actor, director
    studioQuery: '',
    minYear: '',
    maxYear: '',
    minRating: 0,
    excludeNonTheatrical: false,
    jobId: null,
    polling: false,
    results: [],
    status: '',
    cart: [],

    async analyze() {
      this.status = 'Analyzing...';
      this.results = [];
      try {
        let body = {
          mode: this.mode,
          minRating: Number(this.minRating),
          excludeNonTheatrical: this.excludeNonTheatrical,
        };
        if (this.mode === 'person') {
          body.person = this.personQuery;
          body.role = this.role;
        } else if (this.mode === 'studio') {
          body.studio = this.studioQuery;
        } else {
          body.minYear = Number(this.minYear) || 0;
          body.maxYear = Number(this.maxYear) || 9999;
        }
        const r = await fetch('/api/discovery/start', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body)
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
      for (let i = 0; i < 120; i++) {
        await new Promise(r => setTimeout(r, 1000));
        try {
          const r = await fetch('/api/discovery/poll?jobId=' + this.jobId);
          const j = await r.json();
          const d = j.data || {};
          if (d.state === 'done') {
            this.results = d.result?.items || [];
            const missing = this.results.filter(r => !r.inLibrary).length;
            this.status = 'Found ' + this.results.length + ' titles' + (missing ? ', ' + missing + ' not in library' : '');
            this.polling = false;
            return;
          }
          this.status = d.message || 'Analyzing...';
        } catch(e) { break; }
      }
      this.polling = false;
      this.status = 'Timed out';
    },

    get missing() {
      return this.results.filter(r => !r.inLibrary);
    },

    addToCart(item) {
      if (!this.cart.find(c => c.tmdbId === item.tmdbId)) {
        this.cart.push(item);
      }
    },

    removeFromCart(tmdbId) {
      this.cart = this.cart.filter(c => c.tmdbId !== tmdbId);
    },

    copyMissing() {
      const text = this.missing.map(r => '- ' + r.title + ' (' + r.year + ')').join('\n');
      navigator.clipboard.writeText(text);
    },

    copyCart() {
      const text = this.cart.map(r => '- ' + r.title + ' (' + r.year + ')').join('\n');
      navigator.clipboard.writeText(text);
    },

    async clearCache() {
      await fetch('/api/discovery/cache/invalidate', { method: 'POST' });
      alert('TMDB cache cleared');
    }
  };
}
