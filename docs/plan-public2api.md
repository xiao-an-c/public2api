# public2api 实施计划

## 业务逻辑

**我们要做什么**：把现在这个「只管 WorkBuddy 一家」的账号池面板，升级成一个**多渠道路由网关**——对外仍然是同一套 OpenAI / Anthropic 兼容接口，对内可以同时托管 **WorkBuddy 国内、WorkBuddy 国际、Grok** 三路账号。调用方不需要知道自己在用哪一家，网关按渠道、健康度和额度自己挑。

**为什么要拆成「渠道」**：今天面板里已经有「国内 / 国际」的概念，但它只是一个**筛选条件**——账号混在一个池子里，靠一个域标记区分。这带来两个问题：一是没法给不同渠道配不同的策略（国内号签到、国际号不用；Grok 号要挂代理、WorkBuddy 不要）；二是没法对外区分（调用方想「只用国际号」时，只能靠模型名前缀硬凑）。把渠道升级成**一等公民**之后，账号、策略、密钥、用量、维护任务全都挂在渠道下，互不干扰。

**为什么 WorkBuddy 要拆成两个**：国内版（`codebuddy.cn`）和国际版（`workbuddy.ai`）是**两套独立的账号体系**——登录端点不同、凭据不通用、额度各自计算、运营活动也各搞各的。合成一个渠道，任何一侧的限流或改版都会污染另一侧的调度决策。

**为什么 Grok 是「一个渠道、三种账号」**：Grok 官方有三条互不相通的入口——**网页版**（grok.com，要过 Cloudflare 反爬）、**官方命令行**（Build，走设备码授权、凭据能自动续期）、**开发者控制台**（console.x.ai，粘凭据就能用但模型少）。三者登录方式和凭证形态完全不同，但对调用方来说是同一个「Grok」。所以它们归在 `grok` 这一个渠道下，作为**三种账号类型**并列，而不是三个渠道。

**做完之后长什么样**：

- **一级菜单**放通用能力：仪表盘、账号总表、API 密钥、用量、模型、日志、设置；
- **每个渠道是一个独立的维护空间**，点进去有自己的二级菜单。菜单项不是写死的，而是**由该渠道声明自己有哪些能力自动生成**——WorkBuddy 渠道会出现「签到 / 任务」，Grok 渠道不会；Grok 渠道会出现「出口 / 额度」，WorkBuddy 不会；
- **API 密钥可以锁定渠道**：签一个只走国际号的 Key 给某个调用方，它就不可能用到国内号。

---

以下为实现计划（给要动手的人）。

> **文档定位**：这是一份**实施计划**，不是逆向分析。它回答「从今天的代码到目标架构，中间要做哪几步、每步动哪些文件、风险在哪」。
> **路径口径**：本页出现的 `路径:行号` 分属四个仓库，引用时一律带仓库前缀标识——
> - `[panel]` → `~/wx/demo/workbuddy2api-panel`（**底子**）
> - `[codex]` → `~/wx/demo/codex2api`（**要照抄的模式**）
> - `[grok]` → `~/wx/demo/grok2api/backend`（**要移植的能力**）。
>   注意：该仓库的 Go 后端在 `backend/` 子目录下，**本页所有 `[grok]` 引用都已省去 `backend/` 前缀**；
>   前端不在其内（在 `grok2api/frontend`），本页提到时会写全路径。
> - `[vault]` → 本仓库（`docs/`）
>
> 只有 `[vault]` 的引用能在本机直接点开，其余三个是相邻仓库的相对路径。
> 相关分析页：[[workbuddy2api-panel/index|workbuddy2api-panel]]、[[codex2api/index|codex2api]]、[[grok2api-chenyme/index|grok2api-chenyme]]、[[grok2api-jiujiu532/index|grok2api-jiujiu532]]。
> 写作契约：[[写作规范]]（本页是计划文档，不适用其「两段式」硬要求，但**每条结论仍然带依据**）。
>
> **本页是真源。** 计划跟代码一起演进——§12 的未决项被消费时**就地标注**，
> 实施中发现的与计划的偏差记在 [`docs/adr/`](./adr/)。
> 笔记库里有一份同名副本，那份是**规划快照**，不再更新。

## 0. 结论先行：三句话

1. **这是一次架构升级，不是加一个适配器**。底子实测 21,259 行，Grok 要移植的部分实测 **29,587 行**（76 个文件），两者相加已近 5.1 万行，再加新写的渠道/存储/维护层与面板重写，**目标体量约 5.5 万行**。**没有捷径**——除非接受旁挂方案（见 §11.1）。
2. **最大的风险不是 Grok 的协议，是它的出口治理**。Grok 网页版要求 TLS 指纹伪装 + 过 Cloudflare + 一个外部签名服务，这套东西（3,360 行）在底子里**完全没有对应物**，必须新引入。
3. **最容易被低估的是面板**。底子的面板（`internal/panel/app.js` 1,648 行 + `index.html` 941 行）是**单渠道硬编码**的 WorkBuddy 运营台，改造成「渠道 + 数据驱动二级菜单」等于重写前端信息架构。

## 1. 三个来源仓库的实测事实

### 1.1 底子：`workbuddy2api-panel`

