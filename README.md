# public2api

多渠道账号池网关：把 **WorkBuddy 国内**、**WorkBuddy 国际**、**Grok** 三路账号，
包装成对外一套 OpenAI / Anthropic 兼容接口。对外只有一个地址、一种协议，
对内按渠道、健康度与额度自己挑账号。

## 当前状态：骨架

**还不能用**——四条链路（`wbp` / `grok_web` / `grok_build` / `grok_console`）都还没有
适配器，所以还没有对外端点。渠道目录、能力模型、漂移守卫与存储层已就位。

```sh
go run ./cmd/public2api   # 打印能力矩阵与接入进度
```

## 三个渠道

| 渠道 | 上游 | 账号类型 | 独有能力 |
| --- | --- | --- | --- |
| `wbp-cn` | `codebuddy.cn` | `wbp` | 签到、任务（上学 / 旅游 / 活动） |
| `wbp-global` | `workbuddy.ai` | `wbp` | 同上（账号体系独立于国内） |
| `grok` | `grok.com` / `console.x.ai` | `grok_web`、`grok_build`、`grok_console` | 出口治理、联网搜索 |

**为什么 WorkBuddy 拆两个、Grok 不拆**：判据是「账号体系是否独立 + 运维策略是否要分开配」。
国内与国际是两套账号（端点不同、凭据不通用、额度各算）；Grok 三条入口共用同一份账号，
所以做成一个渠道下的三种账号类型。完整判据见 [`CONTEXT.md`](./CONTEXT.md)。

## 能力驱动面板

渠道维护空间的二级菜单由能力声明派生：

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

**凭据加密后落库，密钥不可更换。** 详见 [`docs/adr/0002`](./docs/adr/0002-credential-encryption.md)。

**迁移只增不改。** `internal/store` 里已发布的迁移条目不可修改，改结构加新的一条。
