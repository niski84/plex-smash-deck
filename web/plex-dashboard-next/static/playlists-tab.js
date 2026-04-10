/* playlists-tab.js — Alpine.js data component for the playlists tab */
function playlistsTab() {
  return {
    playlists: [],
    selectedPlaylist: '',
    playlistItems: [],
    loadingItems: false,
    playStatus: '',
    // Build by people
    buildTitle: 'People Picks',
    buildCount: 8,
    actorName: '',
    directorName: '',
    buildStatus: '',
    showBuildForm: false,
    // Build by genre/rating
    buildGenre: 'Action',
    buildMinRating: 0,
    genreStatus: '',

    async init() {
      try {
        const r = await fetch('/api/playlists');
        const j = await r.json();
        this.playlists = j.data?.playlists || j.playlists || [];
      } catch(e) {}
    },

    async loadItems() {
      if (!this.selectedPlaylist) return;
      this.loadingItems = true;
      this.playlistItems = [];
      try {
        const r = await fetch('/api/playlists/items?title=' + encodeURIComponent(this.selectedPlaylist) + '&limit=250');
        const j = await r.json();
        this.playlistItems = j.data?.movies || j.data?.items || j.items || [];
      } catch(e) {}
      this.loadingItems = false;
    },

    async play() {
      if (!this.selectedPlaylist) return;
      this.playStatus = 'Sending...';
      try {
        const r = await fetch('/api/playlists/play', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ title: this.selectedPlaylist })
        });
        const j = await r.json();
        this.playStatus = j.success ? 'Playing!' : ('Error: ' + (j.error || 'failed'));
      } catch(e) {
        this.playStatus = 'Error: ' + e.message;
      }
    },

    async buildByPeople() {
      this.buildStatus = 'Creating...';
      try {
        const r = await fetch('/api/playlists/by-people', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            title: this.buildTitle,
            count: Number(this.buildCount),
            actor: this.actorName,
            director: this.directorName
          })
        });
        const j = await r.json();
        const d = j.data || j;
        this.buildStatus = j.success
          ? ('Created "' + d.Title + '" with ' + d.Count + ' movies')
          : ('Error: ' + (j.error || 'failed'));
        if (j.success) {
          await this.init(); // refresh playlists
        }
      } catch(e) {
        this.buildStatus = 'Error: ' + e.message;
      }
    }
  };
}
