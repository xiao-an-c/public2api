package server

import (
	"strings"

	"github.com/xiao-an-c/public2api/internal/channel"
)

// resolveModel 解析模型名协议：
//
//	分渠道前缀： "[channel:]model"
//
// 新前缀使用稳定 ChannelID（wbp-cn / wbp-global / grok）；旧 cn/global
// 保留为 WorkBuddy 分区别名。未知前缀仍视为裸模型名，避免破坏含冒号的模型 ID。
// bare 即出站/选号/账本使用的裸模型名。
func resolveModel(model string) (realm, bare string) {
	idx := strings.IndexByte(model, ':')
	if idx < 0 {
		return "cn", model
	}
	prefix := model[:idx]
	switch prefix {
	case "cn":
		return "cn", model[idx+1:]
	case "global":
		return "global", model[idx+1:]
	case string(channel.WBPChina):
		return "cn", model[idx+1:]
	case string(channel.WBPGlobal):
		return "global", model[idx+1:]
	case string(channel.Grok):
		// Grok 尚未接入当前 WorkBuddy handler；先保留渠道前缀的可解析性，
		// 由后续 Grok 路由层消费 channel ID，而不是把它误投到 CN。
		return string(channel.Grok), model[idx+1:]
	default:
		return "cn", model
	}
}

// ResolveModel 是 resolveModel 的导出面（跨包调用）。
func ResolveModel(model string) (realm, bare string) { return resolveModel(model) }
