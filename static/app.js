(function () {
  const roomID = window.ROOM_ID || 'unknown';
  const clientToken = crypto.randomUUID ? crypto.randomUUID() : Math.random().toString(36).slice(2);
  let ws = null;
  let lockHolder = null;

  const elements = {
    editor: document.getElementById('editor'),
    codeHighlight: document.getElementById('codeHighlight'),
    codeDisplay: document.getElementById('codeDisplay'),
    language: document.getElementById('languageSelect'),
    statusText: document.getElementById('statusText'),
    connectionText: document.getElementById('connectionText'),
    wsState: document.getElementById('wsState'),
    lockHolder: document.getElementById('lockHolder'),
    codeSize: document.getElementById('codeSize'),
    takeControlBtn: document.getElementById('takeControlBtn'),
    formatBtn: document.getElementById('formatBtn'),
    copyBtn: document.getElementById('copyBtn'),
    reconnectBtn: document.getElementById('reconnectBtn'),
    themeBtn: document.getElementById('themeBtn'),
    lockOverlay: document.getElementById('lockOverlay'),
    clientToken: document.getElementById('clientToken'),
    toast: document.getElementById('toast'),
  };

  function highlightCode(code, language) {
    const keywords = {
      javascript: ['function', 'const', 'let', 'var', 'if', 'else', 'for', 'while', 'return', 'class', 'import', 'export', 'async', 'await', 'try', 'catch', 'finally', 'new', 'this', 'super', 'static'],
      python: ['def', 'class', 'if', 'else', 'elif', 'for', 'while', 'return', 'import', 'from', 'as', 'try', 'except', 'finally', 'with', 'lambda', 'yield', 'raise', 'assert', 'pass', 'break', 'continue', 'and', 'or', 'not', 'in', 'is'],
      go: ['package', 'import', 'func', 'var', 'const', 'type', 'struct', 'interface', 'if', 'else', 'for', 'range', 'switch', 'case', 'default', 'return', 'defer', 'go', 'chan', 'select', 'map', 'make', 'new', 'len', 'cap', 'append', 'copy', 'delete'],
      html: ['html', 'head', 'body', 'div', 'span', 'a', 'img', 'script', 'style', 'link', 'meta', 'title', 'h1', 'h2', 'h3', 'p', 'ul', 'li', 'form', 'input', 'button', 'table', 'tr', 'td', 'class', 'id'],
      css: ['font-size', 'color', 'background', 'margin', 'padding', 'border', 'display', 'flex', 'grid', 'position', 'width', 'height', 'transform', 'transition', 'animation', 'box-shadow', 'border-radius', 'overflow', 'z-index'],
      json: ['true', 'false', 'null']
    };

    const keywordSet = new Set(keywords[language] || []);
    let highlighted = '';
    let i = 0;

    while (i < code.length) {
      if (code[i] === '"' || code[i] === "'") {
        const quote = code[i];
        let str = quote;
        i++;
        while (i < code.length && code[i] !== quote) {
          if (code[i] === '\\') {
            str += code[i] + code[i + 1];
            i += 2;
          } else {
            str += code[i];
            i++;
          }
        }
        if (i < code.length) str += code[i++];
        highlighted += '<span class="string">' + escapeHtml(str) + '</span>';
      } else if (code[i] === '/' && code[i + 1] === '/' && (language === 'javascript' || language === 'go' || language === 'css')) {
        let comment = '';
        while (i < code.length && code[i] !== '\n') {
          comment += code[i++];
        }
        highlighted += '<span class="comment">' + escapeHtml(comment) + '</span>';
      } else if (code[i] === '#' && (language === 'python' || language === 'html')) {
        let comment = '';
        while (i < code.length && code[i] !== '\n') {
          comment += code[i++];
        }
        highlighted += '<span class="comment">' + escapeHtml(comment) + '</span>';
      } else if (/\d/.test(code[i])) {
        let num = '';
        while (i < code.length && /[\d.]/.test(code[i])) {
          num += code[i++];
        }
        highlighted += '<span class="number">' + num + '</span>';
      } else if (/[a-zA-Z_]/.test(code[i])) {
        let word = '';
        while (i < code.length && /[a-zA-Z0-9_]/.test(code[i])) {
          word += code[i++];
        }
        if (keywordSet.has(word)) {
          highlighted += '<span class="keyword">' + word + '</span>';
        } else if (language === 'html' && word.match(/^[A-Z]/)) {
          highlighted += '<span class="tag">' + word + '</span>';
        } else {
          highlighted += word;
        }
      } else if (code[i] === '<' && language === 'html') {
        let tag = '';
        while (i < code.length && code[i] !== '>') {
          tag += code[i++];
        }
        if (i < code.length) tag += code[i++];
        highlighted += '<span class="tag">' + escapeHtml(tag) + '</span>';
      } else {
        highlighted += escapeHtml(code[i]);
        i++;
      }
    }

    return highlighted;
  }

  function escapeHtml(text) {
    const map = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };
    return text.replace(/[&<>"']/g, m => map[m]);
  }

  function updateCodeDisplay() {
    const code = elements.editor.value;
    const language = elements.language.value;
    elements.codeHighlight.innerHTML = highlightCode(code, language);
    elements.codeDisplay.scrollTop = elements.editor.scrollTop;
    elements.codeDisplay.scrollLeft = elements.editor.scrollLeft;
  }

  function updateStatus() {
    const isConnected = ws && ws.readyState === WebSocket.OPEN;
    elements.connectionText.textContent = isConnected ? 'Connected via WebSocket' : (ws && ws.readyState === WebSocket.CONNECTING ? 'WebSocket connecting...' : 'WebSocket disconnected');
    elements.wsState.textContent = ws ? ['connecting', 'open', 'closing', 'closed'][ws.readyState] || 'unknown' : 'offline';

    if (!isConnected) {
      elements.statusText.textContent = 'Connecting to room...';
    } else if (!lockHolder) {
      elements.statusText.textContent = 'Room unlocked. Take control to edit.';
    } else if (lockHolder === clientToken) {
      elements.statusText.textContent = 'You own the lock. Editing enabled.';
    } else {
      elements.statusText.textContent = 'Locked by another user.';
    }

    elements.lockHolder.textContent = lockHolder ? (lockHolder === clientToken ? 'You' : lockHolder.slice(0, 10) + '...') : 'None';
    elements.clientToken.textContent = clientToken.slice(0, 16) + '...';
    elements.codeSize.textContent = `${elements.editor.value.length} chars`;
  }

  function setReadOnly(readOnly) {
    elements.editor.readOnly = !!readOnly;
    elements.lockOverlay.classList.toggle('hidden', !readOnly || lockHolder === clientToken);
  }

  function showToast(message, variant) {
    if (!elements.toast) return;
    elements.toast.textContent = message;
    elements.toast.className = `toast visible ${variant === 'error' ? 'error' : 'success'}`;
    clearTimeout(window._toastTimer);
    window._toastTimer = setTimeout(() => {
      elements.toast.className = 'toast';
    }, 4200);
  }

  let codeUpdateTimer = null;

  function sendCodeUpdate() {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    if (lockHolder !== clientToken) return;
    const payload = {
      type: 'code_update',
      code: elements.editor.value,
    };
    ws.send(JSON.stringify(payload));
  }

  function connectWebSocket() {
    ws = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/ws/${roomID}?client_id=${encodeURIComponent(clientToken)}`);
    updateStatus();

    ws.addEventListener('open', () => {
      showToast('Connected to collaboration room.', 'success');
      lockHolder = null;
      updateStatus();
      setReadOnly(true);
    });

    ws.addEventListener('message', (event) => {
      try {
        const msg = JSON.parse(event.data);
        if (msg.type === 'room_state') {
          lockHolder = msg.holder || null;
          if (typeof msg.code === 'string') {
            elements.editor.value = msg.code;
            updateCodeDisplay();
          }
          setReadOnly(lockHolder !== clientToken);
        }
        if (msg.type === 'code_update') {
          if (typeof msg.code === 'string') {
            elements.editor.value = msg.code;
            updateCodeDisplay();
          }
        }
        if (msg.type === 'lock_status' || msg.type === 'lock_granted') {
          lockHolder = msg.holder || null;
          setReadOnly(lockHolder !== clientToken);
        }
        if (msg.type === 'lock_released') {
          lockHolder = null;
          setReadOnly(true);
        }
        if (msg.type === 'lock_denied') {
          lockHolder = msg.holder || null;
          setReadOnly(true);
          showToast('Lock denied: another user has control.', 'error');
        }
      } catch (err) {
        console.error('Invalid WS message', err);
      }
      updateStatus();
    });

    ws.addEventListener('close', () => {
      showToast('WebSocket connection closed.', 'error');
      lockHolder = null;
      setReadOnly(true);
      updateStatus();
    });

    ws.addEventListener('error', () => {
      showToast('WebSocket error encountered.', 'error');
      updateStatus();
    });
  }

  function requestLock() {
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      showToast('WebSocket is not ready yet.', 'error');
      return;
    }
    ws.send(JSON.stringify({ type: 'request_lock' }));
  }

  function formatCode() {
    fetch('/format', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        code: elements.editor.value,
        language: elements.language.value,
      }),
    })
      .then(async (res) => {
        const payload = await res.json().catch(() => ({}));
        if (!res.ok) {
          throw new Error(payload.error || 'Formatting failed');
        }
        if (payload.code) {
          elements.editor.value = payload.code;
          updateStatus();
          showToast('Code formatted successfully.', 'success');
        }
      })
      .catch((err) => {
        showToast(err.message || 'Formatting failed.', 'error');
      });
  }

  function copyRoomLink() {
    navigator.clipboard.writeText(window.location.href).then(() => {
      showToast('Room link copied.', 'success');
    }).catch(() => {
      showToast('Copy failed. Use browser copy instead.', 'error');
    });
  }

  function toggleTheme() {
    document.documentElement.classList.toggle('dark-mode');
  }

  elements.takeControlBtn.addEventListener('click', requestLock);
  elements.formatBtn.addEventListener('click', formatCode);
  elements.copyBtn.addEventListener('click', copyRoomLink);
  elements.reconnectBtn.addEventListener('click', () => {
    if (ws) ws.close();
    connectWebSocket();
    showToast('Reconnecting WebSocket...', 'success');
  });
  elements.themeBtn.addEventListener('click', toggleTheme);
  elements.language.addEventListener('change', () => {
    updateCodeDisplay();
  });
  elements.editor.addEventListener('input', () => {
    updateStatus();
    updateCodeDisplay();
    clearTimeout(codeUpdateTimer);
    codeUpdateTimer = setTimeout(sendCodeUpdate, 250);
  });
  elements.editor.addEventListener('scroll', () => {
    elements.codeDisplay.scrollTop = elements.editor.scrollTop;
    elements.codeDisplay.scrollLeft = elements.editor.scrollLeft;
  });

  setReadOnly(true);
  updateStatus();
  updateCodeDisplay();
  connectWebSocket();
})();
