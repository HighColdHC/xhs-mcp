const { app, BrowserWindow, ipcMain, dialog, Tray, Menu, nativeImage } = require('electron');
const path = require('path');
const fs = require('fs');
const { spawn } = require('child_process');

const LOG_MAX = 2000;
const BACKEND_PORT = ':18060';

let mainWindow = null;
let tray = null;
let isQuitting = false;
let backendProc = null;
let backendOverridePath = '';
let runtimeConfig = null;
let logBuffer = [];

function appendLogLines(lines) {
  const payload = Array.isArray(lines) ? lines : [String(lines)];
  payload.forEach((line) => {
    if (!line) return;
    logBuffer.push(line);
  });
  if (logBuffer.length > LOG_MAX) {
    logBuffer = logBuffer.slice(logBuffer.length - LOG_MAX);
  }
  if (mainWindow && !mainWindow.isDestroyed()) {
    mainWindow.webContents.send('log:append', payload);
  }
}

function appendLog(line) {
  appendLogLines([line]);
}

function resolveBaseDir() {
  if (process.env.PORTABLE_EXECUTABLE_DIR) {
    return process.env.PORTABLE_EXECUTABLE_DIR;
  }
  if (app.isPackaged) {
    return path.dirname(process.execPath);
  }
  return path.resolve(__dirname);
}

function ensureDir(target) {
  fs.mkdirSync(target, { recursive: true });
}

function copyFileIfNeeded(src, dest) {
  const destDir = path.dirname(dest);
  ensureDir(destDir);
  let shouldCopy = false;
  let reason = '';

  if (!fs.existsSync(dest)) {
    shouldCopy = true;
    reason = 'dest not exists';
  } else {
    try {
      const srcStat = fs.statSync(src);
      const destStat = fs.statSync(dest);
      // 🔥 强制覆盖策略：只要时间戳不一致就覆盖
      if (srcStat.mtimeMs !== destStat.mtimeMs) {
        shouldCopy = true;
        reason = `timestamps differ (src=${srcStat.mtimeMs}, dest=${destStat.mtimeMs})`;
      } else {
        reason = `timestamps identical (${srcStat.mtimeMs})`;
      }
    } catch (_err) {
      shouldCopy = true;
      reason = 'stat error';
    }
  }

  if (shouldCopy) {
    appendLog(`[COPY] ${path.basename(src)} -> ${path.basename(dest)} (${reason})`);
    fs.copyFileSync(src, dest);
  } else {
    appendLog(`[SKIP] ${path.basename(src)} already up-to-date (${reason})`);
  }
}

function copyDirIfMissing(srcDir, destDir) {
  const destExe = path.join(destDir, 'chrome.exe');
  if (!fs.existsSync(destExe)) {
    ensureDir(path.dirname(destDir));
    fs.cpSync(srcDir, destDir, { recursive: true });
  }
}

function resolveDevBackendPath() {
  // 🔥 跨平台支持：Windows 用 .exe，macOS/Linux 用无扩展名
  const ext = process.platform === 'win32' ? '.exe' : '';
  return path.resolve(__dirname, '..', 'backend', `xhs-mcp${ext}`);
}

function resolveDevChromiumDir() {
  // 🔥 跨平台支持：Windows 用 chrome-win64，macOS 用 chrome-mac，Linux 用 chrome-linux
  if (process.platform === 'win32') {
    return path.resolve(__dirname, 'vendor', 'chromium', 'chrome-win64');
  } else if (process.platform === 'darwin') {
    return path.resolve(__dirname, 'vendor', 'chromium', 'chrome-mac');
  } else {
    return path.resolve(__dirname, 'vendor', 'chromium', 'chrome-linux');
  }
}

