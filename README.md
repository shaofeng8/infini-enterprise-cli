# infini-cli

面向 ToB / 私有化部署的 InfiniSynapse 命令行工具。目标是让 CLI 能执行 Infini 的所有操作，重点覆盖看板的创建与编辑。

与 [`infinisynapse-cli`](../infinisynapse-cli)（`agent_infini`，面向 ToC 与 AI Agent 场景）并行维护，两者不共享代码包，各自独立发版。

完整功能清单与实施进度见 [PLAN.md](./PLAN.md)。

## 当前状态

- **P0 地基** ✅ 配置与多 profile、认证、HTTP/SSE 客户端、退出码语义、`api` 逃生舱、`events` 事件流
- **P1 看板 REST 轨** ✅ 列表、查看、导出/导入、版本回滚、布局、查询、刷新、筛选、上下文附件
- **P1 看板 Agent 轨** ✅ 创建、编辑、提问、回答、取消
- **P2 任务** ✅ 列表/查看、状态、置顶、取消、删除、DAG、原生 SQL、KPI SQL、证据、消息、工作区文件与归档、分享
- **P2 资源面** ✅ 数据源（CRUD、连接测试、schema、上传、绑定）、知识库（CRUD、文档存储、绑定）、项目（CRUD、成员、文件）
- **P2 语义层** ✅ 记忆构建、表/列/Playbook/KPI/偏好 CRUD、草稿与审核流
- **P3 Agent 深度控制** ✅ 24 种命令类型全覆盖、审批/选项应答、回滚与改写首条、模式与资源/引擎/工具参数、能力开关、模型配置
- **P4 扩展面** ✅ 技能、工具、规则、模板
- **P4 运维面** ✅ 定时任务、引擎与运行时、license、文件与分片续传、浏览器自动化
- **P4 分享与审计** ✅ 公开只读与超管审计读、证据回溯
- **P5 交付与分发** ✅ AI Agent 规范输出、六平台交叉编译、自更新通道、`--dry-run` 与审计日志

未包装的接口随时可用 `infini-cli api` 直调。

## 环境要求

- Go 1.26+
- Make（可选，也可直接用 `go build`）

## 构建

```bash
make build          # 产物在 build/infini-cli
make cross          # 交叉编译 linux/darwin/windows × amd64/arm64
make test           # 单元测试
make lint           # go vet
```

没有 make 也行（Windows 上通常如此）：

```powershell
go build -o build/infini-cli.exe .
go test ./...
```

## 快速开始

```bash
# 1. 指向目标部署（本地可省略：未配置时用 http://127.0.0.1:$APP_PORT，APP_PORT 未设则为 8088）
infini-cli config set server https://infini.example.com
#    或：export APP_PORT=7001


# 2. 登录（auth/proxy 地址会自动从后端发现）
infini-cli auth login --username alice@example.com

# 3. 自检：配置、连通性、凭证、授权状态
infini-cli config doctor --table

# 4. 看板
infini-cli dash ls --table

# 5. 任意接口直调（业务子命令尚未实现时的通用入口）
infini-cli api GET /api/ai/dashboards
```

## 看板

看板工作分两轨：**运维**（列表、查询、刷新、版本、布局）走 REST；**创作**（把业务需求变成一块看板）走 Agent，因为 spec 背后是一整张 Infini-SQL DAG 和筛选契约，由服务端校验和水合。

### 创建与编辑

```bash
infini-cli dash new --brief "近 30 天各渠道 GMV 与转化率趋势" --database db_sales
infini-cli dash edit dash_123 --brief "把转化率卡片换成折线图，并加上环比"
infini-cli dash chat dash_123 --question "为什么华东转化率比上月低？" --filter period=last_30d
```

**默认非交互**，为的是 CI 可预期：Agent 提问时命令会停下来把问题打出来，以退出码 1 结束，你再回答：

```bash
infini-cli dash reply <taskId> "用自然月，不要滚动 30 天"
infini-cli dash reply <taskId> --approve          # 审批类提问
```

想在终端里直接对话，加 `-i`；想让 Agent 先反过来梳理需求再动手，用 `--guided -i`（引导提示词与 Web 端一致）：

```bash
infini-cli dash new --brief "销售看板" --guided -i
```

进度流写 stderr，stdout 只有一份 JSON 结果，所以管道里解析不会被干扰。`--quiet` 关掉进度，`--reasoning` 打开思考过程，`--transcript` 把完整对话放进结果。

