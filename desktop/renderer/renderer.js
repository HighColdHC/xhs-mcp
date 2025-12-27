const API_BASE = 'http://127.0.0.1:18060';

const tabAccounts = document.getElementById('tab-accounts');
const tabLogs = document.getElementById('tab-logs');
const panelAccounts = document.getElementById('panel-accounts');
const panelLogs = document.getElementById('panel-logs');

const btnRefresh = document.getElementById('btn-refresh');
const btnNew = document.getElementById('btn-new');
const btnRawStart = document.getElementById('btn-raw-start');
const accountBody = document.getElementById('account-body');

const modal = document.getElementById('modal');
const formTitle = document.getElementById('form-title');
const formId = document.getElementById('form-id');
const formName = document.getElementById('form-name');
const formType = document.getElementById('form-type');
const formHost = document.getElementById('form-host');
const formPort = document.getElementById('form-port');
const formUser = document.getElementById('form-user');
const formPass = document.getElementById('form-pass');
const formSave = document.getElementById('form-save');
const formCancel = document.getElementById('form-cancel');
const proxyFields = document.getElementById('proxy-fields');

const btnLogReload = document.getElementById('btn-log-reload');
const btnBackendRestart = document.getElementById('btn-backend-restart');
const logBox = document.getElementById('log-box');

let editingId = null;

function switchTab(tab) {
  if (tab === 'accounts') {
    tabAccounts.classList.add('active');
    tabLogs.classList.remove('active');
    panelAccounts.classList.add('active');
    panelLogs.classList.remove('active');
  } else {
    tabLogs.classList.add('active');
    tabAccounts.classList.remove('active');
    panelLogs.classList.add('active');
    panelAccounts.classList.remove('active');
  }
}

tabAccounts.onclick = () => switchTab('accounts');
tabLogs.onclick = () => switchTab('logs');

