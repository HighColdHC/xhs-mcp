const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('electronAPI', {
  getLogs: () => ipcRenderer.invoke('log:get'),
  onLogAppend: (cb) => ipcRenderer.on('log:append', (_event, lines) => cb(lines)),
  restartBackend: () => ipcRenderer.invoke('backend:restart'),
  getBackendPath: () => ipcRenderer.invoke('backend:path'),
  chooseBackend: () => ipcRenderer.invoke('backend:choose'),
});
