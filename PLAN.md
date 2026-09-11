# infini-enterprise-cli 执行清单

面向 ToB / 私有化部署的 InfiniSynapse 命令行工具。目标：**CLI 可执行 Infini 的所有操作**，重点覆盖看板的创建与编辑。

与现有 `infinisynapse-cli`（`agent_infini`，ToC + AI Agent 场景，仅覆盖 task/db/rag 约 5% 接口面）并行维护，不替换、不合仓。

---

## 1. 目标与非目标

### 目标

- 覆盖 Infini 后端全部 24 个 controller、约 200 个 REST 端点
- 覆盖 Agent 命令通道 `POST /api/ai/message` 的全部 30 种命令类型
- 看板全生命周期：列表、详情、创建、编辑、查询、刷新、版本、布局、删除
- 支持私有化部署：自定义 server / console、多 profile、多租户、License 感知
- 人机双用：交互式 TTY 输出 + `--json` 管道输出 + 非交互脚本模式（CI 可用）

### 非目标

- 不在客户端重新实现 `dashboard-builder` 的 spec 生成逻辑（见 §3.2）
- 不做 Web UI 的替代品（图表渲染、拖拽布局仅做数据与坐标层）
- 不接管 `agent_infini` 的 WinClaw 工具市场分发通道

---

## 2. 技术选型

| 项 | 决定 | 理由 |
|---|---|---|
| 二进制名 | `infini` | 与 ToC 的 `agent_infini` 明确区分 |
| Module path | `github.com/chaozwn/infini-enterprise-cli` | 与现有 CLI 同 owner |
| 语言 | Go 1.26+ | 复用 `infinisynapse-cli` 已验证的 HTTP/SSE/配置实现，单文件分发 |
| 命令框架 | `spf13/cobra` | 与现有 CLI 一致，子命令层级深时可控 |
| 表格输出 | `olekukonko/tablewriter` | 同上 |
| 配置格式 | YAML（`go.yaml.in/yaml/v3`） | 与 `~/.agent_infini/config.txt` 结构兼容 |

### 从 `infinisynapse-cli` 移植（不重写）

- `internal/client/client.go` — HTTP 封装、Bearer、`x-lang`、`APIResponse.code` 解包、1101/1105 token 失效识别
- `internal/client/sse.go` — SSE 流式读取
- `internal/output/output.go` — JSON / Table 双输出
- `internal/config/config.go` — 凭证加载链（需扩展为多 profile）

---

## 3. 三个必须先定下来的核心机制

这三点决定了命令层怎么写，务必在 P0 完成前定稿。

### 3.1 Agent 命令通道是异步队列，不是同步 RPC

`POST /api/ai/message` **只把命令写入 `ai_task_command` 并入队，立刻返回**，真正执行由持有任务 lease 的 Worker 完成，结果只通过 SSE 推送。

因此任何 Agent 类命令（`newTask`、`askResponse`、回滚等）在 CLI 侧都必须是这套流程：

1. 生成 `clientOperationId`（HTTP 重试必须复用同一个值，保证幂等）
2. `POST /api/ai/message`，带 `protocolVersion: 2`，拿到 `taskId` + `commandId`
3. 订阅 `GET /api/ai/events?connId=<uuid>`，跟踪 `command.state`（queued → running → done）
4. 消费 `message.add` / `message.update` / `message.partial` 做流式输出
5. 收到 `state.ready` 后**必须回头全量拉取**（`GET /api/ai_task/getTaskData` 等），以数据库状态为准，不能只依赖增量事件

例外（同步返回，不入队）：`autoApprovalSettings`、无 `taskId` 的 `updateChatMode` / `togglePlanActMode`、`killActiveJobs`。

- [ ] 实现 `internal/agent/command.go`：命令入队 + `clientOperationId` 幂等 + `commandId` 状态机
- [ ] 实现 `internal/agent/events.go`：SSE 多路复用，按 `taskId` 分发，heartbeat 保活与断线重连
- [ ] 实现 `state.ready` 后的全量对账（reconcile）逻辑
- [ ] `clearTask` 不带 `taskId` 时服务端会展开成 per-task 命令，CLI 需处理返回的 `commandIds` 数组