Agent 提交被服务端拒绝时，会逐条列出被拒的 spec 路径和错误码，而不是只说一句失败：

```
dashboard submit rejected with 2 error(s):
  widgets[0].query_id [UNKNOWN_QUERY] query q_x does not exist
  filters[1] [BAD_NAME] filter name must be snake_case
```

### 运维

```bash
infini-cli dash ls --table
infini-cli dash show <id> --table
infini-cli dash filter ls <id> --table          # 有哪些筛选，怎么传
infini-cli dash query <id> --filter period=last_30d --filter region=east --table
infini-cli dash refresh <id> --wait --force-refresh
infini-cli dash revisions <id> --table
infini-cli dash rollback <id> --revision 3
```

### 筛选值

筛选值的 JSON 类型取自看板自己的 spec，所以不用关心该传字符串还是数字：

```bash
--filter region=east                    # 文本或单选枚举
--filter tags=a,b                       # 多选枚举，也可重复传 --filter tags=a --filter tags=b
--filter min_pv=100                      # 数字
--filter period=2026-01-01..2026-01-31   # 显式日期区间
--filter period=last_30d                 # 日期区间预设
--filter-values @filters.json            # 完整报文，优先级高于 --filter
```

传错筛选名会在本地就报错并列出该看板的合法名字，不用等服务端拒绝。

### spec 往返

```bash
infini-cli dash export <id> -o board.json
# 编辑 board.json
infini-cli dash apply <id> board.json --change-brief "新增转化率卡片"
```

`export` 会把 `specHash` 写进文件，`apply` 先比对再写，看板被别人改过就中止。这道检查在客户端：REST 接口本身没有乐观锁，所以它挡的是「两个人同时改一块看板」的常见窗口，不是严格的竞态保护。`--force` 可跳过。

`dash import` 与 `dash apply` 都同时接受裸 spec 和导出的 bundle，所以手写的 spec 或从别的部署拷来的 spec 一样能用。

## 任务

一个任务就是一次 Agent 对话，外加它产出的全部东西：查询构成的 notebook DAG、独立的工作目录、每个结果背后的工具证据。任务由 `dash new` 这类 Agent 命令创建，`task` 负责创建之后的一切。

```bash
infini-cli task ls --table
infini-cli task ls --status running --table
infini-cli task status t_1 t_2 --table          # 只读状态，批量轮询用这个，不拉对话
infini-cli task show t_1                         # 含完整对话
infini-cli task cancel t_1
infini-cli task pin t_1
```

### 结果溯源

一个数字是怎么算出来的，靠 DAG 和证据回答，不用重跑任务：

```bash
infini-cli task graph t_1                                 # 节点与依赖
infini-cli task sql t_1 --view kpi_monthly_sales           # 展开 infini_ref 得到原生 SQL
infini-cli task evidence t_1 --id ev_1 --include-subagent  # 工具调用的入参与出参
infini-cli task msg t_1 --ts 1736200000000                 # 列表里被截断的消息全文
```

`task kpi-sql` 可以在存进语义层之前先验证一条 KPI 的 SQL。源表按 `<库>.<表>` 传入，语句里用 `<库>_<表>` 引用，与服务端的注册约定一致：

```bash
infini-cli task kpi-sql --db chinook --table chinook.artists \
    --sql "SELECT count(*) FROM chinook_artists"
```

### 工作区文件

图表、导出、中间数据都落在任务自己的工作目录里：

```bash
infini-cli task file ls t_1 --files-only --table
infini-cli task file preview t_1 data/result.csv   # 解析后的预览，表格会按行返回
infini-cli task file get t_1 charts/sales.png --out ./downloads/
infini-cli task zip t_1 --out ./t_1.zip
```

下载是流式落盘，大归档不受内存限制；中途失败会删掉半个文件，因为截断的归档看起来像正常结果。

### 分享

```bash
infini-cli task share get t_1 --table
infini-cli task share set t_1 --public     # 需确认：任何拿到链接的人都能读
infini-cli task share set t_1 --private
```

## 数据源

连接配置的字段随 `--type` 变，CLI 原样透传给服务端校验。`db test` 可以在保存之前先验证一份配置：

