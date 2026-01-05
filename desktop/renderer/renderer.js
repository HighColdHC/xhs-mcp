const API_BASE = 'http://127.0.0.1:18060';

// 激活相关元素
const licensePanel = document.getElementById('license-panel');
const mainContainer = document.getElementById('main-container');
const licenseKeyInput = document.getElementById('license-key');
const machineIdInput = document.getElementById('machine-id');
const btnActivate = document.getElementById('btn-activate');
const btnEnter = document.getElementById('btn-enter');
const licenseMsg = document.getElementById('license-msg');

// 授权信息展示元素
const infoKey = document.getElementById('info-key');
const infoMachine = document.getElementById('info-machine');
const infoExpire = document.getElementById('info-expire');

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

// ==================== 激活相关逻辑 ====================

// 检查授权状态
async function checkLicense() {
  try {
    const res = await fetch(`${API_BASE}/api/v1/license/status`);
    const data = await res.json();
    if (data.success && data.data) {
      return data.data;
    }
    return { licensed: false };
  } catch (e) {
    console.error('检查授权失败:', e);
    return { licensed: false };
  }
}

// 获取机器码
async function getMachineID() {
  try {
    const res = await fetch(`${API_BASE}/api/v1/license/machine-id`);
    const text = await res.text();
    console.log('机器码响应:', text);
    const data = JSON.parse(text);
    if (data.success && data.data && data.data.machine_id) {
      return data.data.machine_id;
    }
    if (data.message) {
      return '错误: ' + data.message;
    }
  } catch (e) {
    console.error('获取机器码失败:', e);
  }
  return '获取失败';
}

// 显示激活界面
async function showLicensePanel(existingKey = '') {
  const machineID = await getMachineID();
  machineIdInput.value = machineID;
  licenseKeyInput.value = existingKey; // 回显已激活的卡密

  // 如果已有卡密，显示"进入系统"按钮
  if (existingKey) {
    btnEnter.classList.remove('hidden');
  } else {
    btnEnter.classList.add('hidden');
  }

  licensePanel.classList.remove('hidden');
  mainContainer.classList.add('hidden');
}

// 隐藏激活界面，显示主界面
function hideLicensePanel() {
  licensePanel.classList.add('hidden');
  mainContainer.classList.remove('hidden');
}

// 更新主界面授权信息
function updateLicenseInfo(status) {
  if (status.licensed) {
    infoKey.textContent = status.key_masked || status.key || '-';
    infoMachine.textContent = status.machine_id || '-';
    infoExpire.textContent = status.expire_at ? new Date(status.expire_at).toLocaleDateString() : '-';
  } else {
    infoKey.textContent = '-';
    infoMachine.textContent = '-';
    infoExpire.textContent = '-';
  }
}

// 激活
async function activate() {
  const key = licenseKeyInput.value.trim();
  if (!key) {
    showLicenseMsg('请输入卡密', 'error');
    return;
  }

  btnActivate.disabled = true;
  showLicenseMsg('激活中...', '');

  try {
    const res = await fetch(`${API_BASE}/api/v1/license/activate`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ key }),
    });
    const text = await res.text();
    console.log('激活响应:', text);
    const data = JSON.parse(text);

    if (data.success) {
      showLicenseMsg('激活成功！正在进入...', 'success');
      setTimeout(() => {
        hideLicensePanel();
        updateLicenseInfo(data.data); // 更新授权信息
        loadAccounts(); // 加载主界面数据
      }, 1000);
    } else {
      showLicenseMsg(data.message || '激活失败', 'error');
    }
  } catch (e) {
    showLicenseMsg('激活失败: ' + e.message, 'error');
  } finally {
    btnActivate.disabled = false;
  }
}

// 显示激活消息
function showLicenseMsg(msg, type) {
  licenseMsg.textContent = msg;
  licenseMsg.className = 'license-msg ' + type;
}

// 绑定激活按钮事件
btnActivate.onclick = activate;
btnEnter.onclick = enterMain;

// 初始化：始终显示激活界面
async function initLicense() {
  const status = await checkLicense();
  // 始终显示激活界面，已激活时回显卡密
  await showLicensePanel(status.licensed ? status.key : '');
}

// 进入主界面（跳过激活界面）
async function enterMain() {
  const status = await checkLicense();
  if (status.licensed) {
    hideLicensePanel();
    updateLicenseInfo(status);
    loadAccounts();
    loadLogs();
  } else {
    showLicenseMsg('请先激活软件', 'error');
  }
}

// init
initLicense();
toggleProxyFields();