| 项 | 事实 | 依据 |
| --- | --- | --- |
| 规模 | 72 个非测试 Go 文件 / **21,259 行** | 本次统计 |
| 语言 | Go + 原生 `net/http`（无框架）+ 单文件前端 | `[panel] internal/server/handler.go:125` |
| 对外端点 | **只有 3 个**：`POST /v1/chat/completions`、`GET /v1/models`、`GET /status` | `[panel] internal/server/handler.go:125-128` |
| 面板端点 | `/panel/api/*` 共 **35 条**：运营类 18（签到/任务/上学/旅游/活动/保活）、账号类 8、通用 9 | `[panel] internal/panel/panel.go:148-182` |
| 存储 | **JSON 文件**：`auths/` 一账号一文件 + `data/state.json` | `[panel] internal/pool/persist.go` |
| 鉴权 | **单一 `api_key`**，常量时间比较；**没有 Key 体系** | `[panel] internal/httpauth/httpauth.go:25` |
| 渠道概念 | **已有 `realm`（cn/global）**，但只是池内过滤器 | `[panel] internal/pool/realm.go:11-53` |
| realm 计算 | 显式 `realm` 字段优先，否则按 `domain` 后缀 `.workbuddy.ai` 推断 | `[panel] internal/auth/auth.go:122-130` |
| realm 路由 | 模型名前缀 `[cn\|global:]model`，只认这两个前缀 | `[panel] internal/server/resolve_model.go:15-22` |
| 配置 | **扁平单渠道**：`global.*` 一节、`schedule.*` 一节 | `[panel] config.example.json` |

**关键判断**：`realm` 已经做对了 80% 的事——有归一化、有推断、有逃生门（`global.enabled=false` 锁死纯 CN，`[panel] internal/auth/auth.go:94-106`）、有池级过滤（`AvailableUIDsForRealm`）。**升级成渠道主要是「提升抽象层级 + 换存储 + 拆前端」，不是从零发明。**

### 1.2 要照抄的模式：`codex2api` 的渠道模型

codex2api 把渠道做成了贯穿全栈的一等公民，有**五个挂载点**，这是我们要对齐的完整清单：

| # | 挂载点 | codex2api 实现 | 依据 |
| --- | --- | --- | --- |
| 1 | **渠道枚举（有序）** | `AllUpstreamChannels = ["codex","claude","antigravity","grok"]`，顺序即展示顺序 | `[codex] database/visible_channels_settings.go:16` |
| 2 | **账号页按渠道分 Tab** | 路由 `/accounts/{grok,antigravity,claude,invite}` | `[codex] frontend/src/pages/Accounts.tsx:1756-1761` |
| 3 | **渠道可见性开关** | 管理台可勾选显示哪些渠道；兜底渠道恒在列，避免关成空白 | `[codex] database/visible_channels_settings.go:19-49` |
| 4 | **API Key 渠道作用域** | Key 上带 `upstream_channel`，`""`/auto = 不限 | `[codex] database/postgres.go:1842-1875` |
| 5 | **渠道级批量操作** | 代理自动绑定 `autoBalanceProxies({channel})` | `[codex] frontend/src/api.ts:1638` |

> [!TIP] 最值得抄的是第 4 条
> 「Key 能锁渠道」这一个字段，就把「渠道」从**运维概念**变成了**产品能力**——你可以给不同调用方签不同渠道的 Key，而不用维护两套网关。这是 codex2api 渠道设计里性价比最高的一处，底子目前完全没有（只有单一 api_key）。
>
> 注意 `ResolveUpstreamChannel` 的兜底语义：**未知值一律视为不限**（`[codex] database/postgres.go:1873-1880`），而不是报错。这样旧配置不会因为加了新渠道而失效。

### 1.3 要移植的能力：`grok2api-chenyme`

| 项 | 事实 | 依据 |
| --- | --- | --- |
| 规模 | 248 个非测试 Go 文件 / **94,527 行** | 本次统计 |
| 渠道枚举 | `Provider = grok_build \| grok_web \| grok_console` | `[grok] internal/domain/account/account.go:11-16` |
| 渠道能力描述 | `Definition{Provider, ModelNamespace, ModelCatalog, Quota, Credential, Conversation, Media, Inference}` | `[grok] internal/infra/provider/definition.go:86-96` |
| 登录方式 | 三种完全不同：Build 走 RFC 8628 设备码；Web 粘 SSO cookie；Console 粘凭据 | `[grok] internal/infra/provider/cli/oauth.go`（348 行） |
| 出口治理 | TLS 指纹伪装 + WARP/socks + FlareSolverr + 出口租约与探测 | `[grok] internal/infra/egress/`（8 文件 / 3,360 行） |
| 外部依赖 | **一个第三方签名服务**（算 Cloudflare 防爬标记），默认 `https://grok.wodf.de/sign`，带 70 字节硬校验 | `[grok] internal/infra/provider/web/statsig.go:409-417` |

**要移植的部分（按目录实测）**：

| 目录 | 文件 | 行数 | 移植难度 |
| --- | --- | --- | --- |
| `[grok] internal/infra/provider/web` | 18 | 9,474 | 🔴 最难（私有 WebSocket 协议 + Cloudflare + statsig） |
| `[grok] internal/infra/provider/cli` | 26 | 9,228 | 🟡 中（设备码 OAuth + 原生 Responses） |
| `[grok] internal/infra/provider/console` | 12 | 3,442 | 🟢 易（原生 Responses，粘凭据即用） |
| `[grok] internal/infra/provider/conversation` | 11 | 4,017 | 🟡 中（三渠道共用的协议转换） |
| `[grok] internal/infra/provider/searchresult` | 1 | 66 | 🟢 易（信源消毒） |
| `[grok] internal/infra/egress` | 8 | 3,360 | 🔴 最难（底子零对应物） |
| **小计** | **76** | **29,587** | |