```bash
infini-cli db ls --type mysql --table
infini-cli db add --name chinook --type sqlite --config '{"path":"/data/chinook.db"}'
infini-cli db test --type mysql --config @mysql.json
infini-cli db test --id db_1                    # 测已保存的那份
infini-cli db schema db_1                       # 表、列，以及语义层对它的了解
infini-cli db upload db_1 ./sales-2026.csv      # 仅 file 类型
```

`db update` 会先读当前记录、再只覆盖你传了的 flag——服务端的更新接口要整条记录，直接发部分字段会把没传的清空。

启停是单独的命令：它存在按用户的映射表里，不在数据源本身上，`update` 会忽略它。

```bash
infini-cli db enable db_1 db_2
infini-cli db disable db_1
```

## 知识库

文档本身留在原处：本地路径，或 OSS / S3 / COS 桶。把知识库绑到数据源，是让 Agent 用文档来理解这个库的表。

```bash
infini-cli rag ls --all --table
infini-cli rag create --name finance_docs --doc-dir /data/finance --ext pdf --ext md
infini-cli rag bind-db rag_1 --db db_1 --db db_2   # 替换而非追加，不传则全部解绑
infini-cli rag binds rag_1
```

文档存储按路径直接寻址，因为同一个目录可以撑起多个知识库。对象存储密钥只从 `INFINI_STORAGE_SECRET` 读，不提供 flag——命令行上的密钥会进 shell history 和进程列表：

```bash
export INFINI_STORAGE_SECRET=...
infini-cli rag file ls --fs oss --dir docs/ --endpoint oss-cn-hangzhou.aliyuncs.com --access-key AKID
infini-cli rag file get docs/q3.pdf --fs oss --access-key AKID --out ./downloads/
infini-cli rag file rm docs/stale.pdf --fs oss --access-key AKID   # 仅 oss/s3/cos 支持删除
```

## 语义层

Context Hub 是 Agent 写 SQL 之前读的东西：表和列的描述、分析 Playbook、KPI 定义、展示偏好。空的语义层只能靠猜，描述过的才能给出站得住的答案。

填充它最快的方式是记忆构建——读 schema 和样本数据，生成描述候选交给审核：

```bash
infini-cli hub memory start chinook --wait          # 选表由 CLI 从 schema 推导，默认全选
infini-cli hub memory start chinook --missing-only  # 只补语义层还没覆盖的表
infini-cli hub memory start chinook --table "invoices:id,total,date"
infini-cli hub memory batch db_1 db_2 --mode missing_only
infini-cli hub memory active --table
```

构建跑在服务端，CLI 退出不影响它。同一个数据源同时只有一个构建，重复 start 返回的是已经在跑的那个。

### 手工维护定义

```bash
infini-cli hub table ls --database db_1 --table
infini-cli hub table add chinook invoices --description "订单事实表，一行一个订单行"
infini-cli hub column add t_1 total --meaning "订单金额，含税，单位元"
infini-cli hub playbook add "月度复盘" --database chinook --content @playbook.md
infini-cli hub kpi add 月度销售额 --mode sql_playground --table chinook.invoices --sql @sales.sql
infini-cli hub pref add "金额显示" --value "金额保留两位小数，单位万元" --table chinook.invoices
```

KPI 的 `--mode` 决定哪个字段承载定义：`business_logic` 用 `--logic` 写文字口径，`sql_playground` 用 `--sql` 放语句。SQL 口径存进去之前，可以先用 `task kpi-sql` 验证。

### 审核

改别人拥有的数据源不会直接生效，而是产生一条草稿，所以审核是正常编辑路径的一环，不是管理员的额外工作：

```bash
infini-cli hub review pending --table
infini-cli hub draft ls table_data --status pending --table
infini-cli hub review approve table_data d_1
infini-cli hub review approve table_data d_1 --field table_description    # 只批准这一个字段
infini-cli hub review approve kpi d_2 --set kpi_description="季度口径已修正"  # 边批准边改值
infini-cli hub review reject kpi d_3 --comment "口径与财务对不上"
infini-cli hub review translate --to zh-CN --field table_description="Fact table of orders"
```

## Agent

`dash new` 这类命令是包了业务外壳的 Agent 调用；`agent` 是同一条通道的直接入口，Web UI 能让 Agent 做的事这里都能做。

