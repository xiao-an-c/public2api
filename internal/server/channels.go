package server

import (
	"net/http"

	"github.com/xiao-an-c/public2api/internal/channel"
)

// 本文件是渠道目录的对外端点。
//
// 它是「渠道是一等公民」的对外证据：调用方据此发现有哪些渠道可用、每个渠道
// 有哪些账号类型、以及各自的维护入口。与 /v1/models 不同，它**不依赖任何账号**
// ——渠道目录本身就是注册表的输出，所以账号池为空时它照样说真话。

// channelDTO 是渠道的对外形状。
//
// 它是**刻意的投影**，不是内部结构的直接序列化：内部字段改名不该悄悄改掉
// 对外契约，反之亦然。
type channelDTO struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	UpstreamHost string        `json:"upstream_host"`
	Partition    string        `json:"partition,omitempty"`
	Kinds        []kindDTO     `json:"kinds"`
	Capabilities []string      `json:"capabilities"`
	Menu         []menuItemDTO `json:"menu"`
}

type kindDTO struct {
	Kind         string   `json:"kind"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
}

// menuItemDTO 是渠道维护空间的二级菜单项。
//
// 把「菜单由能力派生」直接暴露在 API 上：调用方能看出某个渠道会有哪些维护入口，
// 而不需要知道前端怎么渲染。
type menuItemDTO struct {
	Capability string `json:"capability"`
	Label      string `json:"label"`
}

// listChannels 输出渠道目录。
func (h *Handler) listChannels(w http.ResponseWriter, _ *http.Request) {
	all := h.channels.All()
	out := make([]channelDTO, 0, len(all))
	for _, c := range all {
		dto := channelDTO{
			ID:           string(c.ID),
			Name:         c.Name,
			UpstreamHost: c.UpstreamHost,
			Partition:    c.Partition,
			Kinds:        make([]kindDTO, 0, len(c.Kinds)),
			Capabilities: capStrings(c.Capabilities()),
		}
		for _, spec := range c.Kinds {
			dto.Kinds = append(dto.Kinds, kindDTO{
				Kind:         string(spec.Kind),
				Name:         spec.Name,
				Capabilities: capStrings(spec.Capabilities),
			})
		}
		for _, cap := range c.Menu() {
			dto.Menu = append(dto.Menu, menuItemDTO{
				Capability: string(cap),
				Label:      cap.Label(),
			})
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": out})
}

func capStrings(caps []channel.Capability) []string {
	out := make([]string, 0, len(caps))
	for _, c := range caps {
		out = append(out, string(c))
	}
	return out
}