**明确**不**移植的部分**（底子已有等价物，或应重写）：

| grok2api 目录 | 行数 | 为什么不要 |
| --- | --- | --- |
| `[grok] internal/application/`（gateway/account/model/quota） | 26,524 | 选号/重试/冷却 → 用底子自己的 `internal/pool`（已有三因子加权 + 会话粘性 + 两级冷却） |
| `[grok] internal/infra/persistence/` | 10,973 | GORM + Postgres/sqlite 双驱动 → 换成 public2api 自己的 SQLite 层 |
| `[grok] internal/transport/http/` | 11,682 | Gin 路由 → 用底子的 `net/http` mux |
| `grok2api/frontend/`（不在 `backend/` 内） | — | React 管理台 → 用底子的面板 |
| `[grok] internal/domain/`（其余） | ~3,000 | 只取 `account` 的凭证模型，其余按需 |

> [!WARNING] 为什么不能整体搬 grok2api
> 一是**它自己的网关层（application + transport + persistence ≈ 49,000 行）和底子高度重复**，搬进来等于两套调度器打架；
> 二是**它的前端是 React，底子是单文件 JS**，两套管理台无法共存；
> 三是**它的 `Definition` 抽象比我们需要的更细**（区分 ModelCatalog/Quota/Media/Inference 四套 surface），照搬会把底子的简单模型撑爆。
> 正确做法是**只搬「上游适配器」这一层**，把它接到 public2api 自己的渠道接口上。

## 2. 目标架构

### 2.1 两级模型：Channel × AccountKind

```
Channel（渠道，用户可见的维护空间，3 个）
├── wbp-cn        WorkBuddy 国内      kinds: [wbp]
├── wbp-global    WorkBuddy 国际      kinds: [wbp]
└── grok          Grok                kinds: [grok_web, grok_build, grok_console]
```

- **`Channel`** 是**一级维度**：账号、策略、密钥作用域、用量统计、维护任务全部挂它。
- **`AccountKind`** 是**渠道内的凭证/上游形态**：决定登录方式、凭证结构、上游协议。
- WorkBuddy 两个渠道的 kind 相同（都是同一套 OAuth 凭据，只是端点不同）——**差异全部落在渠道配置里**，不落到代码分支。
- Grok 一个渠道三种 kind——**差异落在 kind 上**，因为登录方式和凭证结构确实不同。

这个划分直接对应源码：Grok 的 `Provider` 枚举（`[grok] internal/domain/account/account.go:11-16`）原样成为 `grok` 渠道的 kind 集合。

### 2.2 能力声明（Capability）→ 二级菜单数据驱动

这是「渠道 = 独立维护空间」这条要求的落地方式。**菜单不写死，由渠道声明自己有什么能力生成**：

```go
type Capability string

const (
    // 账号接入
    CapOAuthLogin  Capability = "account.oauth"    // 走授权流程（WorkBuddy、Grok Build）
    CapCookieImport Capability = "account.cookie"  // 粘贴 cookie / 凭据（Grok Web/Console）
    CapRefresh     Capability = "account.refresh"  // 凭据自动续期
    CapQuotaQuery  Capability = "quota.query"      // 查上游额度
    // 维护运营（决定二级菜单有没有这一项）
    CapKeepalive   Capability = "ops.keepalive"    // 保活
    CapCheckin     Capability = "ops.checkin"      // 签到
    CapTasks       Capability = "ops.tasks"        // 任务（上学/旅游/活动）
    CapEgress      Capability = "ops.egress"       // 出口管理（Grok 特有）
    // 业务面
    CapMedia       Capability = "media"            // 图片/视频生成
    CapSearch      Capability = "search"           // 联网搜索
)

type Channel struct {
    ID           string        // "wbp-cn"
    Name         string        // "WorkBuddy 国内"
    UpstreamHost string        // "codebuddy.cn"
    Kinds        []AccountKind
    Capabilities []Capability  // ★ 驱动二级菜单
}
```

渠道能力矩阵（**这就是二级菜单的来源**）：

| 能力 → 菜单项 | wbp-cn | wbp-global | grok |
| --- | --- | --- | --- |
| 账号（OAuth 登录） | ✅ | ✅ | — |
| 账号（粘贴凭据） | — | — | ✅ web / console |
| 账号（设备码授权） | — | — | ✅ build |
| 保活（凭据续期） | ✅ | ✅ | ✅ |
| 签到 | ✅ | ✅ | — |
| 任务（上学/旅游/活动） | ✅ | ✅ | — |
| 额度 | ✅ | ✅ | ✅ |
| 出口管理 | — | — | ✅ |
| 联网搜索 | — | — | ✅ |
| 图片 / 视频 | ✅ | ✅ | ✅ |

> [!TIP] 这套声明的额外好处
> 二级菜单只是它的**第一个消费者**。同一个 `Capabilities` 还能驱动：
> ① 账号导入页显示哪些导入方式；② 定时任务调度器只跑该渠道声明支持的任务；③ 前端不显示无意义按钮（直接解决「Grok 账号页出现签到按钮」的问题）；④ 后端启动时校验「渠道声明的能力都有实现」，避免接口和实现悄悄漂移（照抄 `[grok] internal/infra/provider/definition.go:121` 的 `Validate()` 思路）。

### 2.3 目录结构