```bash
infini-cli agent new "统计上季度各区域营收，输出一张表" --database db_1
infini-cli agent new "跑通全流程" --interactive          # 在终端里逐轮回答
infini-cli agent reply t_1 "用自然月口径"
infini-cli agent reply t_1 --approve                     # 或 --deny
infini-cli agent options t_1 '["华东","华南"]'
infini-cli agent messages t_1 --table
```

默认**不交互**：Agent 一提问就停下并把问题报出来，退出码非零。这是给 CI 用的——没人在旁边时挂着等回答，比直接失败更糟。要在终端里对话就加 `--interactive`。

### 停止与回滚

```bash
infini-cli agent cancel t_1                    # 停整个任务
infini-cli agent stop shell t_1 local:t_1:ab   # 只停一个 shell 会话
infini-cli agent stop job t_1 job_7            # 只停一个 SQL job
infini-cli agent stop subagent t_1_sub_2       # 只停一个子 Agent
infini-cli agent clear t_1                     # 释放运行时，对话仍留在库里
```

回滚要先知道能回到哪儿。服务端只在**已提交的用户回合**建快照并按精确时间戳查，所以 `snapshots` 先列出合法的 `--ts`：

```bash
infini-cli agent snapshots t_1 --table
infini-cli agent rollback t_1 --ts 1700000000000
infini-cli agent rollback t_1 --ts 1700000000000 --message "换个口径重来"
infini-cli agent edit-first t_1 "改成按渠道拆，不按区域"
```

带 `--message` 会顺带从那一点继续跑，所以它像一次普通回合那样流式输出；不带就只回滚，新状态随 SSE 到达。两者都是破坏性的，默认需要确认。

### 运行时配置

```bash
infini-cli agent mode plan --task t_1          # 不带 --task 则改账号默认
infini-cli agent resources t_1 --database db_1 --database db_2
infini-cli agent resources t_1 --rag ""        # 空值 = 清空该组
infini-cli agent engine t_1 --engine eng_1     # 或 --clear
infini-cli agent tool-params t_1 --tool tool_1=params.yaml
```

资源组有三态：**不传 = 不动，空值 = 清空，有值 = 替换**。`graph` 和 `fast` 只能在建任务时选，切不进已有任务，CLI 会在发命令前就拦住。

### 能力开关与模型

```bash
infini-cli agent auto-approve --table                    # 不带 flag 就是查看
infini-cli agent auto-approve --max-requests 500 --browser=false
infini-cli agent settings --provider openai --model gpt-4o
infini-cli agent settings --task t_1 --provider anthropic --model claude-sonnet-4
infini-cli agent models --table
```

`auto-approve` 是读-改-写：服务端存的就是收到的那个对象，少传一个字段等于把那项能力关掉，所以 CLI 先读当前值再只覆盖你传的那几个。模型 API key 只从 `INFINI_MODEL_API_KEY` 读，不提供 flag。

### 逃生舱

```bash
infini-cli agent send togglePlanActMode --task t_1 --field chatSettings='{"mode":"plan"}'
```

`send` 会校验 type。这一步不是多余的：`/api/ai/message` 的 body 在服务端根本没走校验（签名上的类型是 `import type` 来的，运行时已擦除），未知 type 会被接受、入队，然后在 worker 里只留一行日志就丢掉——调用方看到的是 `{"queued": true}`，一个伪成功。

## 项目

项目把任务、看板和文件圈到一组人身上，成员角色决定谁能改什么。角色有 `viewer`、`editor`、`manager`。

```bash
infini-cli project ls --table
infini-cli project create "销售分析" --description "季度复盘"
infini-cli project member add proj_1 user_7 --role editor
infini-cli project member ls proj_1 --table
infini-cli project tree proj_1
infini-cli project file get proj_1 data/result.csv --out ./downloads/
infini-cli project mkdir proj_1 data/raw
```

## 扩展 Agent 的能力

四样东西决定 agent 能做什么：**技能**（一份 SKILL.md 加配套文件，告诉它怎么做某件事）、**工具**（可调用的外部命令）、**规则**（每次都前置的常驻指令）、**模板**（存起来的提示词）。

技能和工具都有两个来源：发布到 proxy 的 catalog（`install` 装）和你自己上传的压缩包（`upload` 传）。两者的 id 不通用——`uninstall` 收 catalog id，`rm` 收本地安装行的 id。