async function fetchJSON(url, opts) {
  const res = await fetch(url, opts);
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

async function loadAccounts() {
  try {
    const data = await fetchJSON(`${API_BASE}/api/v1/accounts`);
    renderAccounts(data.data || []);
  } catch (e) {
    alert('加载账号失败: ' + e.message);
  }
}

function renderAccounts(list) {
  accountBody.innerHTML = '';
  list.forEach((acc) => {
    const proxyInfo = acc.proxy_type
      ? `${acc.proxy_type}://${acc.proxy_host || ''}${acc.proxy_port ? ':' + acc.proxy_port : ''}`
      : acc.proxy || '-';
    const tr = document.createElement('tr');
    tr.dataset.type = acc.proxy_type || 'direct';
    tr.dataset.host = acc.proxy_host || '';
    tr.dataset.port = acc.proxy_port || '';
    tr.dataset.user = acc.proxy_user || '';
    tr.dataset.pass = acc.proxy_pass || '';
    tr.innerHTML = `
      <td>${acc.id}</td>
      <td>${acc.name || '-'}</td>
      <td>${proxyInfo || '-'}</td>
      <td>${acc.proxy_type && acc.proxy_host ? '正常' : '未设置'}</td>
      <td>${acc.logged_in ? '已登录' : '未登录'}</td>
      <td>
        <a href="#" data-action="start" data-id="${acc.id}">启动</a>
        &nbsp;
        <a href="#" data-action="edit" data-id="${acc.id}">编辑</a>
        &nbsp;
        <a href="#" data-action="delete" data-id="${acc.id}">删除</a>
      </td>
    `;
    accountBody.appendChild(tr);
  });
}

btnRefresh.onclick = loadAccounts;

btnNew.onclick = () => {
  editingId = null;
  formTitle.textContent = '新增账号';
  formId.value = '自动';
  formName.disabled = false;
  formName.readOnly = false;
  formName.removeAttribute('disabled');
  formName.removeAttribute('readonly');
  formName.value = '';
  formType.value = 'direct';
  formHost.value = '';
  formPort.value = '';
  formUser.value = '';
  formPass.value = '';
  toggleProxyFields();
  modal.classList.remove('hidden');
  setTimeout(() => formName.focus(), 0);
};

if (btnRawStart) {
  btnRawStart.onclick = async () => {
    try {
      await fetchJSON(`${API_BASE}/api/v1/raw/start`, {
        method: 'POST',
      });
      alert('已启动原生浏览器窗口');
    } catch (e) {
      alert('启动失败: ' + e.message);
    }
  };
}

accountBody.onclick = (e) => {
  const action = e.target.getAttribute('data-action');
  const id = e.target.getAttribute('data-id');
  if (!action || !id) return;
  if (action === 'edit') {
    editingId = Number(id);
    formTitle.textContent = '编辑账号';
    formId.value = id;
    const row = e.target.closest('tr');
    formName.disabled = false;
    formName.readOnly = false;
    formName.removeAttribute('disabled');
    formName.removeAttribute('readonly');
    formName.value = row.children[1].textContent;
    formType.value = row.dataset.type || 'direct';
    formHost.value = row.dataset.host || '';
    formPort.value = row.dataset.port || '';
    formUser.value = row.dataset.user || '';
    formPass.value = row.dataset.pass || '';
    toggleProxyFields();
    modal.classList.remove('hidden');
    setTimeout(() => formName.focus(), 0);
  }
  if (action === 'delete') {
    if (confirm(`确认删除账号 ${id} 吗？`)) {
      deleteAccount(id);
    }
  }
  if (action === 'start') {
    startAccountWindow(id);
  }
  e.preventDefault();
};

formCancel.onclick = () => {
  modal.classList.add('hidden');
};

formSave.onclick = async () => {
  if (formSave.disabled) return;
  formSave.disabled = true;
  formSave.textContent = '保存中...';
  const proxyType = formType.value;
  const host = formHost.value.trim();
  const port = Number(formPort.value.trim());
  const user = formUser.value.trim();
  const pass = formPass.value.trim();
  const name = formName.value.trim();

  const payload = {
    proxy_type: proxyType,
    proxy_host: host,
    proxy_port: port || 0,
    proxy_user: user,
    proxy_pass: pass,
    name,
  };

  try {
    if (editingId) {
      await fetchJSON(`${API_BASE}/api/v1/accounts/${editingId}/proxy`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
    } else {
      await fetchJSON(`${API_BASE}/api/v1/login/start`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
    }
    modal.classList.add('hidden');
    await loadAccounts();
  } catch (e) {
    alert('保存失败: ' + e.message);
  } finally {
    formSave.disabled = false;
    formSave.textContent = '保存';
  }
};

function toggleProxyFields() {
  if (formType.value === 'direct') {
    proxyFields.style.display = 'none';
  } else {
    proxyFields.style.display = 'block';
  }
}
formType.onchange = toggleProxyFields;

async function deleteAccount(id) {
  try {
    await fetchJSON(`${API_BASE}/api/v1/accounts/${id}`, {
      method: 'DELETE',
    });
    await loadAccounts();
  } catch (e) {
    alert('删除失败: ' + e.message);
  }
}

// 轮询指定账号的登录状态，直到登录完成或超时
async function pollAccountLoginStatus(id, maxAttempts = 120, interval = 2000) {
  for (let i = 0; i < maxAttempts; i++) {
    try {
      const data = await fetchJSON(`${API_BASE}/api/v1/accounts`);
      const account = (data.data || []).find(a => a.id === Number(id));
      if (account && account.logged_in) {
        return true; // 登录成功
      }
    } catch (e) {
      // 忽略轮询中的错误，继续下次尝试
    }
    await new Promise(resolve => setTimeout(resolve, interval));
  }
  return false; // 超时未登录
}

async function startAccountWindow(id) {
  try {
    await fetchJSON(`${API_BASE}/api/v1/accounts/${id}/start`, {
      method: 'POST',
    });
    alert('已启动该账号的浏览器窗口，请在浏览器中扫码登录');

    // 启动轮询监控登录状态
    pollAccountLoginStatus(id).then(success => {
      if (success) {
        loadAccounts(); // 登录成功，刷新账号列表
      }
    });
  } catch (e) {
    alert('启动失败: ' + e.message);
  }
}

// 日志
async function loadLogs() {
  const lines = (await window.electronAPI.getLogs()) || [];
  logBox.value = lines.join('\n');
  logBox.scrollTop = logBox.scrollHeight;
}

window.electronAPI.onLogAppend((lines) => {
  const extra = Array.isArray(lines) ? lines : [String(lines)];
  logBox.value += (logBox.value ? '\n' : '') + extra.join('\n');
  logBox.scrollTop = logBox.scrollHeight;
});

btnLogReload.onclick = loadLogs;
btnBackendRestart.onclick = async () => {
  await window.electronAPI.restartBackend();
};

// init
loadAccounts();
loadLogs();
toggleProxyFields();
