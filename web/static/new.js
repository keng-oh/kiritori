(function () {
  var $ = function (id) { return document.getElementById(id); };
  var player = null, currentId = null, apiReady = false;

  // internal/youtube.ParseVideoID と同じ規則（最終判断はサーバー側）
  function parseId(s) {
    s = s.trim();
    if (/^[A-Za-z0-9_-]{11}$/.test(s)) return s;
    try {
      var u = new URL(s), h = u.hostname.replace(/^www\./, ''), id = '';
      if (h === 'youtu.be') id = u.pathname.slice(1);
      else if (/^(m\.|music\.)?youtube\.com$|^youtube-nocookie\.com$/.test(h)) {
        if (u.pathname === '/watch') id = u.searchParams.get('v') || '';
        else { var m = u.pathname.match(/^\/(embed|shorts|live)\/([^/]+)/); if (m) id = m[2]; }
      }
      return /^[A-Za-z0-9_-]{11}$/.test(id) ? id : null;
    } catch (e) { return null; }
  }

  function updateLength() {
    var len = Number($('end').value) - Number($('start').value);
    $('length').textContent = isNaN(len) || $('end').value === '' ? '' : '現在の長さ: ' + len + '秒';
  }

  function loadVideo() {
    var id = parseId($('url').value);
    if (!id || id === currentId || !apiReady) return;
    currentId = id;
    $('player-wrap').hidden = false;
    $('preview').hidden = false;
    if (player) player.loadVideoById(id);
    else player = new YT.Player('player', {
      host: 'https://www.youtube-nocookie.com',
      videoId: id,
      playerVars: { rel: 0, playsinline: 1 },
    });
  }

  window.onYouTubeIframeAPIReady = function () { apiReady = true; loadVideo(); };
  var s = document.createElement('script');
  s.src = 'https://www.youtube.com/iframe_api';
  document.head.appendChild(s);

  $('url').addEventListener('input', loadVideo);
  $('url').addEventListener('change', loadVideo);
  $('start').addEventListener('input', updateLength);
  $('end').addEventListener('input', updateLength);
  $('set-start').addEventListener('click', function () {
    if (player && player.getCurrentTime) { $('start').value = Math.floor(player.getCurrentTime()); updateLength(); }
  });
  $('set-end').addEventListener('click', function () {
    if (player && player.getCurrentTime) { $('end').value = Math.ceil(player.getCurrentTime()); updateLength(); }
  });
  $('preview').addEventListener('click', function () {
    var st = Number($('start').value), en = Number($('end').value);
    if (!player || isNaN(st) || isNaN(en) || en <= st) return;
    player.loadVideoById({ videoId: currentId, startSeconds: st, endSeconds: en });
  });
  updateLength();
})();