```bash
infini-cli skill available --table            # 当前可达的技能
infini-cli skill available --task t_1 --table  # agent 在这个任务里实际能看到的
infini-cli skill install <skillId> my-skill
infini-cli skill upload ./my-skill.zip
infini-cli skill toggle <skillId> inactive

infini-cli tool ls --source local --table
infini-cli tool install <pluginId> --name 数据分析 --alias data-analysis --author admin
infini-cli tool state <pluginId>
```

`skill available` 是随上下文变的：服务端按任务挂载的数据源类型和浏览器是否开启过滤 catalog，所以同一账号对不同任务看到的列表不同。

规则的作用域要么全局，要么绑定到具体数据源，这样「某个仓库的口径」不会跟着 agent 到处跑。想知道当前真正生效的是哪些，看 `rule enabled`——那是 agent 自己收到的那一份。

```bash
infini-cli rule add "时间范围" --value "查询必须带时间范围限制"
infini-cli rule add "chinook 口径" --value @rule.md --database db_1
infini-cli rule enabled --table
infini-cli rule toggle 1 2 3 --off

infini-cli template create "月度复盘" --text @monthly.md
infini-cli template ls --table
```

模板只是存储，没有任何命令会展开或执行它；要用就读出文本交给 `agent new`。

## 定时任务

定时任务按 cron 触发一段提示词，产出的就是普通任务，所以 `task` 和 `agent` 下所有命令对结果都适用。

```bash
infini-cli schedule create "每日销售简报" \
    --prompt "汇总昨日销售数据并生成简报" \
    --cron "0 30 9 * * ?" \
    --database db_sales

infini-cli schedule ls --table
infini-cli schedule runs s_1 --table
infini-cli schedule run s_1          # 立刻跑一次，不影响 cron
infini-cli schedule pause s_1
```

两件容易踩的事。cron 是**六段 Quartz**（秒 分 时 日 月 周），`"0 30 9 * * ?"` 是每天 09:30；五段的 Unix 写法会被当场拦下。另外，运行配置在保存时就冻结了，不是触发时解析的——夜间报表会一直用当初设定的模型和数据源，账号默认值变了也不跟随。这是刻意的，代价是改默认值不会更新已有计划，得 `schedule update`。

`schedule runs` 里每条记录都带它产出的 `taskId`，失败的报表可以直接追进那段对话。标了 misfire 的是重启后补发，而非按时触发的。

## 运维

「引擎」是两个不同的东西。`engine status/start/stop` 管的是本部署内嵌的那个进程，全员共用——所以 `engine stop` 会打断这台机器上所有人的查询。`engine available` 列的是你的账号可以绑定的引擎，那才是 `agent engine` 和 `schedule --engine` 收的 id。

```bash
infini-cli engine check                 # 运行中退出 0，未运行退出 3
infini-cli engine ensure                # 脚本里用这个，start 不幂等
infini-cli engine logs --limit 200
infini-cli engine available --table
```

任务不在 API 进程里跑：worker 领走一个任务（lease）、执行、续租；worker 挂了 lease 过期，另一个接手。所以任务一直排队而 API 日志干干净净是正常现象，第一站看 `runtime execution`：

```bash
infini-cli runtime instances --table
infini-cli runtime execution t_1
infini-cli runtime autoscaling
INFINI_INTERNAL_TOKEN=... infini-cli runtime drain
```

`instances` 里 `REPORTED` 是实例自己上报的状态，`EFFECTIVE` 是注册表的结论——心跳断了就是 offline，不管它自称什么。drain 打到哪个 worker 取决于请求落到哪个实例，要指定就把 `--server` 指向具体实例。

授权这边，synapse 自己不做判定，只透传 proxy 的结论，所以这里失败通常意味着 proxy 不可达而不是 license 无效：

```bash
infini-cli license status     # 不需要登录
infini-cli license limits     # 需要登录，配额按账号算
```

## 文件

```bash
infini-cli fs ls --table
infini-cli fs tree --search sales --files-only --table
infini-cli fs put my-folder ./sales.csv
infini-cli fs task-put t_1 ./raw.csv --subdir data
infini-cli fs config          # 这个部署的大小上限
```

大文件走会话式分片，理由只有一个：能续传。

```bash
infini-cli fs session push ./dump.csv \
    --target-type database --target-id db_1 --post-action import_database

# 断了就接着传，只补服务端缺的那些分片
infini-cli fs session push ./dump.csv --resume <uploadId>
```

