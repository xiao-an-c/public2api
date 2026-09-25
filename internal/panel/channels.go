package panel

import (
	"net/http"

	"github.com/xiao-an-c/public2api/internal/channel"
)

// channelDTO 是面板消费的渠道目录；菜单由能力派生，不在前端重复声明。
type channelDTO struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	UpstreamHost string    `json:"upstream_host"`
	Partition    string    `json:"partition,omitempty"`
	Kinds        []kindDTO `json:"kinds"`
	Menu         []menuDTO `json:"menu"`
}

type kindDTO struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
}

type menuDTO struct {
	Capability string `json:"capability"`
	Label      string `json:"label"`
}

func (p *Panel) channels(w http.ResponseWriter, r *http.Request) {
	out := make([]channelDTO, 0, len(p.cfg.Channels.All()))
	for _, c := range p.cfg.Channels.All() {
		d := channelDTO{
			ID: string(c.ID), Name: c.Name, UpstreamHost: c.UpstreamHost,
			Partition: c.Partition,
			Kinds:     make([]kindDTO, 0, len(c.Kinds)),
			Menu:      make([]menuDTO, 0, len(c.Menu())),
		}
		for _, k := range c.Kinds {
			d.Kinds = append(d.Kinds, kindDTO{ID: string(k.Kind), Name: k.Name, Capabilities: capStrings(k.Capabilities)})
		}
		for _, cap := range c.Menu() {
			d.Menu = append(d.Menu, menuDTO{Capability: string(cap), Label: cap.Label()})
		}
		out = append(out, d)
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