function resolveResources() {
  const baseDir = resolveBaseDir();
  const dataDir = path.join(baseDir, 'xhs-data');
  ensureDir(dataDir);
  ensureDir(path.join(dataDir, 'profiles'));
  ensureDir(path.join(dataDir, 'cookies'));

  // 🔥 跨平台支持：根据平台选择正确的后端和 Chromium
  const isWin = process.platform === 'win32';
  const isMac = process.platform === 'darwin';
  const backendExt = isWin ? '.exe' : '';
  const chromiumDir = isWin ? 'chrome-win64' : (isMac ? 'chrome-mac' : 'chrome-linux');
  const chromeExeName = isWin ? 'chrome.exe' : 'Chromium';  // macOS/Linux 可执行文件名

  let backendSrc = '';
  let chromiumSrcDir = '';
  if (app.isPackaged) {
    backendSrc = path.join(process.resourcesPath, 'backend', `xhs-mcp${backendExt}`);
    chromiumSrcDir = path.join(process.resourcesPath, 'chromium', chromiumDir);
  } else {
    backendSrc = resolveDevBackendPath();
    chromiumSrcDir = resolveDevChromiumDir();
  }

  const backendDest = path.join(dataDir, 'backend', `xhs-mcp${backendExt}`);
  const chromiumDestDir = path.join(baseDir, 'chromium', chromiumDir);
  const chromiumExe = path.join(chromiumDestDir, chromeExeName);

  if (backendSrc && fs.existsSync(backendSrc)) {
    try {
      copyFileIfNeeded(backendSrc, backendDest);
    } catch (err) {
      appendLog(`[DESKTOP] copy backend failed: ${err.message}`);
    }
  } else {
    appendLog('[DESKTOP] backend source not found');
  }

  if (chromiumSrcDir && fs.existsSync(path.join(chromiumSrcDir, 'chrome.exe'))) {
    try {
      copyDirIfMissing(chromiumSrcDir, chromiumDestDir);
    } catch (err) {
      appendLog(`[DESKTOP] copy chromium failed: ${err.message}`);
    }
  } else {
    appendLog('[DESKTOP] chromium source not found');
  }

  return {
    baseDir,
    dataDir,
    backendPath: backendDest,
    chromePath: chromiumExe,
  };
}

function startBackend() {
  if (!runtimeConfig) return;
  const backendPath = backendOverridePath || runtimeConfig.backendPath;
  const chromePath = runtimeConfig.chromePath;
  if (!backendPath || !fs.existsSync(backendPath)) {
    appendLog('[DESKTOP] backend exe missing, skip start');
    return;
  }

  // 🔥 跨平台支持：检查并清理旧的后端进程
  const { exec } = require('child_process');
  const backendName = path.basename(backendPath);

  if (process.platform === 'win32') {
    // Windows 使用 tasklist/taskkill
    exec(`tasklist | findstr "${backendName}"`, (err, stdout) => {
      if (stdout && stdout.includes(backendName)) {
        appendLog('[DESKTOP] detected existing backend process, cleaning up...');
        const killCmd = `taskkill /F /IM ${backendName}`;
        exec(killCmd, (killErr) => {
          if (killErr) {
            appendLog(`[DESKTOP] cleanup failed: ${killErr.message}`);
          } else {
            appendLog('[DESKTOP] old backend process killed');
            setTimeout(() => doStartBackend(backendPath, chromePath), 1000);
          }
        });
      } else {
        doStartBackend(backendPath, chromePath);
      }
    });
  } else if (process.platform === 'darwin' || process.platform === 'linux') {
    // macOS/Linux 使用 pgrep/pkill
    exec(`pgrep -f "${backendName}"`, (err, stdout) => {
      if (stdout && stdout.trim()) {
        appendLog('[DESKTOP] detected existing backend process, cleaning up...');
        const killCmd = `pkill -9 -f "${backendName}"`;
        exec(killCmd, (killErr) => {
          if (killErr) {
            appendLog(`[DESKTOP] cleanup failed: ${killErr.message}`);
            doStartBackend(backendPath, chromePath);
          } else {
            appendLog('[DESKTOP] old backend process killed');
            setTimeout(() => doStartBackend(backendPath, chromePath), 1000);
          }
        });
      } else {
        doStartBackend(backendPath, chromePath);
      }
    });
  } else {
    // 其他平台直接启动
    doStartBackend(backendPath, chromePath);
  }
}