### 3.2 看板是双轨制：REST 做运维，Agent 做创作

后端注释写得很明确：

```
@ApiOperation({ summary: '创建看板（导入/兜底场景，常规创建走 agent）' })
@Post()
```

Web 前端**没有** `createDashboard()`；点「对话创建」发的是一条 `newTask`，提示词就是 `messages.dashboard.newViaChatPrompt`（引导 → 确认蓝图 → 再查数创建）。真正干活的是后端 `dashboard-builder` skill，通过 `dashboard_read` / `dashboard_submit` 两个工具落地。

CLI 必须照抄这个分工：

| 轨道 | 用途 | 手段 |
|---|---|---|
| 确定性轨（REST） | 列表、详情、导出/导入 spec、删卡片、版本、回滚、布局、查询、刷新、筛选项 | 直接调 `/api/ai/dashboards/*` |
| 创作轨（Agent） | 新建看板、按需求改看板、优化布局 | 转 `newTask` / `askResponse`，复用 Web 的引导提示词 |

**不要**在 CLI 里自己拼 DashboardSpec 再 `POST /api/ai/dashboards`。那会跳过 spec 的 zod 校验、Infini-SQL Notebook DAG 水合、`${dash_filter_*}` Filter Contract、datasource id 推断，只适合"导入已有 spec"这一种场景。

编辑看板走 REST 时注意乐观锁：`PUT` 需要先 `GET` 拿 `specHash`，Agent 轨则是 `expected_spec_hash`。

- [ ] 明确 `dash new` / `dash edit` 走 Agent 轨，并在 `--help` 里写清
- [ ] `dash import` 走 REST `POST`，标注为"导入/兜底"
- [ ] 实现 spec 的本地文件往返：`dash export <id> -o spec.json` → 编辑 → `dash import`

### 3.3 认证与租户：api-key 不够用

现有 `agent_infini` 只支持个人 API Key 当 Bearer。ToB 还需要：

- **JWT 登录**：走 `infini-proxy` 的 `POST /auth/loginByUsername`（口令为 `md5(trim(password))`，与 Web 端一致）或 `POST /auth/login`，再用 `GET /user/getJwtProfile` 校验
- **proxy 地址发现**：`GET /api/auth/getAuthingPath`、品牌码 `GET /api/auth/getBrand`
- **租户上下文**：`tenantId` 由服务端从认证结果注入，客户端传的同名字段会被覆盖 —— CLI 不要自己伪造
- **License 限额**：`GET /api/license/limits` 需登录态；`newTask` 会被 `LicenseGuardService.assertCanCreateTask` 拦截，CLI 要能把限额错误翻译成可读提示
- **多 profile**：一套二进制要能在多个私有化环境 / 多个账号间切换

- [ ] 配置结构从单 `global` 扩展为 `profiles.<name>`，保留 `global` 兼容读取
- [ ] `--profile` 全局 flag + `INFINI_PROFILE` 环境变量
- [ ] Token 过期自动刷新（1101 / 1105 触发重登，而非直接报错退出）

---

## 4. 命令清单（按后端模块全量映射）

全局前缀 `/api`。下表是命令设计与端点的对应关系，勾选即代表该组已实现并自测通过。

### 4.1 `infini auth` / `infini config` — 认证与配置

- [x] `auth login [--username] [--password-stdin]` → proxy `/auth/loginByUsername`，自动发现 console 并落盘
- [x] `auth whoami` → `/user/getJwtProfile`
- [x] `auth logout`（`GET /auth/logout` + 清本地凭证）、`auth status`（纯本地）
- [x] `auth mode` → `/api/auth/getAuthingPath`、`/api/auth/getBrand`
- [x] `config ls` / `get` / `set` / `unset` / `path`
- [x] `config profile ls` / `use` / `add` / `rm`
- [x] `config doctor` — 配置、连通性、console 发现、凭证有效性、License 一体化自检

### 4.2 `infini dash` — 看板（**P1 重点**）

REST 轨，`/api/ai/dashboards`：

