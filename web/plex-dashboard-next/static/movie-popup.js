/* movie-popup.js — shared movie hover popup via Alpine.store
 *
 * Any component can trigger the popup by calling:
 *   Alpine.store('moviePopup').show(item, anchorEl, opts)
 *
 * Normalized item shape (all fields optional except title):
 *   { thumbUrl, title, year, rating, contentRating, durationMs, viewCount,
 *     fileContainer, genres[], summary, directors[], actors[],
 *     ratingKey, partKey, container, partSize,
 *     tmdbId, imdbId, mediaType }
 *
 * opts:
 *   { showPlay: bool }   — hide/show Play on TV + Browser buttons
 */

function _popupFmtDuration(ms) {
  if (!ms) return '';
  const m = Math.round(ms / 60000);
  return m >= 60 ? Math.floor(m / 60) + 'h ' + (m % 60) + 'm' : m + 'm';
}

// Rating source → badge classes
function _ratingBadgeClass(source) {
  switch (source) {
    case 'imdb':          return 'border-yellow-600/60 bg-yellow-950/40 text-yellow-300';
    case 'rottenTomatoes':return 'border-red-700/60 bg-red-950/40 text-red-300';
    case 'metacritic':    return 'border-green-700/60 bg-green-950/40 text-green-300';
    case 'tmdb':          return 'border-teal-700/60 bg-teal-950/40 text-teal-300';
    default:              return 'border-zinc-700 bg-zinc-800/40 text-zinc-400';
  }
}

// Trimmed mean on 0–10 scale (mirrors backend averageScore10).
// 1–2 scores → simple average; 3 → median; 4+ → drop min+max, average rest.
function _ratingsAvg10(entries) {
  const scores = (entries || []).map(e => e.score10).filter(s => s > 0);
  if (!scores.length) return 0;
  if (scores.length <= 2) return scores.reduce((a, b) => a + b, 0) / scores.length;
  scores.sort((a, b) => a - b);
  if (scores.length === 3) return scores[1];
  const trimmed = scores.slice(1, -1);
  return trimmed.reduce((a, b) => a + b, 0) / trimmed.length;
}

document.addEventListener('alpine:init', () => {
  Alpine.store('moviePopup', {
    visible: false,
    item: null,
    opts: { showPlay: true },
    x: 0,
    y: 0,
    ratings: null,       // {ok, entries, average10} from /api/omdb-ratings
    collection: null,    // {collectionId, collectionName, parts[]} from /api/tmdb/collection
    _showTimer: null,
    _hideTimer: null,
    _ratingsCtrl: null,
    _collectionCtrl: null,

    show(item, anchor, opts) {
      clearTimeout(this._showTimer);
      clearTimeout(this._hideTimer);
      this._showTimer = setTimeout(() => {
        this._position(anchor);
        this.item = item;
        this.opts = Object.assign({ showPlay: true }, opts || {});
        this.ratings = null;
        this.collection = null;
        this.visible = true;
        this._fetchRatings(item.tmdbId || 0, item.imdbId || '');
        this._fetchCollection(item.tmdbId || 0);
      }, 500);
    },

    hide() {
      clearTimeout(this._showTimer);
      this._hideTimer = setTimeout(() => {
        this.visible = false;
        this.item = null;
        this.ratings = null;
        this.collection = null;
        if (this._ratingsCtrl) { this._ratingsCtrl.abort(); this._ratingsCtrl = null; }
        if (this._collectionCtrl) { this._collectionCtrl.abort(); this._collectionCtrl = null; }
      }, 280);
    },

    keepOpen() {
      clearTimeout(this._hideTimer);
    },

    fmtDuration(ms) { return _popupFmtDuration(ms); },

    ratingBadgeClass(source) { return _ratingBadgeClass(source); },

    get ratingsAvg() {
      if (!this.ratings?.entries?.length) return 0;
      return _ratingsAvg10(this.ratings.entries);
    },

    _fetchRatings(tmdbId, imdbId) {
      if (this._ratingsCtrl) { this._ratingsCtrl.abort(); }
      if (!tmdbId && !imdbId) return;
      this._ratingsCtrl = new AbortController();
      const params = tmdbId > 0 ? 'tmdbId=' + tmdbId : 'imdbId=' + encodeURIComponent(imdbId);
      fetch('/api/omdb-ratings?' + params, { signal: this._ratingsCtrl.signal })
        .then(r => r.json())
        .then(j => { this.ratings = j.data || j; })
        .catch(() => {});
    },

    _fetchCollection(tmdbId) {
      if (this._collectionCtrl) { this._collectionCtrl.abort(); }
      if (!tmdbId || tmdbId <= 0) return;
      this._collectionCtrl = new AbortController();
      fetch('/api/tmdb/collection?tmdbId=' + tmdbId, { signal: this._collectionCtrl.signal })
        .then(r => r.json())
        .then(j => {
          const d = j.data || j;
          if (d && d.collectionId > 0 && d.parts && d.parts.length > 1) {
            this.collection = d;
          }
        })
        .catch(() => {});
    },

    // Navigate to a collection member: if it's owned, open its popup; otherwise
    // set the dashboard search query to the title.
    goToCollectionMember(part) {
      this.hide();
      clearTimeout(this._hideTimer);
      this.visible = false;
      this.item = null;
      this.collection = null;
      if (part.ratingKey) {
        // Owned — jump to it in the grid via the global search helper.
        if (typeof goToDashboardSearch === 'function') {
          goToDashboardSearch(part.title, 'title');
        }
      } else {
        // Not owned — still search for it so the user sees nothing and knows to add it.
        if (typeof goToDashboardSearch === 'function') {
          goToDashboardSearch(part.title, 'title');
        }
      }
    },

    _position(anchor) {
      if (!anchor) return;
      const rect = anchor.getBoundingClientRect();
      const popW = Math.min(640, window.innerWidth * 0.92);
      const popH = 680;
      let x, y;
      if (rect.width > 400) {
        // Wide element (e.g. hero banner): show below-left
        x = Math.max(10, rect.left);
        y = rect.bottom + 8;
        if (y + popH > window.innerHeight - 10) y = Math.max(10, rect.top - popH - 8);
      } else {
        // Normal card: prefer right, fall back to left
        x = rect.right + 10;
        if (x + popW > window.innerWidth - 10) x = rect.left - popW - 10;
        if (x < 10) x = 10;
        y = rect.top;
        if (y + popH > window.innerHeight - 10) y = Math.max(10, window.innerHeight - popH - 10);
      }
      this.x = Math.round(x);
      this.y = Math.round(y);
    },

    async play(mode) {
      const item = this.item;
      if (!item) return;
      if (mode === 'browser') {
        if (item.ratingKey) window.open('/api/stream/' + encodeURIComponent(item.ratingKey), '_blank');
        return;
      }
      if (!item.ratingKey) return;
      const transport = (mode === 'direct' || !mode) ? '' : mode;
      try {
        const r = await fetch('/api/movies/play', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            items: [{
              ratingKey: item.ratingKey,
              partKey: item.partKey || '',
              container: item.container || 'mp4',
              title: item.title + (item.year ? ' (' + item.year + ')' : ''),
              partSize: item.partSize || 0,
            }],
            shuffle: false,
            transport,
          }),
        });
        const j = await r.json();
        if (!j.success) throw new Error(j.error || 'play failed');
        this.visible = false;
      } catch (e) {
        alert('Play failed: ' + e.message);
      }
    },
  });
});
