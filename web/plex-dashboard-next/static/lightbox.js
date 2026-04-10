/* lightbox.js — fullscreen image viewer with fanart gallery support */
(function () {
  let overlay = null;
  let imgEl = null;
  let counterEl = null;
  let prevBtn = null;
  let nextBtn = null;
  let gallery = [];
  let idx = 0;

  function buildDOM() {
    if (overlay) return;

    overlay = document.createElement('div');
    overlay.id = 'pd-lb';
    overlay.innerHTML = [
      '<div class="pd-lb-bg"></div>',
      '<img class="pd-lb-img" src="" alt="" draggable="false">',
      '<button class="pd-lb-close" title="Close (Esc)">✕</button>',
      '<button class="pd-lb-prev" title="Previous (←)" hidden>‹</button>',
      '<button class="pd-lb-next" title="Next (→)" hidden>›</button>',
      '<div class="pd-lb-counter" hidden></div>',
    ].join('');
    document.body.appendChild(overlay);

    imgEl     = overlay.querySelector('.pd-lb-img');
    counterEl = overlay.querySelector('.pd-lb-counter');
    prevBtn   = overlay.querySelector('.pd-lb-prev');
    nextBtn   = overlay.querySelector('.pd-lb-next');

    overlay.querySelector('.pd-lb-bg').addEventListener('click', close);
    overlay.querySelector('.pd-lb-close').addEventListener('click', close);
    prevBtn.addEventListener('click', (e) => { e.stopPropagation(); step(-1); });
    nextBtn.addEventListener('click', (e) => { e.stopPropagation(); step(1); });

    document.addEventListener('keydown', (e) => {
      if (!overlay.classList.contains('pd-lb-open')) return;
      if (e.key === 'Escape')      close();
      if (e.key === 'ArrowLeft')  step(-1);
      if (e.key === 'ArrowRight') step(1);
    });
  }

  function render() {
    if (!gallery[idx]) return;
    imgEl.src = gallery[idx].url;
    imgEl.alt = gallery[idx].label || '';
    const multi = gallery.length > 1;
    counterEl.hidden = !multi;
    prevBtn.hidden   = !multi;
    nextBtn.hidden   = !multi;
    if (multi) counterEl.textContent = (idx + 1) + ' / ' + gallery.length;
  }

  function step(delta) {
    idx = (idx + delta + gallery.length) % gallery.length;
    render();
  }

  function close() {
    if (!overlay) return;
    overlay.classList.remove('pd-lb-open');
    document.body.style.overflow = '';
    imgEl.src = '';
    gallery = [];
  }

  window.lightbox = {
    open(src, opts) {
      opts = opts || {};
      buildDOM();
      gallery = [{ url: src, label: opts.alt || '' }];
      idx = 0;
      render();
      overlay.classList.add('pd-lb-open');
      document.body.style.overflow = 'hidden';

      // Fetch fanart gallery in background
      const rk = opts.ratingKey;
      if (rk) {
        fetch('/api/fanart-movie/prefetch?ratingKey=' + encodeURIComponent(rk))
          .then(function (r) { return r.json(); })
          .then(function (j) {
            const items = (j.data && j.data.items) || [];
            for (let i = 0; i < items.length; i++) {
              gallery.push({ url: items[i].url, label: items[i].label || items[i].kind || '' });
            }
            render(); // refresh counter/nav now that we have more slides
          })
          .catch(function () {});
      }
    },
    close: close,
  };
})();