- [x] `dash ls [--project]` → `GET /`
- [x] `dash show <id> [--spec]` → `GET /:id`，附带 filter / query / widget 摘要
- [x] `dash export <id> [-o file] [--spec-only] [--revision N]` → 导出可回写的 bundle
- [x] `dash import <spec.json>` → `POST /`（兼容裸 spec 与 bundle 两种输入）
- [x] `dash apply <id> <spec.json> [--change-brief]` → `PUT /:id`，带客户端 specHash 冲突检查
- [x] `dash rm <id>` → `DELETE /:id`
- [x] `dash widget ls <id>`、`dash widget rm <id> <widgetId>` → `DELETE /:id/widgets/:widgetId`
- [x] `dash revisions <id>` → `GET /:id/revisions`
- [x] `dash revision <id> <rev>` → `GET /:id/revisions/:revision`
- [x] `dash rollback <id> --revision N` → `POST /:id/rollback`
- [x] `dash layout get/set <id>` → `PATCH /:id/layout`，本地校验 12 栅格与 widget id
- [x] `dash layout commit <id>` → `POST /:id/layout/revision`
- [x] `dash query <id> [--query-ids ...] [--filter k=v]` → `POST /:id/query`，默认跑全部数据查询
- [x] `dash table-query <id> --widget ... --page N` → `POST /:id/table-query`
- [x] `dash filter ls <id>` → 从 spec 列出 filter 及传参示例
- [x] `dash filter options <id> <name>` → `POST /:id/filter-options`
- [x] `dash filter range <id> <name>` → `POST /:id/filter-range`
- [x] `dash refresh <id> [--wait]` → `POST /:id/refreshes`（202）+ 轮询到终态
- [x] `dash refresh status <id> <refreshId>` → `GET /:id/refreshes/:refreshId`
- [x] `dash refresh active <id>` → `GET /:id/refreshes/active`（无任务返回 null，非错误）
- [x] `dash refresh result <id> <refreshId> <queryId>` → `GET .../results/:queryId`
- [x] `dash refresh cancel <id> <refreshId>` → `DELETE /:id/refreshes/:refreshId`
- [x] `dash ask <id> [--widget]` → `POST /:id/ask`（生成上下文附件）

REST 轨实现要点：

- **filter 值按 spec 声明的类型转换**：`number` 转 JSON number、`daterange` 转 `{start,end}` 或 `{preset}`、多选 enum 转数组。靠文本形状猜类型两个方向都会错（`number` 拒绝字符串 `"10"`，`text` 拒绝数字 `10`），所以先读 spec 再转，未知 filter 名在本地就报错并列出合法名字。
- **`apply` 的冲突检查是客户端的**：REST `PUT` 的 DTO 不接受 `expected_spec_hash`（只有 Agent 轨的 `dashboard_submit` 有），所以 `export` 把 specHash 写进 bundle，`apply` 先比对再写，`--force` 可跳过。这挡住「两个人同时改一块看板」的常见窗口，不是严格的竞态保护。
- **`query` 默认排除 `filter_options` 查询**：这类查询只用于填充筛选下拉，query 接口会直接拒绝。
- **部分失败不算失败**：单个 query 失败在结果里逐条报告，只有全部失败才以退出码 1 结束。

Agent 轨：

- [x] `dash new --brief "..."` → `newTask`，**默认非交互**：一条 brief 让 Agent 建完，CI 友好
- [x] `dash new --guided --interactive` → 注入与 Web 端一致的引导提示词，走多轮确认
- [x] `dash edit <id> --brief "..."` → `newTask`，先 `dashboard_read` 再 patch；发起前先校验 `canWrite`
- [x] `dash chat <id> --question "..."` → `dash ask` 产出上下文后内联进 prompt，并挂上 pack 里的数据库与项目
- [x] `dash reply <taskId> [message] [--approve|--reject]` → `askResponse`，回答 Agent 的提问
- [x] `dash cancel <taskId>` → `cancelTask`
- [x] 流式渲染 `dashboard_read_result` / `dashboard_submit_result` 两类事件
- [x] `dashboard_submit` 被拒时把 `errors[]` 逐条可读化输出（含 spec 路径与错误码）

