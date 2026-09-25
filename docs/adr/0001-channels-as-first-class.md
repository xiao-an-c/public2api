# 1. 渠道是一等公民，能力挂在账号类型上

日期：2026-09-25
状态：已接受

## 背景

底子 `workbuddy2api-panel` 只有一家上游，但内部已经有 `realm`（cn / global）的概念：
`internal/pool/realm.go` 有池级过滤，`internal/auth/auth.go` 有归一化与推断，
`internal/server/resolve_model.go` 有 `[cn|global:]model` 模型前缀。

它的问题是 `realm` 只是**筛选条件**，不是结构：账号混在一个池子里靠一个域标记区分，
导致三件事做不到——① 给不同渠道配不同策略（国内号要签到、Grok 要挂代理）；
② 对外区分（调用方想「只用国际号」只能靠模型名前缀硬凑）；③ 按渠道统计与限流。

同时要接入的 Grok 有三条入口（网页版 / Build / 控制台），登录方式、凭据结构、
上游协议各不相同。这带来一个建模问题：它们是三个渠道，还是一个渠道的三种形态？

参考实现 `codex2api` 把渠道做成了贯穿全栈的一等公民（`database/visible_channels_settings.go:16`
的 `AllUpstreamChannels`），有五个挂载点：渠道枚举、账号页 Tab、可见性开关、
**API Key 渠道作用域**、渠道级批量操作。其中 Key 作用域最有价值——它把渠道从
运维概念变成了产品能力。

## 决策

**两级模型**：`Channel`（渠道，3 个）× `Kind`（账号类型，4 种）。

```
wbp-cn        kinds: [wbp]
wbp-global    kinds: [wbp]          ← 与国内共用同一种账号类型
grok          kinds: [grok_web, grok_build, grok_console]
```

**拆渠道的判据**（两条都满足才拆）：① 账号体系是否独立（凭据能不能通用、额度是否各算）；
② 运维策略是否要能分开配。WorkBuddy 国内/国际两条都满足；Grok 三条入口共享同一份账号
（同一个 SSO cookie 能换出三种凭据），策略也一致，所以做成一个渠道下的三种账号类型。

**能力挂在账号类型上，不挂在渠道上。** 渠道的能力集合是它的账号类型的并集
（`Channel.Capabilities()`），是派生值而非独立字段。

## 后果

**好处**：渠道可以独立配策略、独立计量、独立限流；API Key 可以锁定渠道
（照抄 codex2api 的 `upstream_key.upstream_channel`）；面板一级菜单放通用能力、
每个渠道是一个带二级菜单的维护空间。

**实现时发现计划里的一处错误**：计划 §2.2 把 `Capabilities` 写成了 `Channel` 上的
声明字段。真正实现时发现这会制造一个可以表达的不一致——「渠道声明了某能力，
但没有任何账号类型支持它」。改成派生之后这个状态在结构上无法表达，少了一整类 bug。

**因此也修正了 `Registry` 的一条约束**：最初写了「Kind 全局唯一」，但 `wbp-cn` 与
`wbp-global` 共用 `KindWBP` 是正确建模，这条约束会误杀它。正确的不变式是
「账号的渠道由 `accounts.channel` 列单值回答，不由 Kind 反推」，所以 `Registry`
提供的是 `ChannelsOf(kind) []ChannelID` 而不是单值查询。

**代价**：
- 现有 `realm` 数据要迁移（`cn` → `wbp-cn`，`global` → `wbp-global`）；
- 面板信息架构要重写（底子的 `app.js` 1,648 行是单渠道硬编码）；
- 模型名前缀解析要扩展且**不能破坏兼容**——现有实现是「前段恰好是 `cn`/`global`
  才剥离，否则整串当裸模型名」，这个保守策略必须保留，否则 `gpt-4:latest`
  这类带冒号的模型名会被错误切分。

**被否决的方案**：
- *6 个渠道*（Grok 三条各算一个）——同一个账号的三条入口被拆到三个渠道，
  账号会在三处重复出现，且「同一个 Grok 账号」这个概念消失。
- *保持 realm 作为筛选条件*——省一次重构，但上述三件事永远做不到。
