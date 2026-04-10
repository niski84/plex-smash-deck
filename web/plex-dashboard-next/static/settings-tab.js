/* settings-tab.js — Alpine.js data component for the settings tab */
function settingsTab() {
  return {
    cfg: {},
    loading: false,
    saving: false,
    status: '',
    players: [],

    async init() {
      this.loading = true;
      try {
        const [sr, pr] = await Promise.all([
          fetch('/api/settings').then(r => r.json()),
          fetch('/api/players').then(r => r.json()),
        ]);
        this.cfg = sr.data || sr;
        this.players = pr.data?.players || pr.players || [];
      } catch(e) {}
      this.loading = false;
    },

    async save() {
      this.saving = true;
      this.status = '';
      try {
        const r = await fetch('/api/settings', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(this.cfg)
        });
        const j = await r.json();
        this.status = j.success ? 'Saved!' : ('Error: ' + (j.error || 'unknown'));
      } catch(e) {
        this.status = 'Error: ' + e.message;
      }
      this.saving = false;
    }
  };
}
