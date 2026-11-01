(function () {
  var el = document.getElementById('player');
  var cfg = {
    id: el.dataset.videoId,
    start: Number(el.dataset.start),
    end: Number(el.dataset.end),
  };
  window.onYouTubeIframeAPIReady = function () {
    new YT.Player('player', {
      host: 'https://www.youtube-nocookie.com',
      videoId: cfg.id,
      playerVars: { start: cfg.start, end: cfg.end, rel: 0, playsinline: 1 },
    });
  };
  var s = document.createElement('script');
  s.src = 'https://www.youtube.com/iframe_api';
  document.head.appendChild(s);
})();
