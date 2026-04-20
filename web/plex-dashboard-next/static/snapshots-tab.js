/* snapshots-tab.js — Alpine.js data component for the snapshots tab */
function snapshotsTab() {
  return {
    snapshots: [],
    latestDiff: null,
    missing: [],
    yearDrift: [],
    compareFrom: '',
    compareTo: '',
    diffResult: null,
    status: '',
    loading: false,

    // Latest Drop expand state
    latestDropExpanded: false,

    // Change-cell hover popup
    hoverSnap: null,       // the snapshot row being hovered
    hoverDiff: null,       // { added: [], removed: [] }
    hoverLoading: false,
    hoverVisible: false,
    hoverX: 0,
    hoverY: 0,
    _hoverTimer: null,
    _hoverHideTimer: null,

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
        this.yearDrift = missR.data?.yearDrift || [];
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

    // Compute net change for a given index (newest-first array)
    netChange(idx) {
      if (this.snapshots[idx]?.noChange) return null;
      // Find the next real snapshot (skip no-change markers) to diff against.
      let prev = idx + 1;
      while (prev < this.snapshots.length && this.snapshots[prev].noChange) prev++;
      if (prev >= this.snapshots.length) return null;
      return (this.snapshots[idx].count || 0) - (this.snapshots[prev].count || 0);
    },

    fmtChange(n) {
      if (n === null || n === undefined) return '—';
      if (n === 0) return '—';
      return (n > 0 ? '+' : '') + n + ' movies';
    },

    fmtDate(iso) {
      if (!iso) return '—';
      return new Date(iso).toLocaleString();
    },

    // Hover popup for change cells
    async onChangeHover(event, idx) {
      const n = this.netChange(idx);
      if (!n || n <= 0) return;
      const snap = this.snapshots[idx];
      const prev = this.snapshots[idx + 1];
      if (!prev) return;

      clearTimeout(this._hoverHideTimer);
      this.hoverX = event.clientX + 12;
      this.hoverY = event.clientY - 8;

      // If already showing this snap, just keep it open
      if (this.hoverSnap?.id === snap.id) {
        this.hoverVisible = true;
        return;
      }

      this.hoverSnap = snap;
      this.hoverDiff = null;
      this.hoverVisible = true;
      this.hoverLoading = true;

      clearTimeout(this._hoverTimer);
      this._hoverTimer = setTimeout(async () => {
        try {
          const r = await fetch('/api/snapshots/diff?from=' + prev.id + '&to=' + snap.id);
          const j = await r.json();
          this.hoverDiff = j.data?.diff || null;
        } catch(e) {}
        this.hoverLoading = false;
      }, 250);
    },

    onChangeLeave() {
      clearTimeout(this._hoverTimer);
      this._hoverHideTimer = setTimeout(() => {
        this.hoverVisible = false;
        this.hoverSnap = null;
        this.hoverDiff = null;
      }, 300);
    },

    keepHoverOpen() {
      clearTimeout(this._hoverHideTimer);
    },

    // Added movies list for hover popup — fall back to latestDiff if same snapshot
    get hoverAdded() {
      return this.hoverDiff?.added || [];
    },
  };
}
