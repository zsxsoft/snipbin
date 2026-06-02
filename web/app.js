(function () {
  'use strict';

  // ── State ──────────────────────────────────────────────────────────

  var selectedFile = null;
  var isUploading = false;

  // Fill CLI tutorial with actual host
  var host = window.location.origin;
  var cliSpans = document.querySelectorAll('.cli-host');
  for (var i = 0; i < cliSpans.length; i++) {
    cliSpans[i].textContent = host;
  }

  // ── DOM refs ───────────────────────────────────────────────────────

  var dropZone = document.getElementById('drop-zone');
  var dropLabel = document.getElementById('drop-label');
  var fileInput = document.getElementById('file-input');
  var textInput = document.getElementById('text-input');
  var ttlSelect = document.getElementById('ttl-select');
  var uploadBtn = document.getElementById('upload-btn');
  var resultSection = document.getElementById('result-section');
  var resultLink = document.getElementById('result-link');
  var resultMeta = document.getElementById('result-meta');
  var copyBtn = document.getElementById('copy-btn');
  var recentList = document.getElementById('recent-list');

  // ── Init ───────────────────────────────────────────────────────────

  dropZone.addEventListener('click', function () { fileInput.click(); });

  fileInput.addEventListener('change', function () {
    if (fileInput.files && fileInput.files.length > 0) {
      selectedFile = fileInput.files[0];
      updateDropLabel();
      handleUpload();
    }
  });

  // Drag on entire page
  var dragCounter = 0;
  document.addEventListener('dragenter', function (e) {
    e.preventDefault();
    dragCounter++;
    dropZone.classList.add('drag-over');
  });
  document.addEventListener('dragleave', function (e) {
    e.preventDefault();
    dragCounter--;
    if (dragCounter <= 0) {
      dragCounter = 0;
      dropZone.classList.remove('drag-over');
    }
  });
  document.addEventListener('dragover', function (e) {
    e.preventDefault();
  });
  document.addEventListener('drop', function (e) {
    e.preventDefault();
    dragCounter = 0;
    dropZone.classList.remove('drag-over');
    if (e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files.length > 0) {
      selectedFile = e.dataTransfer.files[0];
      updateDropLabel();
      handleUpload();
    }
  });

  document.addEventListener('paste', function (e) {
    var items = e.clipboardData && e.clipboardData.items;
    if (items) {
      for (var i = 0; i < items.length; i++) {
        if (items[i].kind === 'file') {
          var file = items[i].getAsFile();
          if (file) {
            selectedFile = file;
            updateDropLabel();
            handleUpload();
            return;
          }
        }
      }
    }
    var text = e.clipboardData && e.clipboardData.getData('text');
    if (text) {
      textInput.value = text;
      updateUploadBtn();
    }
  });

  textInput.addEventListener('input', updateUploadBtn);
  uploadBtn.addEventListener('click', handleUpload);
  copyBtn.addEventListener('click', handleCopy);

  // CLI tutorial toggle
  document.getElementById('cli-toggle').addEventListener('click', function () {
    var content = document.getElementById('cli-content');
    var hint = document.getElementById('cli-hint');
    if (content.classList.contains('open')) {
      content.classList.remove('open');
      hint.textContent = '[展开]';
    } else {
      content.classList.add('open');
      hint.textContent = '[收起]';
    }
  });

  loadRecent();
  setInterval(loadRecent, 30000);
  setInterval(updateCountdowns, 1000);

  // ── UI helpers ─────────────────────────────────────────────────────

  function updateDropLabel() {
    if (selectedFile) {
      dropLabel.textContent = '已选择: ' + selectedFile.name + ' (' + formatSize(selectedFile.size) + ')';
    } else {
      dropLabel.textContent = '拖拽文件到这里，或点击上传';
    }
  }

  function updateUploadBtn() {
    uploadBtn.disabled = isUploading || (!selectedFile && !textInput.value.trim());
  }

  function formatSize(bytes) {
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB';
    if (bytes < 1073741824) return (bytes / 1048576).toFixed(1) + ' MB';
    return (bytes / 1073741824).toFixed(1) + ' GB';
  }

  function formatRelativeTime(isoStr) {
    var diff = new Date(isoStr).getTime() - Date.now();
    if (diff <= 0) return '已过期';
    var s = Math.floor(diff / 1000);
    if (s < 60) return s + '秒后过期';
    var m = Math.floor(s / 60);
    if (m < 60) return m + '分钟后过期';
    var h = Math.floor(m / 60);
    var rm = m % 60;
    if (h < 24) return h + '小时' + (rm > 0 ? rm + '分钟' : '') + '后过期';
    return Math.floor(h / 24) + '天后过期';
  }

  function isTextType(ct) {
    return ct.indexOf('text/') === 0 ||
      ct.indexOf('application/json') === 0 ||
      ct.indexOf('application/xml') === 0 ||
      ct.indexOf('application/javascript') === 0;
  }

  // ── Upload ─────────────────────────────────────────────────────────

  function handleUpload() {
    if (isUploading) return;
    isUploading = true;
    uploadBtn.disabled = true;
    uploadBtn.textContent = '上传中...';

    var ttl = ttlSelect.value;
    var formData = new FormData();

    if (selectedFile) {
      formData.append('file', selectedFile);
    } else if (textInput.value.trim()) {
      formData.append('text', textInput.value);
    } else {
      isUploading = false;
      uploadBtn.textContent = '上传';
      updateUploadBtn();
      return;
    }

    formData.append('ttl', ttl);

    fetch('/?format=json', { method: 'POST', body: formData })
      .then(function (resp) {
        if (!resp.ok) return resp.text().then(function (t) { throw new Error(t); });
        return resp.json();
      })
      .then(function (data) {
        showResult(data);
        resetForm();
        loadRecent();
      })
      .catch(function (err) {
        alert('上传失败: ' + err.message);
      })
      .finally(function () {
        isUploading = false;
        uploadBtn.textContent = '上传';
        updateUploadBtn();
      });
  }

  function showResult(data) {
    resultLink.value = data.url;
    resultMeta.textContent = '过期时间: ' + new Date(data.expiresAt).toLocaleString();
    resultSection.classList.remove('hidden');
    resultLink.select();
  }

  function resetForm() {
    selectedFile = null;
    fileInput.value = '';
    textInput.value = '';
    updateDropLabel();
    updateUploadBtn();
  }

  // ── Copy ───────────────────────────────────────────────────────────

  function handleCopy() {
    if (navigator.clipboard) {
      navigator.clipboard.writeText(resultLink.value).then(function () {
        copyBtn.textContent = '已复制!';
        setTimeout(function () { copyBtn.textContent = '复制'; }, 1500);
      });
    } else {
      resultLink.select();
      document.execCommand('copy');
      copyBtn.textContent = '已复制!';
      setTimeout(function () { copyBtn.textContent = '复制'; }, 1500);
    }
  }

  // ── Recent list ────────────────────────────────────────────────────

  function loadRecent() {
    fetch('/api/recent')
      .then(function (r) { return r.json(); })
      .then(function (items) {
        if (!items || items.length === 0) {
          recentList.innerHTML = '<p class="empty-hint">暂无记录</p>';
          return;
        }
        recentList.innerHTML = items.map(function (item) {
          var isText = isTextType(item.contentType);
          var icon = isText ? 'TXT' : 'FILE';
          var iconClass = isText ? 'text' : 'file';
          var name = item.filename || item.id;
          var size = formatSize(item.size);
          var expires = formatRelativeTime(item.expiresAt);
          return '<div class="recent-item">' +
            '<div class="item-icon ' + iconClass + '">' + icon + '</div>' +
            '<div class="item-info">' +
              '<div class="item-name" title="' + escHtml(name) + '">' + escHtml(name) + '</div>' +
              '<div class="item-detail">' + size + ' · <span class="countdown" data-expires="' + item.expiresAt + '">' + expires + '</span></div>' +
            '</div>' +
            '<div class="item-actions">' +
              '<button title="复制链接" data-copy="/s/' + item.id + '">&#128279;</button>' +
              '<a href="/s/' + item.id + '" title="下载" download>&#11015;</a>' +
              '<button title="删除" class="btn-del" data-del="' + item.id + '">&#128465;</button>' +
            '</div>' +
          '</div>';
        }).join('');

        // Bind copy buttons
        recentList.querySelectorAll('[data-copy]').forEach(function (btn) {
          btn.addEventListener('click', function () {
            var url = btn.getAttribute('data-copy');
            if (navigator.clipboard) {
              navigator.clipboard.writeText(window.location.origin + url);
            }
            btn.textContent = '✓';
            setTimeout(function () { btn.innerHTML = '&#128279;'; }, 1000);
          });
        });

        // Bind delete buttons
        recentList.querySelectorAll('[data-del]').forEach(function (btn) {
          btn.addEventListener('click', function () {
            if (!confirm('确认删除？')) return;
            var id = btn.getAttribute('data-del');
            fetch('/s/' + id, { method: 'DELETE' }).then(function () {
              loadRecent();
            });
          });
        });
      })
      .catch(function () {});
  }

  function escHtml(s) {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  // ── Countdowns ─────────────────────────────────────────────────────

  function updateCountdowns() {
    document.querySelectorAll('.countdown[data-expires]').forEach(function (el) {
      el.textContent = formatRelativeTime(el.getAttribute('data-expires'));
    });
  }
})();