Agent 轨实现要点（`internal/agent`）：

- **必须先订阅 SSE 再发命令**：命令可能在后来的订阅者接上之前就跑完，那条结果就永久丢了。这条顺序有测试兜住。
- **`clientOperationId` 幂等 + `commandId` 跟踪**：一条 SSE 连接承载该用户的**全部**命令（含其他终端和浏览器标签），所以 `command.state` 事件按 `clientOperationId` 过滤，不是自己的不处理。
- **`state.ready` 后按 `GET /api/ai/state` 对账**：流可能被中途掐断，任务自身的状态才是权威，退出前统一回读一次。
- **模型参数可选但必须成对**：服务端只在传了其中一个时才校验配对，不传则沿用账号配置；`subAgentModelInheritMain=false` 时必须给出子 Agent 模型。这些在本地就拦。
- **提问一律结束当前流**：回答是同一任务上的一条新命令（新幂等键、新流），所以多轮循环放在 `Converse` 里，交互与非交互共用一条代码路径。

### 4.3 `infini task` — 任务

`/api/ai_task`：

- [ ] `task ls` → `GET /list`；`task statuses` → `POST /statuses`
- [ ] `task show <id>` → `GET /showTaskWithId/:id`；`task info <id>` → `GET /getTaskInfo/:id`
- [ ] `task rm <id...>` → `POST /deleteTaskWithId`
- [ ] `task cancel <id>` → `POST /cancelTask`
- [ ] `task pin <id>` → `POST /setPinned`
- [ ] `task graph <id>` → `GET /tasks/:taskId/notebook-graph`
- [ ] `task sql <id> --node ...` → `POST /native-query-sql`（展开 `infini_ref` 得原生 SQL）
- [ ] `task kpi-sql` → `POST /runKpiSql`
- [ ] `task share set/get` → `POST /setShare`、`GET /shareStatus`
- [ ] `task evidence` → `POST /toolEvidence`
- [ ] `task msg <id>` → `GET /getUiMessageById`、`GET /messagePayload`
- [ ] `task workspace <id>` → `GET /getTaskWorkspace/:id`
- [ ] `task file ls/preview/download` → `GET /tools/taskFileTree/:taskId`、`POST /previewFile`、storage 下载
- [ ] `task zip <id>` → `GET /downloadZip`
- [ ] `task public *` → `publicTask`、`publicMessagePayload`、`publicToolEvidence`、`publicTaskFileTree`、`publicPreviewFile`、`publicDownloadTaskFile`、`publicDownloadZip`

### 4.4 `infini agent` — Agent 命令全集

`POST /api/ai/message`，30 种 type 全覆盖：

- [ ] `agent new` (`newTask`)、`agent ask` (`askResponse`)、`agent options` (`optionsResponse`)
- [ ] `agent approve` / `agent deny` (`askResponse` + `yesButtonClicked` / `noButtonClicked`)
- [ ] `agent cancel` (`cancelTask`)、`agent clear` (`clearTask`)、`agent stop-shell` (`stopShellSession`)
- [ ] `agent stop-tool` (`stopToolExecution`)、`agent kill-sub` (`killSubAgent`)、`agent kill-jobs` (`killActiveJobs`)
- [ ] `agent rollback` (`rollbackToSnapshot`、`rollbackAndSendMessage`、`editFirstMessageAndResend`)
- [ ] `agent mode plan|act` (`togglePlanActMode` / `updateChatMode`)
- [ ] `agent auto-approve` (`autoApprovalSettings`)
- [ ] `agent resources` (`updateTaskResources`)、`agent tool-params` (`updateTaskToolParams`)、`agent engine` (`updateTaskEngine`)
- [ ] `agent summary start/stop/save` (`summary_task_start` / `summary_task_stop` / `save_task_summary`)
- [ ] `agent export` (`exportTaskWithId`)、`agent models refresh` (`refreshOpenAiModels`)
- [ ] 其余 UI 语义命令（`selectImages`、`openImage`、`openSettings`、`openExtensionSettings`、`checkIsImageUrl`）按需暴露或显式标注不适用
- [ ] `agent state` → `GET /ai/state`；`agent ping` → `GET /ai/ping`
- [ ] `agent settings` → `POST /ai/settings`；`agent configuration` → `GET /ai/configuration`
- [ ] `agent models` → `GET /ai/models`
- [ ] `infini events [--task]` → `GET /ai/events` 原始事件流（调试用）

