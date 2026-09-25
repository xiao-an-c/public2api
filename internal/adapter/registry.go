package adapter

// registry 是全部适配器的注册表。
//
// 接入一条链路时在这里加一行，然后**不需要**改任何其它地方：
// 渠道目录（internal/channel）负责声明能力，漂移守卫
// （internal/channel/drift_test.go）负责核对两者一致。
//
// 目前为空——四条链路都还没接入。
var registry []Adapter

// All 返回全部已注册的适配器。
func All() []Adapter { return append([]Adapter(nil), registry...) }
