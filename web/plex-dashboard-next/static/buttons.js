/* buttons.js — squishy button click animation
 *
 * Adds a `smash-active` class to any squishy button (.btn-gold, .btn-red,
 * .btn-blue, .btn-green, .pd-tab-active, .pd-tab-inactive) on click. The class
 * triggers the smash-bounce + smash-ring keyframes defined in input.css and
 * is removed after the animation completes (280ms) so it can re-trigger on
 * the next click.
 */
(function () {
  'use strict';

  var SQUISH_CLASSES = [
    'btn-gold',
    'btn-red',
    'btn-blue',
    'btn-green',
    'pd-tab-active',
    'pd-tab-inactive',
  ];
  var ANIM_MS = 290;

  function isSquishy(el) {
    if (!el || el.tagName !== 'BUTTON') return false;
    for (var i = 0; i < SQUISH_CLASSES.length; i++) {
      if (el.classList.contains(SQUISH_CLASSES[i])) return true;
    }
    return false;
  }

  document.addEventListener(
    'click',
    function (e) {
      var btn = e.target.closest('button');
      if (!isSquishy(btn)) return;

      // Re-trigger the animation by clearing the class first, forcing reflow.
      btn.classList.remove('smash-active');
      void btn.offsetWidth;
      btn.classList.add('smash-active');

      setTimeout(function () {
        btn.classList.remove('smash-active');
      }, ANIM_MS);
    },
    { passive: true }
  );
})();