### 4.5 `infini db` — 数据源

`/api/ai_database`：

- [ ] `db ls` → `GET /list`
- [ ] `db add` / `db update` → `POST /add`、`POST /update`
- [ ] `db rm` → `POST /delete`
- [ ] `db enable` / `db disable` → `POST /enabled`
- [ ] `db test` → `POST /testConnection`
- [ ] `db show <id|name>` → `GET /getDatabaseById/:id`、`GET /getDatabaseByName/:name`
- [ ] `db schema <id>` → `GET /schema/:databaseId`
- [ ] `db upload <id> <file>` → `POST /upload/:databaseId`（file 类型数据源）
- [ ] `db bind-rag` / `db binds` → `POST /bindRags`、`GET /getBindRags/:databaseId`
- [ ] `db review-list` → `GET /context-hub-review-databases`

### 4.6 `infini rag` — 知识库

`/api/ai_rag_sdk`：

- [ ] `rag ls` / `rag ls --all` → `GET /`、`GET /all`
- [ ] `rag show <id>` → `GET /:id`
- [ ] `rag create` / `rag update <id>` → `POST /create`、`POST /update/:id`
- [ ] `rag rm` → `POST /delete`
- [ ] `rag enable` / `rag disable` → `POST /enabled`
- [ ] `rag file ls/download/rm` → `POST /fileTree`、`POST /download`、`POST /deleteRemoteFile`
- [ ] `rag bind-db` / `rag binds` → `POST /bindDatabases`、`GET /getBindDatabases/:ragId`

### 4.7 `infini project` — 项目与成员

`/api/ai_project`：

- [ ] `project ls` / `create` / `update` / `rm` → `GET /list`、`POST /`、`PATCH /:projectId`、`DELETE /:projectId`
- [ ] `project member ls/add/update/rm` → `/:projectId/members` 四个方法
- [ ] `project tree` → `GET /:projectId/tree`
- [ ] `project file preview/download/move/copy/rm` → 对应 4 个文件路由
- [ ] `project mkdir` → `POST /:projectId/directories`

### 4.8 `infini hub` — Context Hub（语义层）

`/api/ai/context-hub`，端点最多的一组：

- [ ] `hub memory start/batch/active/cancel/status/latest` → `memory-build/*` 6 个端点
- [ ] `hub table ls/add/update/rm` + `hub table dbs` → `table-data/*`
- [ ] `hub column ls/add/update/rm` → `table-column/*`
- [ ] `hub playbook ls/add/update/rm` → `playbook/*`
- [ ] `hub kpi ls/add/update/rm` → `kpi/*`
- [ ] `hub pref ls/add/update/rm` → `preference/*`
- [ ] `hub draft add/ls/table-updates` → `draft/*`
- [ ] `hub review pending/approve/reject/restore/translate` → `review/*`

### 4.9 `infini skill` / `infini tool` / `infini rule` / `infini template`

- [ ] `skill ls/available/install/uninstall/toggle/state/upload/edit/rm` → `/api/ai_skill/*` 9 个端点
- [ ] `tool ls/local/installed/state/is-installed/install/uninstall/toggle/upload/edit/rm` → `/api/ai_tool/*` 11 个端点
- [ ] `rule ls/add/update/rm/enable/show/enabled/all/databases` → `/api/ai_rule/*` 10 个端点
- [ ] `template ls/show/create/update/rm` → `/api/ai_template/*` 5 个端点

### 4.10 `infini schedule` — 定时任务

`/api/ai_scheduler`：