```
public2api/
├── cmd/public2api/main.go
├── internal/
│   ├── channel/                 # ★ 新增：渠道注册表与能力声明
│   │   ├── channel.go           #   Channel / AccountKind / Capability 定义
│   │   ├── registry.go          #   静态注册 + Validate()
│   │   ├── wbp.go               #   wbp-cn / wbp-global 两个渠道的描述
│   │   └── grok.go              #   grok 渠道（三种 kind）
│   ├── account/                 # ★ 新增：跨渠道统一账号模型（替代 [panel] internal/auth）
│   │   ├── account.go           #   Account{ID, Channel, Kind, Credentials, Quota, Health}
│   │   └── crypto.go            #   AES-256-GCM 凭据加密（移植 [grok] internal/infra/security/cipher.go）
│   ├── pool/                    # 从 [panel] 平移，realm 过滤改为 channel 过滤
│   ├── adapter/                 # ★ 新增：上游适配器（每个 kind 一个实现）
│   │   ├── adapter.go           #   Adapter 接口（对话/额度/刷新/维护）
│   │   ├── wbp/                 #   从 [panel] internal/upstream 平移
│   │   ├── grokweb/             #   移植 [grok] internal/infra/provider/web
│   │   ├── grokbuild/           #   移植 [grok] internal/infra/provider/cli
│   │   ├── grokconsole/         #   移植 [grok] internal/infra/provider/console
│   │   └── conversation/        #   移植 [grok] internal/infra/provider/conversation
│   ├── egress/                  # ★ 新增：出口治理（移植 [grok] internal/infra/egress）
│   ├── maintenance/             # ★ 新增：渠道维护任务（保活/签到/任务）
│   │   ├── scheduler.go         #   通用调度（从 [panel] internal/scheduler 抽）
│   │   ├── wbp_checkin.go       #   WorkBuddy 专属
│   │   └── wbp_tasks.go         #   上学/旅游/活动
│   ├── store/                   # ★ 新增：SQLite 层（替代 [panel] internal/pool/persist.go）
│   │   ├── schema.go            #   AutoMigrate
│   │   ├── accounts.go
│   │   ├── apikeys.go           #   含渠道作用域（照抄 codex2api）
│   │   └── importexport.go      #   JSON 导入导出
│   ├── server/                  # 从 [panel] 平移，加渠道路由
│   ├── panel/                   # 重写信息架构（见 §7）
│   └── usage/                   # 从 [panel] 平移，加渠道维度
└── ...
```

## 3. 渠道差异矩阵

这张表决定了每个渠道要实现哪些接口。**空白的格子就是不用写的代码**。

| 维度 | wbp-cn | wbp-global | grok-web | grok-build | grok-console |
| --- | --- | --- | --- | --- | --- |
| 上游主机 | `codebuddy.cn` | `workbuddy.ai` | `grok.com` | Build 服务 | `console.x.ai` |
| 登录方式 | 插件 OAuth 三端点 | 同左（端点不同） | 粘 SSO cookie | RFC 8628 设备码 | 粘凭据 |
| 凭据续期 | ✅ refresh token | ✅ | ✅（含 SSO→Build 转换） | ✅ refresh token | ⚠️ 无 |
| 上游协议 | 私有 REST | 私有 REST | **私有 WebSocket** | 原生 Responses | 原生 Responses |
| 反爬 | 无 | 无 | **Cloudflare + statsig** | 无 | 无 |
| TLS 指纹 | 无 | 无 | **必须** | 可选 | 可选 |
| 联网搜索 | ❌ | ❌ | ✅ | ✅ | ✅ |
| 图片/视频 | ✅ | ✅ | ✅ | ✅ | ✅ |
| 额度查询 | 积分 | 积分 | 上游 rate-limits | 同 web | 本地自管 |
| 维护任务 | 签到/上学/旅游 | 签到/上学/旅游 | — | — | — |

> [!WARNING] `grok-console` 的凭据不续期
> 它粘的是一段静态凭据，没有 refresh 机制（[[grok2api-chenyme/01-login|上游分析]] §3）。这意味着**它一定会过期，且只能人工重粘**。计划里必须为它设计一个「失效告警 + 一键重粘」的流程，否则运维会周期性踩坑。

## 4. 数据模型（SQLite）

从 JSON 文件迁到 SQLite，**保留 JSON 导入导出**作为迁移与备份通道。

