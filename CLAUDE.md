# CLAUDE.md - 项目开发指南

## 项目概述

这是一个小红书 MCP 服务项目，提供通过 MCP 协议和 HTTP API 操作小红书的能力。

### 技术栈
- **后端**: Go (Gin框架) + Rod (浏览器自动化)
- **前端**: Electron + 原生 JavaScript
- **浏览器**: Chrome/Chromium (无头/可视化模式)

### 目录结构
```
d:/xhs-mcp/
├── backend/           # Go 后端服务
│   ├── accounts/      # 账号管理
│   ├── browser/       # 浏览器封装
│   ├── cookies/       # Cookie 持久化
│   ├── session/       # 会话管理
│   ├── xiaohongshu/   # 小红书业务逻辑
│   ├── service.go     # 核心服务层
│   ├── handlers_api.go # HTTP API 处理器
│   └── main.go        # 程序入口
├── desktop/           # Electron 桌面端
│   ├── renderer/      # 前端页面
│   │   └── renderer.js # 前端逻辑
│   ├── main.js        # Electron 主进程
│   └── package.json
└── data/              # 运行期数据
```

---

## 核心原则

> **稳定性 > 可控性 > 功能扩展 > 代码优雅**

- 只做明确要求的事情
- 不追求"最佳实践式重构"
- 不"顺便优化"或"顺手整理"

---

## 强制规则

### 1. 分支规则
- 每次修改必须基于新分支
- 一个分支只解决一组明确的问题
- 未经用户确认测试通过，不允许合并到主分支

### 2. 修改范围规则
- 不允许修改未被点名的文件
- 不允许修改公共基础模块（除非明确要求）
- 不允许"顺便整理结构/命名/目录"

### 3. 严禁行为
- 大规模重构
- 改函数签名
- 改已有接口入参/出参
- 删除日志
- 替换已有实现为"更优雅的方案"
- 引入新依赖（除非明确允许）

---

## 工作流程

### 标准流程
1. **理解需求** - 用自然语言复述改造目标
2. **提出方案** - 说明改哪几个文件、每个文件做什么、是否影响已有逻辑
3. **等待确认** - 未确认前不写代码
4. **最小改动实现** - 只做当前目标
5. **清晰 diff** - 保证 `git diff` 可读

### 不确定情况处理
如果出现以下情况，必须先停下来提问：
- 不确定原作者设计意图
- 不确定某段逻辑是否可删
- 不确定是否会影响多账号/并发
- 不确定是否该放在当前模块

---

## 关键代码说明

### 账号登录流程
```
StartVisibleWindow (service.go:273)
    ↓
启动可视化浏览器
    ↓
WaitForLogin (异步 goroutine)
    ↓
saveCookies
    ↓
MarkLoggedIn (accounts/manager.go:194)
    ↓
saveLocked (持久化到 accounts.json)
```

### 登录状态更新机制
- `MarkLoggedIn`: 更新内存中的 `LoggedIn` 状态并保存到文件
- 前端通过 `/api/v1/accounts` 接口获取账号列表
- 前端轮询机制检测登录状态变化

### 重要文件位置
- [backend/service.go](backend/service.go) - 核心业务逻辑
- [backend/handlers_api.go](backend/handlers_api.go) - HTTP API
- [backend/accounts/manager.go](backend/accounts/manager.go) - 账号管理
- [desktop/renderer/renderer.js](desktop/renderer/renderer.js) - 前端逻辑
- [backend/xiaohongshu/login.go](backend/xiaohongshu/login.go) - 登录逻辑

---

## 已知问题与注意事项

### 1. 后端编译产物
`backend/` 目录下有大量 `.exe` 文件应被 `.gitignore` 排除：
- `xhs-mcp.exe`
- `xhs-mcp-sched-fix*.exe` (多个版本)

### 2. 默认账号ID
多处代码使用硬编码的默认账号ID=1，需注意：
- `parseAccountID` 函数 (handlers_api.go:50-58)
- 各 API 处理器中的默认值处理

### 3. 外部依赖
- `renderIPAndFingerprint` 使用 `https://api.ipify.org` 获取 IP

### 4. 安全问题
- Cookies 以 JSON 格式明文存储
- 代理密码明文存储

---

## 最近修复记录

### 登录状态自动刷新 (2025-12-26)
**问题**: 登录成功后前端界面状态未及时更新

**修改文件**:
- [desktop/renderer/renderer.js](desktop/renderer/renderer.js#L230-L263)

**修改内容**:
1. 添加 `pollAccountLoginStatus` 函数：轮询检查登录状态
2. 修改 `startAccountWindow` 函数：启动窗口后自动轮询，登录成功时刷新列表

**测试要点**:
- 启动账号窗口后扫码登录
- 验证界面是否在几秒内自动更新为"已登录"

---

## 提交确认清单

任务完成时必须输出：
- 本次修改解决了什么
- 修改了哪些文件
- 是否可能影响旧功能
- 哪些地方需要重点测试