- [ ] `schedule ls` / `show` / `runs` → `GET /`、`GET /:id`、`GET /:id/runs`
- [ ] `schedule create` / `update` → `POST /`、`PUT /:id`
- [ ] `schedule pause` / `resume` → `PATCH /:id/pause`、`PATCH /:id/resume`
- [ ] `schedule run` → `POST /:id/run`
- [ ] `schedule archive` → `DELETE /:id`

### 4.11 `infini engine` / `infini runtime` — 引擎与运行时

- [ ] `engine status/check/start/stop/ensure/logs` → `/api/infinity-sql/*` 6 个端点
- [ ] `engine available` / `engine enabled` → `/api/ai_byzer/available`、`/getEnabledInfiniSQLEngine`
- [ ] `runtime instances` → `GET /api/runtime/instances`
- [ ] `runtime execution <taskId>` → `GET /api/runtime/tasks/:taskId/execution`
- [ ] `runtime ready` / `drain` / `autoscaling` → 对应端点（含 `POST /drain`）

### 4.12 `infini license` — 授权

- [ ] `license status` → `GET /api/license/status`（公开，未登录可用）
- [ ] `license refresh` → `POST /api/license/refresh`
- [ ] `license limits` → `GET /api/license/limits`（需登录）
- [ ] 全局：`newTask` 被限额拦截时输出可读提示与联系方式

### 4.13 `infini fs` — 文件与网盘

- [ ] `fs ls/tree/mkdir/rm` → `/api/directories`、`/api/fileTree`、`/api/createDirectory`、`/api/deleteDirectory`
- [ ] `fs upload <dir> <file>` → `POST /api/upload/:directory`
- [ ] `fs config` → `GET /api/uploadConfig`
- [ ] `fs task-upload <taskId>` → `POST /api/taskUpload/:taskId`
- [ ] `fs download <id>` → `GET /api/storage/download/:id`、`downloadTaskFile/:taskId`
- [ ] `fs rm` → `POST /api/storage/delete`
- [ ] `fs upload-large` → `/api/file_upload` 分片 5 步（init → status → chunks → complete → abort）

### 4.14 `infini browser` — 浏览器自动化

`/api/ai_browser`：

- [ ] `browser sessions` / `session [uid]` → 3 个查询端点
- [ ] `browser navigate/click/input/scroll/key/find/view/move` → `action/*` 8 个端点
- [ ] `browser action --raw` → `POST /action` 通用入口

### 4.15 `infini api` — 逃生舱（保证 100% 覆盖）

- [x] `infini-cli api <METHOD> <path> [--data @file|@-] [--query k=v] [--proxy] [--raw]` — 任意端点直调，带上当前 profile 的鉴权
- [x] `infini-cli api endpoints` — 内置端点索引，便于发现未包装的接口

这一条是"CLI 可执行所有操作"的兜底保证：新增后端接口在未包装成子命令前，也能立即通过 `infini api` 调用。

---

## 5. 分期执行顺序

### P0 — 骨架与地基 ✅

- [x] `go mod init`、目录结构、`Makefile`（build / cross / install / test / lint / clean）
- [x] 移植并改写 `internal/client`、`internal/output`、`internal/config`
- [x] 多 profile 配置 + `--profile` + 环境变量覆盖 + `INFINI_CONFIG` 路径隔离
- [x] 全局 flag：`--server` `--console` `--api-key` `--token` `--tenant-code` `--json` `--table` `--yes` `--timeout` `--lang` `--verbose` `--trace`
- [x] 统一输出协议与退出码（见 §6），服务端错误码 → 退出码 + 修复提示
- [x] `auth login` / `whoami` / `status` / `logout` / `mode`
- [x] `config ls|get|set|unset|path`、`config profile ls|use|add|rm`、`config doctor`
- [x] `infini-cli api` 逃生舱 + `api endpoints` 索引
- [x] `infini-cli events` SSE 订阅器（过滤、退避重连、鉴权失败不重试）
- [x] 配置优先级与 profile 行为的回归测试
- [x] 命令状态机 `internal/agent`（`clientOperationId` 幂等 + `commandId` 跟踪 + `state.ready` 对账）
- [ ] 针对真实部署的 `auth login` / `doctor` / `dash new` 端到端验证（需要本地 Infini + proxy 起服务）