```sql
-- 渠道（静态注册为主，落库只为存渠道级配置与开关）
CREATE TABLE channels (
  id TEXT PRIMARY KEY,              -- wbp-cn | wbp-global | grok
  name TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  sort INTEGER NOT NULL DEFAULT 0,
  config JSON                        -- 渠道级配置（端点、超时、策略）
);

-- 账号（跨渠道统一表）
CREATE TABLE accounts (
  id INTEGER PRIMARY KEY,
  channel TEXT NOT NULL,             -- 渠道
  kind TEXT NOT NULL,                -- 账号类型
  uid TEXT NOT NULL,                 -- 渠道内稳定标识
  source_key TEXT NOT NULL,          -- 去重键（如 sha256(credential)）
  name TEXT, email TEXT,
  status TEXT NOT NULL,              -- active | cooling | disabled | invalid
  credentials BLOB,                  -- AES-256-GCM 加密的凭据 JSON
  quota JSON,                        -- 额度快照
  health JSON,                       -- 健康分/连败/降权
  created_at INTEGER, updated_at INTEGER,
  UNIQUE(channel, kind, source_key)
);
CREATE INDEX idx_accounts_pick ON accounts(channel, kind, status);

-- 渠道维护任务（保活/签到/任务），由渠道能力声明决定有哪些
CREATE TABLE maintenance_tasks (
  id INTEGER PRIMARY KEY,
  channel TEXT NOT NULL,
  kind TEXT NOT NULL,                -- keepalive | checkin | tasks
  name TEXT NOT NULL,
  cron TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  config JSON
);

-- 任务执行记录（面板「任务」二级菜单的数据源）
CREATE TABLE maintenance_runs (
  id INTEGER PRIMARY KEY,
  task_id INTEGER NOT NULL,
  account_id INTEGER,
  started_at INTEGER, finished_at INTEGER,
  ok INTEGER, detail JSON
);

-- API Key（照抄 codex2api 的渠道作用域）
CREATE TABLE api_keys (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  key_hash TEXT NOT NULL UNIQUE,
  channel TEXT NOT NULL DEFAULT '',  -- '' = auto 不限
  cost_limit REAL, token_limit INTEGER, rpm_limit INTEGER,
  expires_at INTEGER, enabled INTEGER NOT NULL DEFAULT 1
);

-- 请求账本（按渠道维度统计的基础）
CREATE TABLE request_logs (
  id INTEGER PRIMARY KEY,
  ts INTEGER NOT NULL,
  channel TEXT NOT NULL, account_id INTEGER, api_key_id INTEGER,
  model TEXT, prompt_tokens INTEGER, completion_tokens INTEGER,
  status TEXT, latency_ms INTEGER, error_code TEXT
);
CREATE INDEX idx_logs_ts_channel ON request_logs(ts, channel);

-- 冷却（账号级 + 账号×模型级，照抄底子已有的两级语义）
CREATE TABLE cooldowns (
  channel TEXT NOT NULL, account_id INTEGER NOT NULL,
  model TEXT NOT NULL DEFAULT '',    -- '' = 账号级
  reason TEXT, until_ts INTEGER NOT NULL,
  PRIMARY KEY (channel, account_id, model)
);

-- 出口（Grok 需要；做成通用表，其它渠道不用）
CREATE TABLE egress_nodes (
  id INTEGER PRIMARY KEY,
  channel TEXT NOT NULL,
  kind TEXT NOT NULL,                -- warp | socks5 | flaresolverr | direct
  addr TEXT, status TEXT,
  last_ok_at INTEGER, config JSON
);

CREATE TABLE settings (key TEXT PRIMARY KEY, value JSON);
```

**迁移映射**（JSON → SQLite，逐字段对照）：

| 现位置 | 新位置 | 说明 |
| --- | --- | --- |
| `[panel] auths/<uid>.json` | `accounts.credentials` | **AES-256-GCM 加密**（现在是明文，见 §11.3） |
| `[panel] data/state.json` 的池状态 | `accounts.health` + `cooldowns` | 冷却改为独立表，支持账号×模型级 |
| `auth.realm` 字段 | `accounts.channel` | `cn` → `wbp-cn`，`global` → `wbp-global` |
| `config.json` 的 `global.*` | `channels.config`（wbp-global 行） | 域配置提升为渠道配置 |
| `config.json` 的 `schedule.*` | `maintenance_tasks` | 五个开关变成五行任务 |
| `config.json` 的 `api_key` | `api_keys` 一行 | 单 Key 升级为 Key 表，渠道作用域 `''` |

## 5. 通用层：从底子抽什么、改什么

