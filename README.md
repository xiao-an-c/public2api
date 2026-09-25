# public2api

多渠道账号池网关：把 **WorkBuddy 国内**、**WorkBuddy 国际**、**Grok** 三路账号，
包装成对外一套 OpenAI / Anthropic 兼容接口。对外只有一个地址、一种协议，
对内按渠道、健康度与额度自己挑账号。

## 当前状态：骨架

已就位：

- **渠道目录**（`internal/channel`）——3 渠道 × 4 种账号类型 × 12 项能力，
  每条能力分配都带来源依据
- **漂移守卫**（`Registry.Drift`）——装配期核对「渠道声明」与「适配器实现」，
  双向查空头承诺与隐性能力
- **账号模型**（`internal/account`）——连续量健康分、账号级与模型级两级冷却
- **存储层**（`internal/store`）——SQLite 迁移，9 张表，含幂等与约束测试

尚未开始：**所有适配器**。四条链路（`wbp` / `grok_web` / `grok_build` /
`grok_console`）都还没有实现，所以还没有对外端点。

```sh
go run ./cmd/public2api          # 打印能力矩阵、接入进度与漂移检查
go test ./...
```

## 三个渠道

| 渠道 | 上游 | 账号类型 | 独有能力 |
| --- | --- | --- | --- |
| `wbp-cn` | `codebuddy.cn` | `wbp` | 签到、任务（上学 / 旅游 / 活动） |
| `wbp-global` | `workbuddy.ai` | `wbp` | 同上（账号体系独立于国内） |
| `grok` | `grok.com` / `console.x.ai` | `grok_web`、`grok_build`、`grok_console` | 出口治理、联网搜索 |

**为什么 WorkBuddy 要拆成两个**：国内版与国际版是两套独立账号体系——登录端点不同、
凭据不通用、额度各算。合成一个渠道，任何一侧的限流或改版都会污染另一侧的调度决策。

**为什么 Grok 是一个渠道三种账号**：三条入口的登录方式和凭证形态完全不同，但在调用方
眼里是同一个 Grok。所以它们作为**账号类型**并列，而不是三个渠道。

## 能力驱动面板

渠道维护空间的二级菜单**不是前端配置**，是渠道能力声明的派生：

```
WorkBuddy 国内          Grok
├─ 账号                 ├─ 账号（Web / Build / Console 三个子页）
├─ 额度                 ├─ 额度
├─ 保活                 ├─ 保活
├─ 签到                 └─ 出口
└─ 任务
```

加渠道、加菜单项都不该改前端——需要改前端就说明抽象漏了。

## 文档

| 文件 | 内容 |
| --- | --- |
| [`AGENTS.md`](./AGENTS.md) | 给 agent 的入口：词汇指针、实现来源、硬约束 |
| [`CONTEXT.md`](./CONTEXT.md) | 领域词汇表（渠道 / 账号类型 / 能力 / 漂移 / 维护空间） |
| [`docs/plan-public2api.md`](./docs/plan-public2api.md) | 实施计划：数据模型、面板 IA、移植清单、里程碑、风险 |
| [`docs/adr/`](./docs/adr/) | 架构决策记录 |

## 开发注意

**拉依赖要换源。** 本机 `proxy.golang.org` 不可达：

```sh
GOPROXY=https://goproxy.cn,direct go mod tidy
```

**凭据永不落明文。** 详见 [`docs/adr/0002`](./docs/adr/0002-credential-encryption.md)。

**迁移只增不改。** `internal/store` 里已发布的迁移条目不可修改，改结构加新的一条。
