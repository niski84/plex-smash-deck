/* volume-control.js — Alpine component for the volume slider with device picker */
function volumeControl() {
  const STORAGE_KEY = 'plexdash.volumeDevice';

  // Kept in closure scope — AbortController can't live inside Alpine's reactive proxy.
  let abortCtrl = null;

  return {
    vol: 0,
    supported: false,
    dragging: false,
    sending: false,
    muted: false,
    devices: [],
    selectedId: localStorage.getItem(STORAGE_KEY) || '',

    async init() {
      await this._loadDevices();
      await this._fetch();
    },

    async _loadDevices() {
      try {
        const r = await fetch('/api/tv-devices');
        const j = await r.json();
        if (j.success && Array.isArray(j.data)) {
          // Audio volume card only shows dedicated audio receivers (smash-deck protocol).
          // LG TVs are video playback targets — they live in the Target Player card, not here.
          this.devices = j.data.filter(d => d.manufacturer === 'smash-deck');
          if (!this.selectedId && this.devices.length > 0) {
            this.selectedId = this.devices[0].id;
            localStorage.setItem(STORAGE_KEY, this.selectedId);
          }
          // If the persisted selection is no longer in the filtered list, reset to first.
          if (this.selectedId && !this.devices.find(d => d.id === this.selectedId) && this.devices.length > 0) {
            this.selectedId = this.devices[0].id;
            localStorage.setItem(STORAGE_KEY, this.selectedId);
          }
        }
      } catch {}
    },

    get _dev() {
      return this.devices.find(d => d.id === this.selectedId) || this.devices[0] || null;
    },

    _isSmashDeck() {
      return this._dev?.manufacturer === 'smash-deck';
    },

    _smashBase() {
      const addr = this._dev?.addr || '';
      if (addr.startsWith('http://') || addr.startsWith('https://')) return addr.replace(/\/$/, '');
      return 'http://' + addr.replace(/\/$/, '');
    },

    async _fetch() {
      try {
        let volume, muted;
        if (this._isSmashDeck()) {
          const r = await fetch(this._smashBase() + '/api/state');
          const j = await r.json();
          volume = j.volume;
          muted  = j.muted;
        } else {
          const devParam = this.selectedId ? `?device=${encodeURIComponent(this.selectedId)}` : '';
          const r = await fetch('/api/lg/volume' + devParam);
          const j = await r.json();
          if (!j.success || !j.data?.supported) { this.supported = false; return; }
          volume = j.data.volume;
          muted  = j.data.mute;
        }
        this.supported = true;
        if (typeof volume === 'number') this.vol = volume;
        if (typeof muted  === 'boolean') this.muted = muted;
      } catch { this.supported = false; }
    },

    async _send(level) {
      // Cancel the previous in-flight request. For LG, the server also has
      // a per-device cancel that stops the key-press goroutines server-side.
      if (abortCtrl) {
        abortCtrl.abort();
        abortCtrl = null;
      }
      const ctrl = new AbortController();
      abortCtrl = ctrl;
      this.sending = true;

      try {
        if (this._isSmashDeck()) {
          const r = await fetch(`${this._smashBase()}/api/v1/volume/set?level=${level}`, {
            method: 'POST',
            signal: ctrl.signal,
          });
          const j = await r.json();
          if (typeof j.volume === 'number') this.vol = j.volume;
        } else {
          const devParam = this.selectedId ? `?device=${encodeURIComponent(this.selectedId)}` : '';
          const r = await fetch('/api/lg/volume' + devParam, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ level }),
            signal: ctrl.signal,
          });
          const j = await r.json();
          if (j.success && j.data && typeof j.data.volume === 'number') this.vol = j.data.volume;
        }
      } catch (e) {
        if (e.name !== 'AbortError') console.warn('[volume]', e);
      } finally {
        if (abortCtrl === ctrl) {
          abortCtrl = null;
          this.sending = false;
        }
      }
    },

    onRelease() {
      this.dragging = false;
      this._send(this.vol);
    },

    step(delta) {
      this.vol = Math.max(0, Math.min(100, this.vol + delta));
      this._send(this.vol);
    },

    async selectDevice(id) {
      this.selectedId = id;
      localStorage.setItem(STORAGE_KEY, id);
      this.supported = false;
      await this._fetch();
    },
  };
}
