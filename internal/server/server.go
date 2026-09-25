// Package server 是 public2api 的 HTTP 层。
//
// 骨架阶段只有**只读**端点：渠道目录与健康检查。它们不依赖任何适配器——
// 渠道目录本身就是渠道注册表的输出，所以即使四条链路都还没接入，
// 这些端点也已经在说真话。
//
// 需要账号的端点（`/v1/chat/completions` 等）要等第一条适配器接入。
package server

import (
	"encoding/json"
	"net/http"

	"github.com/xiao-an-c/public2api/internal/channel"
)

// Server 持有只读端点需要的依赖。
type Server struct {
	reg *channel.Registry
	mux *http.ServeMux
}

// New 构造服务。路由在这里一次性注册完。
func New(reg *channel.Registry) *Server {
	s := &Server{reg: reg, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /healthz", s.healthz)
	s.mux.HandleFunc("GET /v1/channels", s.channels)
	s.mux.HandleFunc("GET /{$}", s.root)
	return s
}

// Handler 返回可挂到任意监听器上的处理器（测试用 httptest 也走这里）。
func (s *Server) Handler() http.Handler { return s.mux }

// healthz 恒返回 200，**不检查任何依赖**。
//
// 这与底子 workbuddy2api-panel 的语义一致：healthz 回答「进程还活着吗」，
// 不回答「能不能接活」。后者是就绪检查的事，等有账号概念了再加。
func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// root 给人和探针一个落脚点。
func (s *Server) root(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "public2api",
		"status":  "skeleton",
		"note":    "四条链路都还没有适配器，所以还没有对话端点",
		"endpoints": []string{
			"GET /healthz",
			"GET /v1/channels",
		},
	})
}

// channelDTO 是渠道目录的对外形状。
//
// 它是**刻意的投影**，不是内部结构的直接序列化：内部字段改名不该悄悄改掉
// 对外契约，反之亦然。
type channelDTO struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	UpstreamHost string        `json:"upstream_host"`
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
// 它把「菜单由能力派生」这件事直接暴露在 API 上：调用方能看到某个渠道
// 会有哪些维护入口，而不需要知道前端怎么渲染。
type menuItemDTO struct {
	Capability string `json:"capability"`
	Label      string `json:"label"`
}

// channels 输出渠道目录。这是计划 §8 里的调用方发现端点。
func (s *Server) channels(w http.ResponseWriter, _ *http.Request) {
	out := make([]channelDTO, 0, len(s.reg.All()))
	for _, c := range s.reg.All() {
		dto := channelDTO{
			ID:           string(c.ID),
			Name:         c.Name,
			UpstreamHost: c.UpstreamHost,
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

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(body)
}