会话还决定合并之后干什么——`store`、`extract_archive`、`build_rag`、`import_database`——所以「把几 GB 的 dump 导进数据源」走的是这条路，不只是搬文件。

## 浏览器

浏览器不在这套部署里，是一个通过 websocket 挂上来的 Chrome 扩展。所以这些命令是转发给你账号当前连着的那个浏览器，没连就什么也不发生。先确认：

```bash
infini-cli browser session
infini-cli browser go https://example.com --session tab-1
infini-cli browser view --session tab-1        # 带元素编号
infini-cli browser click --index 4 --session tab-1
infini-cli browser input "hello" --selector "input[name=q]" --enter
infini-cli browser exec "document.title"
```

同一个 `--session` 就是同一个标签页；navigate 之后想 click 到同一页，两边得用同一个 id。

要提醒一句：浏览器没连上时服务端回的是 HTTP 200 加 `success:false`，不是错误状态码。CLI 以 body 为准并映射成非零退出码，所以这里的退出码是可信的。

想把运行中 agent 的浏览器交出去或收回来，用 `agent browser takeover/resume/stop`，不是这一组。

## 分享与审计

一套命令服务两类读者，而且不是一回事。

不带 `--audit` 是公开读：完全不需要凭据，但只对所有者 `task share` 过的任务有效——就是分享链接看到的那个视图。带 `--audit` 是合规读：需要 proxy 超管令牌，能读任意任务（无论是否分享），并且每次读取都在服务端留下「谁读了什么」的记录。所以别把它当成绕过未分享任务的办法。

```bash
infini-cli task share t_1 --on
infini-cli task public show t_1
infini-cli task public files t_1 --table
infini-cli task public zip t_1 --out ./audit/

infini-cli task public show t_9 --audit          # 超管，留痕
```

这组的重点是 `task public evidence`。分享出去的报告用消息时间戳引用证据，这个命令把引用还原成背后真实跑过的工具调用——读者可以核对一个数字，而不是选择相信它：

```bash
infini-cli task public evidence t_1 1784807100587 1784807100999
```

单次最多 100 条。报告用到了委派工作时加 `--include-subagent`，因为子 agent 的证据在它自己的任务上。

## 配置

配置文件默认在 `~/.infini-cli/config.yaml`，按 **profile** 组织，一个二进制可以在多个部署间切换：

```yaml
current-profile: prod
profiles:
  prod:
    server: https://infini.example.com
    console: https://api.example.com/api
    token: <auth login 写入的 JWT>
  staging:
    server: https://staging.example.com
```

取值优先级：**命令行 flag > 环境变量 > 活动 profile > 内置默认值**。

`INFINI_CONFIG` 可整体改写配置文件路径（CI 里用它做隔离）。

### Profile 操作

```bash
infini-cli config profile ls --table
infini-cli config profile add staging --with-server https://staging.example.com
infini-cli config profile use staging
infini-cli --profile staging api GET /api/ai/dashboards   # 单次切换，不改默认
```

`profile add` 只创建，不切换；切换一律用 `profile use`。

### 支持的配置项

| Key | 说明 |
|---|---|
| `server` | Infini 应用后端地址，所有业务接口都在其 `/api` 下。未配置时默认 `http://127.0.0.1:$APP_PORT`，`APP_PORT` 未设则为 `8088` |
| `console` | auth/proxy 服务地址，负责签发 JWT 与用户/模型数据 |
| `api-key` | API Key 凭证 |
| `token` | `auth login` 写入的 JWT |
| `token-expires-at` | JWT 过期时间（只读展示） |
| `tenant-code` | 多租户部署的租户码，登录时使用 |
| `user-id` / `username` | 当前用户，登录后自动写入 |
| `prefer-language` | 请求头 `x-lang`，取值 `en` `zh_CN` `ar` `ja` `ko` `ru` |
| `default-output` | 默认输出格式，`json` 或 `table` |
| `update-channel` | 自更新通道 base URL，没有默认值（见「自更新」） |

## 认证说明

Infini 自己不签发凭证：应用后端只通过 `GET /api/auth/getAuthingPath` 告诉客户端 auth/proxy 服务在哪，JWT 由该 proxy 签发。`auth login` 一次完成「发现 proxy → 登录 → 校验 → 落盘」。

密码只支持交互式输入或 `--password-stdin`，不提供 `--password` flag —— 那会留在 shell 历史和进程列表里。