### P1 — 看板（核心交付）

REST 运维轨与 Agent 创作轨**同期交付**。

- [x] §4.2 REST 轨全部命令（含 `dash export`、`dash widget ls`、`dash filter ls` 三个清单外的补充命令）
- [x] spec 本地往返（export → 编辑 → import / apply，带 `specHash` 冲突检测）
- [x] 看板刷新的长轮询（`--wait`，进度写 stderr，终态决定退出码）
- [x] filter 类型转换、请求报文与错误码映射的单元与集成测试
- [x] §4.2 Agent 轨 `dash new` / `dash edit` / `dash chat` / `dash reply` / `dash cancel`
- [x] 流式渲染 `dashboard_read_result` / `dashboard_submit_result`，`dashboard_submit` 被拒时逐条输出 `errors[]`
- [x] 命令状态机 `internal/agent`（订阅先行、幂等键、`command.state` 过滤、`state.ready` 对账），含假服务测试

### P2 — 资源与数据面

- [ ] §4.3 task 全量
- [ ] §4.5 db 全量
- [ ] §4.6 rag 全量
- [ ] §4.7 project 全量
- [ ] §4.8 hub 全量（含 KPI 与审核流）

### P3 — Agent 深度控制

- [ ] §4.4 全部 30 种命令类型
- [ ] 审批/选项应答的交互式 TUI
- [ ] 回滚三兄弟 + `state.ready(forceReplace)` 对账
- [ ] 模型与子 Agent 模型切换的参数联动校验（`apiProvider`/`apiModelId` 必须成对，`subAgentModelInheritMain=false` 时子模型必填）

### P4 — 运维与治理

- [ ] §4.9 skill / tool / rule / template
- [ ] §4.10 schedule
- [ ] §4.11 engine / runtime
- [ ] §4.12 license
- [ ] §4.13 fs（含分片上传）
- [ ] §4.14 browser

### P5 — 交付与分发

- [ ] `infini skill` — 输出 AI Agent 规范说明（对标 `agent_infini skill`，但面向企业命令集）
- [ ] 多平台交叉编译（linux/darwin/windows × amd64/arm64）
- [ ] 版本清单 + `--update` 自更新（**独立分发通道，不复用 `plugins/infini_cli`**）
- [ ] README + 命令参考文档
- [ ] 单测（命令解析、参数校验、输出格式）+ 针对本地 Infini 的 e2e 冒烟
- [ ] 私有化交付物：离线包、审计日志、`--dry-run`

---

## 6. 全局交付基线

- **输出协议**：默认 JSON `{"success":bool,"data":any,"message":string}`；列表命令 `--table` 走表格；所有输出可 `jq`
- **退出码**：0 成功；1 业务错误；2 参数错误；3 鉴权失败；4 网络/服务不可达；5 License 限额拦截
- **错误可读化**：后端 `code` → 人类可读提示 + 修复建议（沿用 1101/1105 token 失效的处理思路，扩展到 License、租户、权限）
- **危险操作**：`rm` / `rollback` / `drain` / `archive` 默认二次确认，`--yes` 跳过，非 TTY 环境必须显式 `--yes`
- **幂等**：所有写操作生成 `clientOperationId`，重试复用
- **可观测**：`--verbose` 打印请求摘要，`--trace` 打印完整请求/响应（自动脱敏 api-key 与密码）

---

## 7. 决策记录

已定：

1. 二进制名 `infini`，module path `github.com/chaozwn/infini-enterprise-cli`
2. P1 看板 REST 轨与 Agent 轨同期交付
3. `dash new` 默认非交互（`--brief` 一次建完），`--interactive` 为可选引导模式
4. MCP server 模式（`infini mcp serve`）延后到 P5 之后再评估，当前不纳入范围

仍待定（不阻塞 P0 开工）：

5. 自更新通道的 OSS 前缀（不能与 `agent_infini` 的 `plugins/infini_cli` 混用）
6. 是否把 `infini` 与 `agent_infini` 共享的 HTTP/SSE 抽成独立 Go SDK 包（先各自维护，P5 评估）
