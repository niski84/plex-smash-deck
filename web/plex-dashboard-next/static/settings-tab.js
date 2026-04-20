/* settings-tab.js — Alpine.js data component for the settings tab */
const PD_PLEX_ADDRS_KEY = 'plexdash.plexAddresses';

function loadPlexAddresses() {
  try {
    const raw = localStorage.getItem(PD_PLEX_ADDRS_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed : [];
  } catch(e) { return []; }
}

function savePlexAddresses(addrs) {
  try { localStorage.setItem(PD_PLEX_ADDRS_KEY, JSON.stringify(addrs)); } catch(e) {}
}

function settingsTab() {
  return {
    cfg: {},
    loading: false,
    saving: false,
    status: '',
    players: [],

    // Plex address manager
    plexAddresses: [],
    newAddrLabel: '',
    newAddrUrl: '',
    switchMsg: '',
    switchingUrl: '',

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

      // Load saved addresses from localStorage
      this.plexAddresses = loadPlexAddresses();

      // Seed the address book if empty — add the currently active URL plus
      // any well-known alternates we can derive (plex.direct ↔ raw IP).
      if (this.plexAddresses.length === 0 && this.cfg.PlexBaseURL) {
        const current = this.cfg.PlexBaseURL;
        const seeds = [{ id: 'addr-0', label: 'Primary', url: current }];
        // If the current URL is a plex.direct relay, offer the raw IP as a fallback.
        const ipMatch = current.match(/https?:\/\/(\d{1,3}-\d{1,3}-\d{1,3}-\d{1,3})\./);
        if (ipMatch) {
          const ip = ipMatch[1].replace(/-/g, '.');
          seeds.push({ id: 'addr-1', label: 'Direct IP', url: `http://${ip}:32400` });
        }
        this.plexAddresses = seeds;
        savePlexAddresses(this.plexAddresses);
      }
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
    },

    addPlexAddress() {
      const url = this.newAddrUrl.trim().replace(/\/$/, '');
      if (!url) return;
      // Normalise bare IP/host — add http:// scheme and :32400 if missing
      let normalized = url;
      if (!/^https?:\/\//i.test(normalized)) {
        // bare IP or host — add scheme + default Plex port
        const hasPort = /:\d+$/.test(normalized);
        normalized = 'http://' + normalized + (hasPort ? '' : ':32400');
      }
      if (this.plexAddresses.some(a => a.url === normalized)) {
        this.switchMsg = 'Address already saved.';
        setTimeout(() => { this.switchMsg = ''; }, 3000);
        return;
      }
      const label = this.newAddrLabel.trim() || normalized;
      this.plexAddresses = [...this.plexAddresses, {
        id: Date.now().toString(),
        label,
        url: normalized,
      }];
      savePlexAddresses(this.plexAddresses);
      this.newAddrLabel = '';
      this.newAddrUrl = '';
    },

    removePlexAddress(id) {
      this.plexAddresses = this.plexAddresses.filter(a => a.id !== id);
      savePlexAddresses(this.plexAddresses);
    },

    async switchToAddress(url) {
      if (this.switchingUrl) return;
      this.switchingUrl = url;
      this.switchMsg = '';
      try {
        const r = await fetch('/api/settings/active-plex-url', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ url }),
        });
        const j = await r.json();
        if (j.success) {
          this.cfg = { ...this.cfg, PlexBaseURL: url };
          this.switchMsg = 'Switched to ' + url;
        } else {
          this.switchMsg = 'Error: ' + (j.error || 'failed');
        }
      } catch(e) {
        this.switchMsg = 'Error: ' + e.message;
      }
      this.switchingUrl = '';
      setTimeout(() => { this.switchMsg = ''; }, 5000);
    },

    isActiveAddr(url) {
      return (this.cfg.PlexBaseURL || '').replace(/\/$/, '') === url.replace(/\/$/, '');
    },
  };
}
