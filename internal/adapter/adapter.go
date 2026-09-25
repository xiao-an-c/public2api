// Package adapter 是上游适配器的接缝：每个账号类型一个实现，把「对话、查额度、
// 续期、导入凭据」这些动作翻译成对应上游的协议。
//
// 设计要点是**能力接口细分**：适配器只实现它真有的能力接口，而不是被逼为没有的
// 能力写空实现。grok_console 不实现 Refresher，调用方断言失败就走「不支持」分支——
// 这比返回 ErrNotSupported 更早暴露问题，因为它把错误从运行期提到了装配期。
package adapter

import (
	"context"
	"fmt"
	"time"

	"github.com/xiao-an-c/public2api/internal/account"
	"github.com/xiao-an-c/public2api/internal/channel"
)

// Adapter 是所有适配器的基接口。
type Adapter interface {
	// Kind 报告它服务哪种账号类型。装配时用于建索引与查漂移。
	Kind() channel.Kind
	// Capabilities 声明它能做什么。
	//
	// TODO(grilling): 这个方法是**过渡性**的，也是本设计已知的漂移敞口——
	// 手工声明可能与「实际实现了哪些能力接口」不一致。等能力接口清单补齐
	// （见下方 Chatter / Quoter / Refresher / Importer 之外的九个能力），
	// 就用 Detect 全面取代它，让漂移在编译期不可能发生，然后删掉这个方法。
	Capabilities() []channel.Capability
}

// —— 能力接口 ——
//
// 实现哪个接口 = 具备哪个能力。每个接口只有一个方法，因为能力本身就该是窄的：
// 一个适配器同时是 Chatter 和 Quoter 很正常，但把它们并成一个胖接口会逼
// 不支持额度的上游写空实现。

// Chatter 是对话能力（CapChat）。
type Chatter interface {
	Chat(ctx context.Context, acct account.Account, req ChatRequest) (Stream, error)
}

// Quoter 是查额度能力（CapQuota）。
type Quoter interface {
	Quota(ctx context.Context, acct account.Account) (account.Quota, error)
}

// Refresher 是凭据续期能力（CapRefresh）。
//
// 不实现它的账号类型（如 grok_console）意味着凭据会过期且只能人工重粘，
// 面板必须为这类账号显示「失效告警 + 一键重粘」而不是「自动续期」。
type Refresher interface {
	Refresh(ctx context.Context, acct account.Account) (Credential, error)
}

// Importer 是粘贴凭据导入能力（CapCredential）。
type Importer interface {
	Import(ctx context.Context, raw string) (Credential, error)
}

// Detect 探测适配器**实际实现**了哪些能力接口。
//
// 它只覆盖已经有专属接口的能力；其余能力暂时只能靠 Capabilities() 的自觉声明。
// 这个覆盖面会随能力接口补齐而扩大，直到完全取代 Capabilities()。
func Detect(a Adapter) []channel.Capability {
	var out []channel.Capability
	if _, ok := a.(Chatter); ok {
		out = append(out, channel.CapChat)
	}
	if _, ok := a.(Quoter); ok {
		out = append(out, channel.CapQuota)
	}
	if _, ok := a.(Refresher); ok {
		out = append(out, channel.CapRefresh)
	}
	if _, ok := a.(Importer); ok {
		out = append(out, channel.CapCredential)
	}
	return out
}

// Detected 覆盖的能力集合，供装配期判断「还有哪些能力只有声明没有接口」。
func Detected() []channel.Capability {
	return []channel.Capability{
		channel.CapChat, channel.CapQuota, channel.CapRefresh, channel.CapCredential,
	}
}

// Credential 是一份**解密后**的凭据明文。
//
// 它只在适配器发起请求前的短暂窗口里存在，绝不落库、绝不进日志——所以
// String 与 GoString 都被改写，避免它被 fmt 顺手打出来。
type Credential struct {
	Kind channel.Kind
	// Fields 是凭据字段，结构由 Kind 决定：
	//   KindWBP           → access_token / refresh_token / expires_at / realm
	//   KindGrokWeb       → sso
	//   KindGrokBuild     → access_token / refresh_token / expires_at
	//   KindGrokConsole   → token
	Fields map[string]string
}

// String 刻意打码：凭据明文永远不该出现在日志或错误信息里。
func (c Credential) String() string {
	return fmt.Sprintf("Credential{kind:%s, fields:%d}", c.Kind, len(c.Fields))
}

// GoString 覆盖 %#v，理由同上。
func (c Credential) GoString() string { return c.String() }

// ChatRequest 是一次对话请求。
//
// TODO(grilling): 这个结构是**占位**。真正的字段集合取决于协议层怎么设计——
// 是让所有适配器都走 Responses 协议（Grok build/console 原生就是它，
// 其余做转换），还是每个适配器各自处理下游协议。这个选择会大幅改变本结构的形状，
// 所以现在只放最小字段，不预设。
type ChatRequest struct {
	Model    string
	Messages []Message
	Stream   bool
}

// Message 是对话中的一条消息。
type Message struct {
	Role    string
	Content string
}

// Chunk 是流式响应的一片。
type Chunk struct {
	// Delta 是本次新增的正文。
	Delta string
	// Reasoning 是本次新增的思考过程（上游提供时才有）。
	Reasoning string
	// Done 标记流结束，此后不应再读。
	Done bool
	// Usage 只在最后一片上有值。
	Usage *Usage
}

// Usage 是一次调用的用量。
type Usage struct {
	PromptTokens     int64
	CompletionTokens int64
	// Model 是上游实际使用的模型名，可能与请求的不同（别名解析后）。
	Model string
	// Latency 是上游耗时。
	Latency time.Duration
}

// Stream 是流式响应的读取端。
//
// 读完后必须 Close。约定：读到 io.EOF 表示正常结束，其它错误表示上游失败，
// 调用方据此决定是否换号重试。
type Stream interface {
	// Recv 返回下一片。返回 io.EOF 表示流正常结束。
	Recv() (Chunk, error)
	// Close 释放底层连接。可重复调用。
	Close() error
}
