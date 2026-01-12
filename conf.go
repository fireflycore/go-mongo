package mongo

import "github.com/fireflycore/go-utils/tlsx"

// Conf 定义 MongoDB 连接初始化所需的配置项。
type Conf struct {
	Address  string `json:"address"`
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"password"`

	// Tls 为 TLS 配置，非空且字段齐全时启用双向 TLS。
	Tls *tlsx.TLS `json:"tls"`

	// MaxOpenConnects 用于控制连接池最大连接数（映射到 maxPoolSize）。
	MaxOpenConnects int `json:"max_open_connects"`
	// ConnMaxLifeTime 为连接最大空闲时间（秒），用于回收长时间空闲连接。
	ConnMaxLifeTime int `json:"conn_max_life_time"`

	// Logger 控制是否启用 Mongo 命令监控日志
	Logger bool `json:"logger"`

	// loggerHandle 为内部回调，用于输出结构化日志。
	loggerHandle func(b []byte)
	// loggerConsole 控制是否输出到控制台。
	loggerConsole bool
}

// WithLoggerConsole 设置是否将日志输出到控制台。
func (c *Conf) WithLoggerConsole(state bool) {
	c.loggerConsole = state
}

// WithLoggerHandle 设置结构化日志回调（用于接入自有日志系统）。
func (c *Conf) WithLoggerHandle(handle func(b []byte)) {
	c.loggerHandle = handle
}
