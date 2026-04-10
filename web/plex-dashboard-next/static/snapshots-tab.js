/* snapshots-tab.js — Alpine.js data component for the snapshots tab */
function snapshotsTab() {
  return {
    snapshots: [],
    latestDiff: null,
    missing: [],
    compareFrom: '',
    compareTo: '',
    diffResult: null,
    status: '',
    loading: false,

    async init() {
      await this.refresh();
    },

    async refresh() {
      this.loading = true;
      try {
        const [snapR, diffR, missR] = await Promise.all([
          fetch('/api/snapshots').then(r => r.json()),
          fetch('/api/snapshots/latest-diff').then(r => r.json()),
          fetch('/api/snapshots/missing').then(r => r.json()),
        ]);
        this.snapshots = snapR.data?.snapshots || snapR.snapshots || [];
        this.latestDiff = diffR.data?.diff || null;
        this.missing = missR.data?.missing || missR.missing || [];
      } catch(e) {
        this.status = 'Error loading snapshots: ' + e.message;
      }
      this.loading = false;
    },

    async takeSnapshot() {
      this.status = 'Taking snapshot...';
      try {
        const r = await fetch('/api/snapshots', { method: 'POST' });
        const j = await r.json();
        this.status = j.success ? 'Snapshot taken!' : ('Error: ' + (j.error || 'failed'));
        await this.refresh();
      } catch(e) {
        this.status = 'Error: ' + e.message;
      }
    },

    async compare() {
      if (!this.compareFrom || !this.compareTo) return;
      try {
        const r = await fetch('/api/snapshots/diff?from=' + this.compareFrom + '&to=' + this.compareTo);
        const j = await r.json();
        this.diffResult = j.data?.diff || null;
      } catch(e) {
        this.status = 'Error: ' + e.message;
      }
    },

    fmtDate(iso) {
      if (!iso) return '—';
      return new Date(iso).toLocaleString();
    },

    fmtChange(n) {
      if (n === 0) return '—';
      return (n > 0 ? '+' : '') + n;
    }
  };
}
