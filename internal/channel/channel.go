// Package channel 定义 public2api 的渠道词汇：渠道（Channel）、账号类型（Kind）、
// 能力（Capability）。
//
// 这个包是全仓库的单一真源：数据库里的 channel/kind 列、API 的渠道路由、面板的
// 一级菜单与渠道维护空间二级菜单，全部从这里的常量与描述派生。其它包只引用，
// 不重新声明字符串字面量。
package channel

import (
	"fmt"
	"sort"
	"strings"
)

// ChannelID 是渠道的稳定标识。它落库、出现在 API 响应与配置文件里，
// 因此**发布后不可更改**——改名等于一次数据迁移。
type ChannelID string

const (
	// WBPChina 是 WorkBuddy 国内版（codebuddy.cn）账号体系。
	WBPChina ChannelID = "wbp-cn"
	// WBPGlobal 是 WorkBuddy 国际版（workbuddy.ai）账号体系。
	// 与 WBPChina 是两套独立账号：端点不同、凭据不通用、额度各算。
	WBPGlobal ChannelID = "wbp-global"
	// Grok 是 x.ai 的 Grok。三条入口（网页版 / Build / 控制台）登录方式与凭证
	// 形态完全不同，但在调用方眼里是同一个 Grok，所以归为一个渠道、三种账号类型。
	Grok ChannelID = "grok"
)

// Kind 是渠道内的账号类型，决定登录方式、凭据结构与上游协议。
//
// 一个 Kind 恰好属于一个渠道——这条约束由 Registry 强制，因为「这个账号属于哪个
// 渠道」必须能单值回答。
type Kind string

const (
	// KindWBP 是 WorkBuddy 的 OAuth 凭据，国内与国际两个渠道共用这一种形态，
	// 差异全部落在渠道配置（端点）里，不落成代码分支。
	KindWBP Kind = "wbp"
	// KindGrokWeb 是 grok.com 网页版：粘贴 SSO cookie，上游是私有 WebSocket，
	// 需要过 Cloudflare。
	KindGrokWeb Kind = "grok_web"
	// KindGrokBuild 是 Grok 官方命令行：走 RFC 8628 设备码授权，凭据可自动续期，
	// 上游是原生 Responses 协议。
	KindGrokBuild Kind = "grok_build"
	// KindGrokConsole 是 console.x.ai：粘贴静态凭据即可用，上游是原生 Responses，
	// **没有续期机制**，过期只能人工重粘。
	KindGrokConsole Kind = "grok_console"
)

// Capability 是渠道声明的能力。
//
// 它是本项目的核心杠杆：面板二级菜单、账号导入方式、定时任务调度、API 能力探测
// 全部由它派生，所以「加一个渠道要改前端」这件事不会发生。
type Capability string

const (
	// —— 账号接入 ——
	CapOAuthLogin  Capability = "account.oauth"      // 走浏览器授权流程换长期凭据
	CapDeviceLogin Capability = "account.device"     // 走设备码授权（无需回调地址）
	CapCredential  Capability = "account.credential" // 粘贴已有凭据（cookie / token）
	CapRefresh     Capability = "account.refresh"    // 凭据可自动续期

	// —— 额度 ——
	CapQuota Capability = "quota.query" // 能查上游剩余额度

	// —— 维护运营（决定渠道维护空间里有没有这一项）——
	CapKeepalive Capability = "ops.keepalive" // 定时保活，防止凭据失效
	CapCheckin   Capability = "ops.checkin"   // 定时签到
	CapTasks     Capability = "ops.tasks"     // 定时任务（上学 / 旅游 / 活动）
	CapEgress    Capability = "ops.egress"    // 需要管理出口（代理、clearance、签名服务）

	// —— 业务面 ——
	CapChat   Capability = "chat"   // 对话
	CapMedia  Capability = "media"  // 图片 / 视频生成
	CapSearch Capability = "search" // 联网搜索
)

// menuLabels 是能力到渠道维护空间二级菜单项的中文标签。
// 只列会出现在菜单里的能力；接入类能力（登录/凭据）体现在账号页内部，不单独成项。
var menuLabels = map[Capability]string{
	CapKeepalive: "保活",
	CapCheckin:   "签到",
	CapTasks:     "任务",
	CapEgress:    "出口",
	CapQuota:     "额度",
}

