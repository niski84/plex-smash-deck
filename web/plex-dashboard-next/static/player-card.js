/* player-card.js — Alpine.js data component for the now-playing / target player card */
function playerCard() {
  return {
    players: [],
    selectedPlayer: localStorage.getItem('plexdash.selectedPlayer') || '',
    controlStatus: '',

    // Live Plex session
    primaryFrom: '',
    sessionTitle: '',
    sessionPlayer: '',
    sessionState: '',
    progressPercent: 0,
    sessionDurationMs: 0,
    sessionViewOffsetMs: 0,
    _lastPollAt: 0,

    // "Just finished" state — shown briefly after session disappears
    _prevPrimaryFrom: '',
    _sessionEndedAt: 0,
    _lastSessionTitle: '',
    _lastSessionDurationMs: 0,

    // Local send (app-initiated queue)
    localTitles: [],
    localStale: false,
    localRatingKeys: [],

    summaryLine: '',
    targetName: '',

    // ── Time-remaining helpers ───────────────────────────────────────────────
    get _adjustedViewOffsetMs() {
      if (!this._lastPollAt || !this.sessionViewOffsetMs) return this.sessionViewOffsetMs;
      if (this.sessionState === 'paused') return this.sessionViewOffsetMs;
      return this.sessionViewOffsetMs + (Date.now() - this._lastPollAt);
    },

    get remainingLabel() {
      if (this.sessionDurationMs <= 0) return '';
      const remainMs = this.sessionDurationMs - this._adjustedViewOffsetMs;
      if (remainMs <= 0) return 'ending soon';
      const m = Math.round(remainMs / 60000);
      if (m < 1) return 'ending soon';
      if (m >= 60) {
        const h = Math.floor(m / 60);
        const r = m % 60;
        return r > 0 ? `${h}h ${r}m left` : `${h}h left`;
      }
      return `${m}m left`;
    },

    get predictedEndTime() {
      if (this.sessionDurationMs <= 0 || !this._lastPollAt) return '';
      const remainMs = this.sessionDurationMs - this._adjustedViewOffsetMs;
      if (remainMs <= 0) return '';
      const endAt = new Date(Date.now() + remainMs);
      return 'ends ~' + endAt.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    },

    // Show "just finished" state briefly after session ends
    get showJustFinished() {
      if (this.primaryFrom === 'plex_session') return false;
      if (!this._sessionEndedAt) return false;
      // Show for 2 minutes after session ended
      return Date.now() - this._sessionEndedAt < 2 * 60 * 1000;
    },

    // ── Init ─────────────────────────────────────────────────────────────────
    async init() {
      this.$watch('selectedPlayer', v => {
        if (v) localStorage.setItem('plexdash.selectedPlayer', v);
      });
      await Promise.all([this.loadPlayers(), this.refreshPlayback()]);
      // Poll every 30s (original was 30s)
      setInterval(() => this.refreshPlayback(), 30000);
      // Tick every 30s to keep time-remaining label fresh
      setInterval(() => {
        // Trigger Alpine reactivity by touching a reactive dep
        this._lastPollAt = this._lastPollAt; // no-op but won't trigger Alpine
      }, 30000);
    },

    async loadPlayers() {
      try {
        const r = await fetch('/api/players');
        const j = await r.json();
        this.players = j.data?.players || j.players || [];
        const names = this.players.map(p => p.Name || p);
        if (names.length === 0) return;
        // Use persisted selection if it still exists; otherwise fall back to first device
        if (this.selectedPlayer && names.includes(this.selectedPlayer)) return;
        this.selectedPlayer = names[0];
        localStorage.setItem('plexdash.selectedPlayer', this.selectedPlayer);
      } catch (e) {}
    },

    async refreshPlayback() {
      try {
        const r = await fetch('/api/playback/status');
        const j = await r.json();
        const d = j.data || {};
        const nowMs = Date.now();

        this.summaryLine = d.summaryLine || '';
        this.targetName = d.targetClientName || '';

        const prevPrimary = this.primaryFrom;
        this.primaryFrom = d.primaryFrom || 'idle';

        // Detect session ending
        if (prevPrimary === 'plex_session' && this.primaryFrom !== 'plex_session') {
          this._sessionEndedAt = nowMs;
          this._lastSessionTitle = this.sessionTitle;
          this._lastSessionDurationMs = this.sessionDurationMs;
        }

        // Live Plex session
        const sess = (d.plexSessions || [])[0];
        if (sess) {
          const gt = sess.grandparentTitle;
          const pt = sess.parentTitle;
          const t = sess.title || '';
          this.sessionTitle = gt ? (pt ? `${gt} — ${pt} — ${t}` : `${gt} — ${t}`) : t;
          this.sessionPlayer = sess.playerName || '';
          this.sessionState = sess.playerState || '';
          this.progressPercent = sess.progressPercent || 0;
          this.sessionDurationMs = sess.durationMs || 0;
          this.sessionViewOffsetMs = sess.viewOffsetMs || 0;
          this._lastPollAt = nowMs;
          // Reset ended state since we have an active session
          this._sessionEndedAt = 0;
        } else {
          this.sessionTitle = '';
          this.sessionPlayer = '';
          this.sessionState = '';
          this.progressPercent = 0;
          this.sessionDurationMs = 0;
          this.sessionViewOffsetMs = 0;
        }

        // Local send (app queue)
        const ls = d.localSend || {};
        this.localTitles = ls.titles || [];
        this.localStale = !!ls.stale;
        this.localRatingKeys = ls.ratingKeys || [];
      } catch (e) {}
    },

    get localPoster() {
      const rk = this.localRatingKeys[0];
      return rk ? '/api/plex/thumb?ratingKey=' + encodeURIComponent(rk) : '';
    },

    async control(action) {
      try {
        const r = await fetch('/api/plex/companion/control', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ action, clientName: this.selectedPlayer }),
        });
        const j = await r.json();
        this.controlStatus = j.success ? 'OK' : (j.error || 'error');
        setTimeout(() => { this.controlStatus = ''; }, 2000);
      } catch (e) {
        this.controlStatus = e.message;
        setTimeout(() => { this.controlStatus = ''; }, 3000);
      }
    },
  };
}
