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
  if (!fs.existsSync(dest)) {
    shouldCopy = true;
  } else {
    try {
      const srcStat = fs.statSync(src);
      const destStat = fs.statSync(dest);
      if (srcStat.size !== destStat.size) {
        shouldCopy = true;
      }
    } catch (_err) {
      shouldCopy = true;
    }
  }
  if (shouldCopy) {
    fs.copyFileSync(src, dest);
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
  return path.resolve(__dirname, '..', 'backend', 'xhs-mcp-sched-fix26.exe');
}

function resolveDevChromiumDir() {
  return path.resolve(__dirname, 'vendor', 'chromium', 'chrome-win64');
}

function resolveResources() {
  const baseDir = resolveBaseDir();
  const dataDir = path.join(baseDir, 'xhs-data');
  ensureDir(dataDir);
  ensureDir(path.join(dataDir, 'profiles'));
  ensureDir(path.join(dataDir, 'cookies'));

  let backendSrc = '';
  let chromiumSrcDir = '';
  if (app.isPackaged) {
    backendSrc = path.join(process.resourcesPath, 'backend', 'xhs-mcp.exe');
    chromiumSrcDir = path.join(process.resourcesPath, 'chromium', 'chrome-win64');
  } else {
    backendSrc = resolveDevBackendPath();
    chromiumSrcDir = resolveDevChromiumDir();
  }

  const backendDest = path.join(dataDir, 'backend', 'xhs-mcp.exe');
  const chromiumDestDir = path.join(baseDir, 'chromium', 'chrome-win64');
  const chromiumExe = path.join(chromiumDestDir, 'chrome.exe');

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
      backendProc.kill();
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
  const iconPng =
    'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAYAAAAf8/9hAAABKUlEQVQ4T6WTvUoDQRSGv6G0sNQH0EAWEHqLQH0BCLYQ8u0b3m4q0BE3Q0a4VNoQWJqY0Y9k7uOZ2kXoJt2Ew8f5zPz7oPz7v2k4Sx8Q4B+1S4Qp5U4vT0pGzqv9mX8oK2G+6oQ2m0q5oGQ0x3G6c7mK0q8M2cOQ6rJ1tQp9lVtHc0PpWZ1uS3K1S2wR8e5xH0p9kF2hQxGZr+H2cQj1s8qE1wF7GqOQq2b0S1AqW9c0y2zGQk8q9sC2gEw1bFokA2Ff3x3j0jI8B1Lq7p3rW0kQbGkK7QqgXgP9wJ2c8j3l8gY8dCj3Tg+o0bVwQnD0p4xwcoz4b6RzE7+3z9w8YQm0o6j0JbK6s6U4JXy0xJ4mXyQ1o6t8g2oO0fJx1jL3W+M3sAAAAASUVORK5CYII=';
  const icon = nativeImage.createFromDataURL(iconPng);
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
