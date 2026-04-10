function connectivityBadge() {
  return {
    checks: [],        // array matching the 5-service order: internet, plex, tmdb, omdb, lgtv
    stream: null,      // plexStream object
    summary: '',
    overall: '',
    tooltipVisible: false,
    init() {
      this.poll();
      setInterval(() => this.poll(), 12000);
    },
    async poll() {
      try {
        const r = await fetch('/api/connectivity');
        const j = await r.json();
        const p = j.data || j;
        // ensure order matches I P T O L
        const order = ['internet','plex','tmdb','omdb','lgtv'];
        const byId = {};
        (p.checks||[]).forEach(c => byId[c.id] = c);
        this.checks = order.map(id => byId[id] || {id, label: id, level: 'skip', message: 'no data'});
        this.stream = p.plexStream || null;
        this.summary = p.summary || '';
        this.overall = p.overall || '';
      } catch(e) {
        // keep stale data on network error
      }
    },
    // Returns array of 3 booleans: whether each bar is filled based on level
    bars(level) {
      // barsCount: ok=3, warn=2, error=1, skip=0
      const count = level === 'ok' ? 3 : level === 'warn' ? 2 : level === 'error' ? 1 : 0;
      return [1, 2, 3].map(i => i <= count);
    },
    barColor(level) {
      return level === 'ok' ? '#22c55e' : level === 'warn' ? '#eab308' : level === 'error' ? '#f87171' : '#3f3f46';
    },
    letterFor(id) {
      return {internet:'I', plex:'P', tmdb:'T', omdb:'O', lgtv:'L'}[id] || id[0].toUpperCase();
    },
    tooltipText() {
      const lines = this.checks.map(c => {
        const ms = c.latencyMs ? ` (${c.latencyMs}ms)` : '';
        const sym = c.level === 'ok' ? '✓' : c.level === 'warn' ? '!' : c.level === 'error' ? '✗' : '−';
        return `${sym} ${c.label}${ms}: ${c.message||c.level}`;
      });
      if (this.stream && this.stream.mbps > 0) {
        lines.push(`⚡ Stream: ~${this.stream.mbps.toFixed(1)} Mb/s`);
      }
      return lines.join('\n');
    }
  };
}