```bash
infini-cli auth login --username alice@example.com
echo "$PASSWORD" | infini-cli auth login --username alice --password-stdin  # CI
infini-cli auth whoami
infini-cli auth status     # 只看本地状态，不发请求
infini-cli auth logout
```

同时存在 JWT 和 API Key 时，JWT 优先；显式传入的 `--api-key` 优先于两者。

本地开发还可以直接导出 Infini 进程里的 `BUILTIN_SYSTEM_ACCESS_KEY`，CLI 会把它当作最后一档 api-key，不必 `auth login`。`INFINI_API_KEY` 和登录 JWT 都会盖过它。

## 输出与退出码

默认输出 JSON 信封，可直接 `jq`：

```json
{ "success": true, "data": {}, "message": "" }
```

列表类命令加 `--table` 转人类可读表格。进度与诊断信息一律写 stderr，不污染 stdout。

| 退出码 | 含义 |
|---|---|
| 0 | 成功 |
| 1 | 业务错误 |
| 2 | 参数错误、缺少确认 |
| 3 | 鉴权失败（含 token 过期） |
| 4 | 网络或服务不可达 |
| 5 | License 限额拦截 |

服务端错误码会被映射成可读提示，例如 1101/1105 提示重新登录，3410–3414 提示查看授权限额。

## 事件流

Agent 命令通道是异步的：`POST /api/ai/message` 只入队并立即返回，执行结果只通过 SSE 推送。所以 `dash new` 这类命令内部一律是「先订阅、再发命令、消费到终态、最后按 `GET /api/ai/state` 对账」—— 顺序不能反，命令可能在后来的订阅者接上之前就跑完，那条结果就永久丢了。

一条 SSE 连接承载该用户的全部命令（包括其他终端和浏览器标签发起的），所以 `command.state` 事件按 `clientOperationId` 过滤，不是本次运行的不处理。

`events` 是这条流的原始视图：

```bash
infini-cli events                                  # 全部事件
infini-cli events --task <taskId>                  # 只看某个任务
infini-cli events --event message.add --limit 20   # 过滤类型并自动停止
```

连接断开会自动退避重连；鉴权失败不重试。

## 危险操作

`rm` / `rollback` / `drain` / `archive` 一类操作默认需要二次确认。非交互环境（管道、CI）必须显式传 `--yes`，否则以退出码 2 终止。

## 调试

```bash
infini-cli --verbose api GET /api/ai/dashboards   # 打印请求摘要
infini-cli --trace   api GET /api/ai/dashboards   # 打印完整请求与响应
```

`--trace` 会自动脱敏 `password`、`token`、`api-key` 等字段。

## 演练与审计

```bash
infini-cli template create demo --text hello --dry-run     # 写操作只描述不发送
infini-cli --audit-log /var/log/infini-cli.jsonl dash rm d1
```

`--dry-run` 只拦写，不拦读。CLI 在写之前大多要先读一遍当前状态好把写做成 patch，查不了东西的演练没有意义；被拦下的正好是那些会改变什么的请求。被拦下的写会回一段自述，同时输出信封上多一个 `"dryRun": true`——不然被拦下的 create 仍会解进命令的结果类型，打出一条字段全空的记录，看着像真发生过。

`--audit-log` 每个请求追加一行 JSON（`{ts, method, path, status}`，被拦下的写另带 `dryRun`），文件权限 `0600`。日志在写操作发出**之前**先探一次可写性：写不进去就不发请求，因为有洞的审计比没有审计更糟，它看上去是完整的。

## 自更新

通道地址一个字都不写死。私有化部署往往在自己的内网镜像上分发，甚至没有出网路由，写死公网前缀错的时候比对的时候多：

```bash
infini-cli config set update-channel https://releases.example.com/infini-cli
infini-cli update --check     # 只看有没有新版本
infini-cli update             # 下载、校验 sha256、替换自身
```

也可以用 `INFINI_UPDATE_CHANNEL` 或 `--channel` 临时指定，三者都没有时 `update` 会报错并告诉你怎么设。

切一个通道出来：

```bash
go run ./scripts/release --version 1.4.0 --notes "..."
```

它会编出六个平台并在旁边写一份 `latest.json`，产出目录本身就是通道，挂到任意静态服务上即可。清单里的 `url` 相对通道，所以做镜像只要把目录树拷过去。`sha256` 是必填的，没有校验和的产物会被拒绝而不是盲信。