| 包 | 处置 | 改动要点 |
| --- | --- | --- |
| `[panel] internal/pool/` | **平移 + 改维度** | `AvailableUIDsForRealm(realm)` → `AvailableUIDsForChannel(channel)`；`realm.go` 的 53 行几乎是照抄，只换过滤字段 |
| `[panel] internal/auth/` | **拆解** | 凭证部分进 `internal/account/`，realm 计算进 `internal/channel/`；378 行里约一半是 realm 逻辑 |
| `[panel] internal/upstream/` | **变成 adapter/wbp/** | 27 文件 8,146 行，WorkBuddy 专属，整体平移；但要抽出「上游客户端」的公共接口 |
| `[panel] internal/scheduler/` | **抽出通用调度** | 1,112 行里签到/上学/旅游/黑猫是 WorkBuddy 专属 → 进 `maintenance/wbp_*.go`；定时器与并发控制留下 |
| `[panel] internal/server/` | **平移 + 加渠道** | 3 个端点 → 增加 `GET /v1/channels`；`resolve_model.go` 的前缀解析要从「realm」扩到「渠道」 |
| `[panel] internal/panel/` | **重写 IA** | 见 §7 |
| `[panel] internal/httpauth/` | **替换** | 40 行的单 Key 比较 → 完整的 Key 表 + 渠道作用域 + 限额 |
| `[panel] internal/usage/` | **平移 + 加维度** | 560 行，加 `channel` 维度 |
| `[panel] internal/redisstore/` | **可选保留** | 多实例场景；单机 SQLite 下不需要 |

**唯一需要重新设计的公共接口**是上游适配器。底子的 `internal/upstream` 是「一个上游」写死的，需要抽出：

```go
// internal/adapter/adapter.go
type Adapter interface {
    Kind() AccountKind
    // 对话（流式）
    Chat(ctx context.Context, acct *account.Account, req *ChatRequest) (Stream, error)
    // 额度
    Quota(ctx context.Context, acct *account.Account) (*Quota, error)
    // 凭据续期（不支持则返回 ErrNotSupported）
    Refresh(ctx context.Context, acct *account.Account) error
    // 模型目录
    Models(ctx context.Context, acct *account.Account) ([]Model, error)
}
```

`ErrNotSupported` 的存在是必要的——`grok-console` 不续期、WorkBuddy 不联网搜索，接口必须允许「这个能力我没有」，而不是逼每个适配器写空实现。

## 6. Grok 接入方案

### 6.1 移植顺序：按风险递增，而不是按功能重要性

| 阶段 | 移植什么 | 行数 | 为什么这个顺序 |
| --- | --- | --- | --- |
| **G1** | `[grok] provider/console` + `conversation` | ~7,500 | 上游就是原生 Responses 协议，**粘凭据即用**；能最快跑通「Grok 渠道」的端到端骨架（含账号页、选号、协议转换） |
| **G2** | `[grok] provider/cli`（Build） | ~9,200 | 登录难（设备码）但**协议不难**（原生 Responses）；此时骨架已被 G1 验证 |
| **G3** | `[grok] internal/infra/egress` | 3,360 | 出口治理独立于业务，可单独测试（连不连得上 grok.com） |
| **G4** | `[grok] provider/web` | ~9,500 | 最难：私有 WebSocket + Cloudflare + statsig + TLS 指纹，**且前三阶段已把除出口外的所有基础设施备好** |

> [!IMPORTANT] 为什么 console 排第一
> 它是**唯一一条「不需要出口治理、不需要反爬、不需要设备码」的链路**。用它把「Grok 渠道」这个新概念在系统里跑通（渠道注册、账号表、adapter 接口、面板二级菜单、Key 作用域），风险最低、反馈最快。
> 如果先做 web，会在「渠道抽象对不对」还没验证的时候，就同时背上 Cloudflare + WebSocket 两个难题。

### 6.2 移植时的接口改造清单

移植不是复制粘贴，每个目录都要做「去掉 grok2api 自己的网关假设」的改造：

| grok2api 原样 | 改造为 |
| --- | --- |
| 直接读 `internal/infra/config`（Gin + viper 风格） | 读 public2api 的渠道配置 |
| 直接写 `internal/infra/persistence`（GORM） | 通过 `internal/store` 读写 |
| 自己选号（`internal/application/gateway`） | **删掉**，由 public2api 的 `internal/pool` 选号 |
| 自己的重试与冷却 | **删掉**，用 public2api 的冷却表 |
| `Definition` 四套 surface | 收敛到 public2api 的 `Channel.Capabilities` |
| 自己的错误码体系 | 映射到 public2api 的错误分类（保留上游码表） |

**保留不动的部分**（这些是真正的逆向成果，价值最高）：
- 三条链路的**上游协议实现**（请求构造、SSE/WebSocket 解析、字段映射）
- **凭据获取与续期**的端点与参数
- **statsig 签名**的调用协议与 70 字节校验
- **信源归一化**（`searchresult`，66 行，直接搬）

### 6.3 出口治理的三种选择

这是全计划**最需要提前决策**的一处。Grok 网页版要求请求看起来像真实浏览器，底子完全没有这套东西。

| 方案 | 做法 | 代价 |
| --- | --- | --- |
| **A. 完整移植** | 搬 `[grok] internal/infra/egress` + `statsig`，自己管 WARP/socks/FlareSolverr | 3,360 + 488 行；要跑 FlareSolverr 容器；运维复杂度上一个台阶 |
| **B. 只移植客户端** | 保留 TLS 指纹与 statsig 调用，出口复用底子已有的 `passthrough_ip` 思路，代理外部提供 | 省掉出口调度，但要人工维护代理池 |
| **C. 先不做 web** | G1+G2 先上线（console + build），web 留待后续 | 少一条链路，但**能立刻交付**；且 build 本身就能用前沿模型 |

**建议：C → B → A 逐步推进**，并在计划评审时确认。理由：`grok-web` 是三条里唯一需要外部服务（签名服务 + FlareSolverr）的，而 `grok-build` 已经能覆盖大部分模型需求。

## 7. 面板信息架构（一级菜单 + 渠道维护空间）

按「渠道是独立维护空间、通用功能留一级」重排：

```
┌─ 一级菜单（通用，与渠道无关）─────────────────┐
│  📊 仪表盘        各渠道健康总览、在途、今日用量  │
│  👥 账号总表      跨渠道总表，可按渠道/类型筛选    │
│  🔑 API 密钥      含「渠道作用域」列              │
│  📈 用量          渠道 × 账号 × Key × 模型       │
│  🧠 模型          各渠道模型目录与别名            │
│  📜 请求日志      可按渠道过滤                    │
│  ⚙️ 设置          数据库、面板鉴权、全局策略       │
└──────────────────────────────────────────────┘

┌─ 渠道维护空间（每个渠道一套，菜单由 Capability 生成）─┐
│  ▸ WorkBuddy 国内                                  │
│      ├─ 账号        OAuth 授权登录 / 导入 / 状态     │
│      ├─ 保活        定时 keepalive，周期可配         │
│      ├─ 签到        定时 checkin + 手动一键全签      │
│      └─ 任务        上学 / 旅游 / 活动（含队列）      │
│                                                    │
│  ▸ WorkBuddy 国际                                  │
│      └─ 同上（账号体系独立，配置独立）                │
│                                                    │
│  ▸ Grok                                            │
│      ├─ 账号        三个子页：Web / Build / Console  │
│      ├─ 保活        refresh token 续期 + 失效告警    │
│      ├─ 额度        上游 quota / billing 快照        │
│      └─ 出口        代理节点、clearance、签名服务     │
└────────────────────────────────────────────────────┘
```

**实现方式**：侧栏不再写死，而是
1. 取渠道注册表 → 渲染「渠道维护空间」分组；
2. 每个渠道取自己的 `Capabilities` → 渲染二级菜单项；
3. 前端只认能力名，不认渠道名——**新增渠道时前端零改动**。

**现有面板的改造量**：`[panel] internal/panel/app.js`（1,648 行）目前是「一个 HTML + 一个 JS，所有视图靠 show/hide 切换」。改造路径有两条：

| 方案 | 做法 | 评价 |
| --- | --- | --- |
| **原地改造** | 保留单文件 JS，加一层「渠道上下文 + 能力过滤」 | 改动小，但 1,648 行会继续膨胀，二级菜单会让它更难维护 |
| **拆分模块**（建议） | 拆成 `shell.js`（框架/路由/鉴权）+ 每渠道一个视图模块 | 一次性成本，但新增渠道只需加一个模块文件 |

## 8. 对外 API 设计

**保持不变**（调用方无感）：

| 端点 | 说明 |
| --- | --- |
| `POST /v1/chat/completions` | 主力端点，行为不变 |
| `GET /v1/models` | 改为**按渠道聚合**：不同渠道的同名模型加渠道前缀区分 |
| `GET /status` | 增加渠道维度 |

**新增**：

| 端点 | 用途 |
| --- | --- |
| `GET /v1/channels` | 列出可用渠道及其模型（供调用方发现） |
| `POST /v1/responses` | 补上 Responses 协议（Grok 原生就是它，直通更省转换） |
| `POST /v1/messages` | 补上 Anthropic 协议（可选，看调用方需要） |

**模型名路由**：现在是 `[cn|global:]model` 两段式前缀（`[panel] internal/server/resolve_model.go:15-22`）。**必须扩展但不能破坏兼容**：

```
现有：  cn:gpt-4        →  wbp-cn 渠道
        global:gpt-4    →  wbp-global 渠道
        gpt-4           →  默认渠道（cn）

目标：  wbp-cn:model    →  wbp-cn        ← 新，与旧名共存
        wbp-global:model→  wbp-global
        cn:model        →  wbp-cn        ← 旧别名，永久保留
        global:model    →  wbp-global
        grok:model      →  grok
        model           →  按 Key 的渠道作用域，无作用域则按渠道优先级
```

> [!WARNING] 前缀解析必须保持「未知前缀不算前缀」
> 现有实现的行为是：**只有前段恰好是 `cn`/`global` 才剥离，否则整个字符串当裸模型名**（`[panel] internal/server/resolve_model.go:18-21`）。这个保守策略必须保留——否则形如 `gpt-4:latest` 这类带冒号的模型名会被错误切分。加渠道前缀时照抄这个判断结构。

## 9. 迁移方案（JSON → SQLite，零丢号）

**原则：旧数据必须能一键迁入，且迁入后旧文件不动（可回滚）。**

| 步骤 | 动作 | 验收 |
| --- | --- | --- |
| 1 | 首次启动检测到 `auths/` 且 `accounts` 表为空 → 进入迁移 | 迁移前自动备份 `auths/` 与 `state.json` 到 `backup-<时间戳>/` |
| 2 | 逐账号读 JSON，`realm` → `channel`（`cn`→`wbp-cn`，`global`→`wbp-global`） | 账号总数一致 |
| 3 | 凭据**加密后**写入 `credentials`（AES-256-GCM） | 能解密回原文，逐账号比对 |
| 4 | `state.json` 的冷却/健康写入 `health` 与 `cooldowns` | 冷却中的账号迁移后仍在冷却 |
| 5 | `config.json` 的 `schedule.*` 生成五行 `maintenance_tasks` | 定时任务周期与旧配置一致 |
| 6 | 写迁移标记，旧文件保留 | 重启不重复迁移 |

**反向导出**（面板「导出」按钮）：把 `accounts` 导出成旧格式 JSON，保证任何时候都能退回旧版本运行。

**迁移的三个必测项**：
1. **空库 + 有旧数据** → 全量迁入；
2. **非空库 + 有旧数据** → 不重复迁入，且不覆盖新数据；
3. **迁移中途断电** → 事务回滚，下次启动重新迁（不能留下半截数据）。

## 10. 分阶段里程碑

| 里程碑 | 内容 | 交付物 | 预估工作量 |
| --- | --- | --- | --- |
| **M0 骨架** | 建仓库、`channel` 注册表、`store` SQLite 层、迁移工具 | 能把旧 JSON 迁进 SQLite 并读出来 | 中 |
| **M1 双渠道化** | 现有 WorkBuddy 功能跑在 `wbp-cn`/`wbp-global` 两个渠道上 | **功能零变化**，但内部已是渠道模型 | 中 |
| **M2 面板 IA** | 一级菜单 + 渠道维护空间（能力驱动） | 两个 WorkBuddy 渠道各有独立二级菜单 | 中大 |
| **M3 Key 体系** | `api_keys` 表 + 渠道作用域 + 限额 | 能签「只走国际号」的 Key | 中 |
| **M4 Grok 渠道骨架 + console** | 渠道注册 + adapter 接口 + console 链路 | 能用 Grok 对话，账号页有 Grok Tab | 中大 |
| **M5 Grok Build** | 设备码 OAuth + 续期 + 失效告警 | 凭据能自动续期 | 大 |
| **M6 Grok 出口 + Web** | egress + statsig + WebSocket 链路 | 网页版可用（**依赖 §6.3 决策**） | 很大 |
| **M7 用量与审计统一** | 渠道维度的用量/计费/日志 | 四维统计 | 中 |

**建议的验收节奏**：M1 结束时**必须做一次回归**——对外行为与今天完全一致（同样的请求、同样的响应、同样的 `/v1/models`）。这一步是纯重构，**如果行为变了就是重构做错了**，不要带着偏差往下走。

## 11. 风险清单

### 11.1 风险一：工作量被低估（**最高**）

底子 21,259 行，要移植约 29,587 行，加上新写的通用层与面板，**目标体量 ~55,000 行**。这不是「加个适配器」的量级。

**缓解**：接受 §6.3 的方案 C——先只做 console + build，把 web 与 egress 剥离出首个版本。这样首个可用版本的移植量从 **29,587 行降到约 16,800 行**（console 3,442 + build 9,228 + conversation 4,017 + searchresult 66）。

### 11.2 风险二：出口治理引入外部依赖

`grok-web` 依赖一个**第三方签名服务**（默认 `https://grok.wodf.de/sign`）与 FlareSolverr 容器（`[grok] internal/infra/provider/web/statsig.go:26`）。

**缓解**：① 把签名服务做成可配置 + 可自建；② 明确「该服务不可用时 grok-web 整体不可用」是**预期行为**，要有明确告警而不是静默失败；③ 优先交付不依赖它的 build/console。

### 11.3 风险三：凭据加密不能等到最后做

底子现在是**明文 JSON 落盘**（`[panel] internal/pool/persist.go`）。迁到 SQLite 时如果还留明文，等于把「多渠道凭据集中存放」的风险放大了三倍。

**缓解**：**M0 就把 AES-256-GCM 加密做进去**（可直接移植 `[grok] internal/infra/security/cipher.go:18-46`）。注意它的密钥约束：**首次写入账号后不可更换**（密文无密钥版本号），迁移前就要定好密钥的存放与备份方式。

### 11.4 风险四：上游改版

三条 Grok 链路全部是**逆向私有协议**，上游改版即失效。WorkBuddy 侧同样。

**缓解**：① 渠道适配器必须**可独立发版**（不因一个渠道改版而整体重启失败）；② 保留「上游协议探测」能力（底子已有 `[panel] internal/panel/model_probes_test.go` 的思路）；③ 每个渠道要有独立的健康探针与告警。

### 11.5 风险五：模型名冲突

三个渠道可能有同名模型（如都叫 `grok-4`）。

**缓解**：`GET /v1/models` 一律返回**带渠道前缀**的名字；裸名只作为「默认渠道」的便捷写法保留，并在文档里明确其解析规则。

## 12. 未决项（需要你拍板）

| # | 问题 | 选项 | 建议 |
| --- | --- | --- | --- |
| 1 | **出口治理做到哪一步**（§6.3） | A 完整移植 / B 只移植客户端 / C 先不做 web | **C → B → A**，先交付 console + build |
| 2 | **面板前端**：原地改造 vs 拆模块 | — | **拆模块**（新增渠道零改动，长期成本低） |
| 3 | **是否补 `/v1/responses` 与 `/v1/messages`** | 只 OpenAI / 全补 | **补 `/v1/responses`**（Grok 原生就是它，直通省一层转换）；`/v1/messages` 按需 |
| 4 | **多实例部署** | 单机 SQLite / SQLite+Redis | 先**单机**；底子的 `internal/redisstore` 保留接口不启用 |
| 5 | **渠道可见性开关**（照抄 codex2api） | 要 / 不要 | **要**——单机部署也会遇到「某渠道暂时全挂，想从面板藏起来」 |
| 6 | ~~public2api 与底子的关系~~ **已定** | fork 后改 / 新仓库搬代码 | ✅ **新仓库搬代码**：底子的前端与配置要重写，fork 会背着历史包袱 |

## 13. 附录：本计划用到的实测数据

| 仓库 | 非测试 Go 文件 | 非测试 Go 行数 |
| --- | --- | --- |
| `[panel]` workbuddy2api-panel（底子） | 72 | 21,259 |
| `[grok]` grok2api-chenyme（移植源） | 248 | 94,527 |
| `[grok]` 其中要移植的部分 | 76 | 29,587 |
| `[codex]` codex2api（模式参考） | 444 | 225,556 |

底子各包实测：

| 包 | 文件 | 行数 |
| --- | --- | --- |
| `internal/upstream` | 27 | 8,146 |
| `internal/panel` | 9 | 3,427 |
| `internal/pool` | 9 | 2,590 |
| `internal/server` | 6 | 1,745 |
| `internal/scheduler` | 5 | 1,112 |
| `internal/session` | 2 | 734 |
| `internal/usage` | 1 | 560 |
| `internal/auth` | 1 | 378 |
| `internal/redisstore` | 1 | 269 |
| `internal/livecfg` | 1 | 47 |

> [!NOTE] 数据来源
> 全部为本次在本机对四个仓库源码的实测统计（`find` + `wc -l`，已排除 `_test.go`）。
> 未包含前端资源、配置样例与文档。grok2api 的 94,527 行包含其自有的网关层（约 49,000 行），
> 那部分**不在移植范围内**——移植面按 §1.3 的目录逐项相加得出。

相关分析页：[[workbuddy2api-panel/index|workbuddy2api-panel]]、[[codex2api/index|codex2api]]、[[grok2api-chenyme/index|grok2api-chenyme]]。
方法论参考：[[抓取方法论]]；写作约定：[[写作规范]]。