function doStartBackend(backendPath, chromePath) {

  const env = {
    ...process.env,
    ACCOUNTS_STORE: path.join(runtimeConfig.dataDir, 'accounts.json'),
    USER_DATA_BASE_DIR: path.join(runtimeConfig.dataDir, 'profiles'),
    COOKIES_BASE_DIR: path.join(runtimeConfig.dataDir, 'cookies'),
    ROD_BROWSER_BIN: chromePath,
    CHROME_PATH: chromePath,
    LOG_LEVEL: 'debug',
    XHS_CHROME_VERBOSE: process.env.XHS_CHROME_VERBOSE || '0',
    XHS_ROD_TRACE: process.env.XHS_ROD_TRACE || '0',
  };

  const args = ['--headless=true', '--port', BACKEND_PORT];
  if (chromePath && fs.existsSync(chromePath)) {
    args.push('--bin', chromePath);
  }

  appendLog(`[DESKTOP] baseDir: ${runtimeConfig.baseDir}`);
  appendLog(`[DESKTOP] dataDir: ${runtimeConfig.dataDir}`);
  appendLog(`[DESKTOP] backend exe: ${backendPath}`);
  appendLog(`[DESKTOP] chromium: ${chromePath}`);
  appendLog(`[DESKTOP] starting backend process...`);

  backendProc = spawn(backendPath, args, {
    cwd: runtimeConfig.baseDir,
    env,
    windowsHide: true,
  });

  backendProc.stdout.on('data', (buf) => {
    const lines = buf.toString().split(/\r?\n/).filter(Boolean);
    appendLogLines(lines);
  });
  backendProc.stderr.on('data', (buf) => {
    const lines = buf.toString().split(/\r?\n/).filter(Boolean);
    appendLogLines(lines);
  });
  backendProc.on('exit', (code) => {
    appendLog(`[EXIT] backend exited with code ${code}`);
    backendProc = null;
  });
  backendProc.on('error', (err) => {
    appendLog(`[DESKTOP] backend spawn error: ${err.message}`);
  });
}

function stopBackend() {
  if (backendProc) {
    try {
      appendLog('[DESKTOP] stopping backend...');
      backendProc.kill();

      // 等待进程退出，最多 3 秒
      let exited = false;
      const timeout = setTimeout(() => {
        if (!exited && backendProc) {
          appendLog('[DESKTOP] backend did not exit gracefully, forcing kill...');
          // Windows 下使用 taskkill 强制杀死
          const { exec } = require('child_process');
          exec(`taskkill /F /PID ${backendProc.pid}`, (err) => {
            if (err) {
              appendLog(`[DESKTOP] taskkill failed: ${err.message}`);
            } else {
              appendLog('[DESKTOP] backend process killed');
            }
          });
        }
      }, 3000);

      backendProc.once('exit', () => {
        exited = true;
        clearTimeout(timeout);
        appendLog('[DESKTOP] backend exited cleanly');
      });
    } catch (err) {
      appendLog(`[DESKTOP] backend kill failed: ${err.message}`);
    }
    backendProc = null;
  }
}

function createWindow() {
  mainWindow = new BrowserWindow({
    width: 1100,
    height: 720,
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
    },
  });
  mainWindow.loadFile(path.join(__dirname, 'renderer', 'index.html'));
  mainWindow.on('close', (event) => {
    if (!isQuitting) {
      event.preventDefault();
      mainWindow.hide();
    }
  });
}

function createTray() {
  // 小红书风格的红色图标 - 16x16 PNG
  const iconPath = path.join(__dirname, 'icon_16.png');
  const icon = nativeImage.createFromPath(iconPath);
  // 确保图标不会被缩放
  icon.resize({ width: 16, height: 16 });
  tray = new Tray(icon);
  const menu = Menu.buildFromTemplate([
    { label: 'Show', click: () => mainWindow.show() },
    { label: 'Exit', click: () => app.quit() },
  ]);
  tray.setToolTip('XHS MCP Desktop');
  tray.setContextMenu(menu);
  tray.on('double-click', () => {
    mainWindow.show();
  });
}

ipcMain.handle('log:get', () => logBuffer);
ipcMain.handle('backend:restart', () => {
  stopBackend();
  startBackend();
  return true;
});
ipcMain.handle('backend:path', () => backendOverridePath || (runtimeConfig ? runtimeConfig.backendPath : ''));
ipcMain.handle('backend:choose', async () => {
  const res = await dialog.showOpenDialog({
    title: 'Select backend exe',
    properties: ['openFile'],
    filters: [{ name: 'Executable', extensions: ['exe'] }],
  });
  if (res.canceled || res.filePaths.length === 0) return '';
  backendOverridePath = res.filePaths[0];
  appendLog(`[DESKTOP] backend override: ${backendOverridePath}`);
  stopBackend();
  startBackend();
  return backendOverridePath;
});

app.on('before-quit', () => {
  isQuitting = true;
  stopBackend();
});

app.on('window-all-closed', (event) => {
  event.preventDefault();
});

app.whenReady().then(() => {
  runtimeConfig = resolveResources();
  createWindow();
  createTray();
  startBackend();
});