```json
{
  "version": "1.4.0",
  "releasedAt": "2026-09-11T10:16:56Z",
  "notes": "...",
  "artifacts": [
    { "os": "linux", "arch": "amd64", "url": "1.4.0/linux-amd64/infini-cli",
      "sha256": "c78f96e0...", "size": 9683106 }
  ]
}
```

替换自身是「先改名、再落位」：正在运行的可执行文件在 Windows 上不能被覆盖但可以被改名，留着旧的意味着中途失败时还能退回去。装完顺手删 `.old`，Windows 上旧映像还映射着时删不掉，留到下次更新再清。

## 给 AI Agent 用

```bash
infini-cli spec > infini-cli.md
```

输出一份完整规范：输出协议、退出码、命令清单与 flag，以及那些 agent 真正会搞错的约定——agent 的活儿是投递进队列而不是同步调用、省略资源列表和给空列表是两件事、密钥只走环境变量。命令清单是从 cobra 树里走出来的而不是手写的，所以不会和二进制对不上。

## 项目结构

```
infini-enterprise-cli/
├── main.go                      # 入口，把 Execute() 的返回值作为退出码
├── cmd/
│   ├── root.go                  # 根命令、全局 flag、退出码收口、确认门
│   ├── auth.go                  # 登录与凭证
│   ├── config.go                # 配置、profile、doctor 自检
│   ├── dash*.go                 # 看板：CRUD、spec 往返、版本布局、查询、刷新、Agent 创作
│   ├── task*.go                 # 任务：生命周期、DAG 与证据溯源、工作区文件
│   ├── db.go                    # 数据源：CRUD、连接测试、schema、上传、绑定
│   ├── rag.go                   # 知识库：CRUD、文档存储、绑定
│   ├── project.go               # 项目：CRUD、成员、文件树
│   ├── hub*.go                  # 语义层：记忆构建、五类实体、草稿与审核
│   ├── agent*.go                # Agent 直控：会话、生命周期、回滚、运行时配置
│   ├── skill.go tool.go         # 扩展面：技能与工具的 catalog 安装与本地上传
│   ├── rule.go template.go      # 扩展面：常驻规则与提示词模板
│   ├── schedule.go              # 定时任务：cron、冻结配置、运行历史
│   ├── engine.go runtime.go     # 引擎进程与 worker 舰队、lease 排查、drain
│   ├── license.go               # 授权状态与配额水位
│   ├── fs.go fs_session.go      # 文件目录、对象存储、分片续传会话
│   ├── browser.go               # 浏览器扩展自动化
│   ├── api.go                   # 任意接口直调逃生舱
│   ├── events.go                # SSE 事件流
│   ├── spec.go                  # AI Agent 规范输出，命令清单从 cobra 树生成
│   ├── update.go                # 自更新
│   ├── helpers.go               # 参数解析、JSON 载荷读取
│   ├── term.go                  # TTY 检测与无回显输入
│   └── version.go
└── internal/
    ├── agent/                   # 异步命令状态机、命令目录、SSE 流渲染、看板工具结果解析
    ├── auth/                    # proxy 登录链（md5 口令、JWT、profile）
    ├── cliexit/                 # 退出码与修复提示
    ├── client/                  # HTTP 封装、信封解包、错误分类、SSE、流式上传下载、演练与审计
    ├── config/                  # 多 profile 配置与优先级解析
    ├── dashboard/               # 看板 REST 封装、spec 模型、filter 类型转换
    ├── task/                    # 任务 REST 封装、文件树、流式下载
    ├── database/                # 数据源 REST 封装
    ├── rag/                     # 知识库 REST 封装、文档存储描述
    ├── project/                 # 项目 REST 封装
    ├── hub/                     # 语义层 REST 封装、记忆构建任务、审核请求
    ├── extension/               # 技能 / 工具 / 规则 / 模板
    ├── ops/                     # 定时任务、引擎、运行时舰队、license
    ├── storage/                 # 文件目录、对象存储、分片续传会话
    ├── browser/                 # 浏览器动作分发与拒绝识别
    ├── selfupdate/              # 通道清单、校验和、原子替换自身
    └── output/                  # JSON / 表格输出
```

`scripts/release` 是一个独立的小程序：交叉编译六个平台并写出 `latest.json`，产出目录就是一个可以直接挂出去的更新通道。
