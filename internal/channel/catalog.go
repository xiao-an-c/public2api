package channel

// 本文件是渠道目录：三个渠道的完整描述。它是计划 §3「渠道差异矩阵」的可执行版本，
// 每条能力分配都能追回源码事实（依据写在注释里）。
//
// 标了 TODO(grilling) 的地方是**尚未逐行核实**的分配——grilling 阶段要把它们钉死，
// 在那之前不要当成事实使用。

// wbpChinaCapabilities 是 WorkBuddy 国内版的能力集合。
// 国内版有独立的签到、成长任务与猫猫旅行维护空间。
var wbpChinaCapabilities = []Capability{
	CapOAuthLogin, // 插件 OAuth 三端点（state / token / account）
	CapRefresh,    // refresh token 自动续期，提前 10 分钟
	CapQuota,      // 积分可读，是选号依据
	CapKeepalive,  // 定时保活
	CapCheckin,    // 国内签到
	CapTasks,      // 国内上学 / 旅游 / 活动任务
	CapChat,
	CapMedia, // 图片生成
}

// wbpGlobalCapabilities 是 WorkBuddy 国际版的能力集合。
// 国际版目前没有国内版那套签到/成长任务端点；不能把“目录里有菜单”
// 当成“后端可以执行”。保留额度与保活，后续核实出国际任务端点后再加能力。
var wbpGlobalCapabilities = []Capability{
	CapOAuthLogin,
	CapRefresh,
	CapQuota,
	CapKeepalive,
	CapChat,
	CapMedia,
}

// grokEgress 说明：Grok 三条链路都经出口层发请求（TLS 指纹伪装 + 代理租约），
// 所以三条都声明 CapEgress。差异在出口层的**用法**上——
// grok_web 额外需要 Cloudflare clearance 与一个外部签名服务算防爬标记，
// build / console 只需要代理本身。这层差异落在适配器实现里，不落成两种能力。
const grokEgressNote = "三条链路都走出口层；仅 web 额外需要 clearance 与签名服务"

// Catalog 返回全部渠道，顺序即面板展示顺序。
func Catalog() []Channel {
	return []Channel{
		{
			ID:           WBPChina,
			Name:         "WorkBuddy 国内",
			UpstreamHost: "codebuddy.cn",
			Sort:         10,
			Partition:    "cn", // 底子的 realm，池级过滤沿用它的机制
			Kinds: []KindSpec{{
				Kind:         KindWBP,
				Name:         "WorkBuddy 账号",
				Capabilities: wbpChinaCapabilities,
			}},
		},
		{
			ID:           WBPGlobal,
			Name:         "WorkBuddy 国际",
			UpstreamHost: "workbuddy.ai",
			Sort:         20,
			Partition:    "global",
			Kinds: []KindSpec{{
				Kind:         KindWBP,
				Name:         "WorkBuddy 账号",
				Capabilities: wbpGlobalCapabilities,
			}},
		},
		{
			ID:           Grok,
			Name:         "Grok",
			UpstreamHost: "grok.com",
			Sort:         30,
			// Partition 留空：Grok 没有「域」这个维度，它的三条链路靠 Kind 区分。
			Kinds: []KindSpec{
				{
					Kind: KindGrokWeb,
					Name: "Grok 网页版",
					Capabilities: []Capability{
						CapCredential, // 粘贴浏览器里的 SSO cookie
						// TODO(grilling): grok_web 的凭据能否自动续期尚未逐行核实。
						// 已知仓库里有 SSO→Build 转换（把 web 的 cookie 换成 build 的
						// OAuth），但那是「换一种凭据」，不等于「这份凭据能续」。
						// 在核实之前不声明 CapRefresh。
						CapQuota,
						CapKeepalive,
						CapEgress,
						CapChat,
						CapMedia,
						CapSearch,
					},
				},
				{
					Kind: KindGrokBuild,
					Name: "Grok Build",
					Capabilities: []Capability{
						CapDeviceLogin, // RFC 8628 设备码，无需回调地址
						CapRefresh,     // 有 refresh token，可自动续期
						CapQuota,
						CapKeepalive,
						CapEgress,
						CapChat,
						CapMedia,
						CapSearch,
					},
				},
				{
					Kind: KindGrokConsole,
					Name: "Grok 控制台",
					Capabilities: []Capability{
						CapCredential, // 粘贴静态凭据
						// 刻意**不**声明 CapRefresh：这份凭据没有续期机制，
						// 过期只能人工重粘。声明了它就会漂移。
						CapQuota,
						CapKeepalive,
						CapEgress,
						CapChat,
						CapMedia,
						CapSearch,
					},
				},
			},
		},
	}
}

// allCapabilities 是全部能力的稳定顺序，供面板渲染能力矩阵用。
var allCapabilities = []Capability{
	CapOAuthLogin, CapDeviceLogin, CapCredential, CapRefresh,
	CapQuota,
	CapKeepalive, CapCheckin, CapTasks, CapEgress,
	CapChat, CapMedia, CapSearch,
}

// AllCapabilities 返回全部能力（固定顺序）。
func AllCapabilities() []Capability { return append([]Capability(nil), allCapabilities...) }