// Label 返回能力的中文标签。菜单只渲染有标签的能力，其余能力供后端逻辑使用。
func (c Capability) Label() string { return menuLabels[c] }

// MenuCapability 报告该能力是否应出现在渠道维护空间的二级菜单里。
func (c Capability) MenuCapability() bool { return menuLabels[c] != "" }

// KindSpec 描述一种账号类型：它叫什么、能做什么。
//
// 能力挂在 Kind 上而不是 Channel 上，是刻意的：渠道的能力集合是它的 Kind 的并集
// （见 Channel.Capabilities），所以「渠道声明了某个能力、但没有任何账号类型支持它」
// 这种不一致在结构上无法表达。
type KindSpec struct {
	Kind         Kind
	Name         string // 中文显示名
	Capabilities []Capability
}

// Has 报告该账号类型是否具备某能力。
func (s KindSpec) Has(c Capability) bool {
	for _, have := range s.Capabilities {
		if have == c {
			return true
		}
	}
	return false
}

// Channel 是一个渠道的完整描述。渠道是用户可见的维护空间。
type Channel struct {
	ID           ChannelID
	Name         string // 中文显示名
	UpstreamHost string // 上游主机，供面板展示与出口策略参考
	Kinds        []KindSpec
	Sort         int // 面板展示顺序

	// Partition 是该渠道在账号池里的**上游分区标识**，空串表示不分区。
	//
	// channel 包只负责携带它，**不解释**它——解释权在渠道适配器。这是刻意的：
	// 「账号怎么分区」是各上游自己的事，不是一个通用概念。
	//
	//	WorkBuddy：适配器把它当作底子既有的 realm（"cn" / "global"）传给 pool 的
	//	          分区过滤（AvailableUIDsForRealm 等）。底子那套机制已经成熟，
	//	          渠道层复用它，而不是另起一套分区概念。
	//	Grok：没有这个维度，留空。
	Partition string
}

// Capabilities 返回渠道能力集合——它的所有账号类型能力的并集，去重并排序。
//
// 这是派生值，不是独立声明的字段：改 Kind 的能力就自动改了渠道的能力，
// 不存在两处需要同步。
func (c Channel) Capabilities() []Capability {
	seen := make(map[Capability]struct{})
	for _, spec := range c.Kinds {
		for _, cap := range spec.Capabilities {
			seen[cap] = struct{}{}
		}
	}
	out := make([]Capability, 0, len(seen))
	for cap := range seen {
		out = append(out, cap)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Has 报告渠道整体是否具备某能力。
func (c Channel) Has(cap Capability) bool {
	for _, spec := range c.Kinds {
		if spec.Has(cap) {
			return true
		}
	}
	return false
}

// Kind 按标识取出账号类型描述。
func (c Channel) Kind(k Kind) (KindSpec, bool) {
	for _, spec := range c.Kinds {
		if spec.Kind == k {
			return spec, true
		}
	}
	return KindSpec{}, false
}

// Menu 返回该渠道维护空间的二级菜单项（有序）。
//
// 顺序固定为 账号 → 额度 → 保活 → 签到 → 任务 → 出口，保证面板稳定。
func (c Channel) Menu() []Capability {
	order := []Capability{CapQuota, CapKeepalive, CapCheckin, CapTasks, CapEgress}
	out := make([]Capability, 0, len(order))
	for _, cap := range order {
		if c.Has(cap) {
			out = append(out, cap)
		}
	}
	return out
}

// String 便于日志与错误信息里读。
func (c Channel) String() string { return fmt.Sprintf("%s(%s)", c.ID, c.Name) }

// parseChannelID 把外部输入（配置、API 参数）归一成 ChannelID。
// 未知值返回 ok=false，由调用方决定是报错还是回落到默认渠道。
func parseChannelID(s string) (ChannelID, bool) {
	id := ChannelID(strings.ToLower(strings.TrimSpace(s)))
	switch id {
	case WBPChina, WBPGlobal, Grok:
		return id, true
	default:
		return "", false
	}
}
