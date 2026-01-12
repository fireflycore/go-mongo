package mongo

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/fireflycore/go-mongo/internal"
	"github.com/fireflycore/go-utils/tlsx"
	"go.mongodb.org/mongo-driver/event"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// New 根据配置创建 MongoDB 连接并返回数据库句柄。
func New(c *Conf) (*mongo.Database, error) {
	if c == nil {
		return nil, errors.New("mongo: conf is nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOptions := options.Client()

	if c.Username != "" {
		credential := options.Credential{
			Username: c.Username,
		}
		if c.Password != "" {
			credential.Password = c.Password
		}
		clientOptions.SetAuth(credential)
	}

	// 从配置生成 TLSConfig；tlsEnabled 表示是否启用 TLS。
	tlsConfig, tlsEnabled, err := tlsx.NewTLSConfig(c.Tls)
	// TLS 配置构造失败时直接返回错误。
	if err != nil {
		return nil, err
	}
	// 启用 TLS 时，将 TLSConfig 写入 clientOptions。
	if tlsEnabled {
		// 由 driver 使用该 TLS 配置建立安全连接。
		clientOptions.TLSConfig = tlsConfig
	}

	// 将 address 组装为 MongoDB 标准 URI。
	uri := fmt.Sprintf("mongodb://%s", c.Address)
	// 把 URI 应用到 clientOptions。
	clientOptions.ApplyURI(uri)

	// 设置 BSON 编解码行为。
	clientOptions.SetBSONOptions(&options.BSONOptions{
		UseLocalTimeZone: false, // 关闭本地时区，减少环境差异带来的时间解析偏差。
	})

	if c.MaxOpenConnects > 0 {
		// 设置连接池最大连接数。
		clientOptions.SetMaxPoolSize(uint64(c.MaxOpenConnects))
	}
	if c.ConnMaxLifeTime > 0 {
		// 设置最大空闲时间。
		clientOptions.SetMaxConnIdleTime(time.Second * time.Duration(c.ConnMaxLifeTime))
	}

	// 启用日志时，安装命令监控器以采集 Mongo 命令执行信息。
	if c.Logger {
		logger := internal.NewLogger(&internal.Conf{ // 构造内部 logger 配置并返回 logger 实例。
			SlowThreshold: 200 * time.Millisecond, // 慢查询阈值，超过则按 warn 输出。
			LogLevel:      internal.Info,          // 日志级别：默认 Info。
			Colorful:      true,                   // 是否开启彩色控制台输出。
			Database:      c.Database,             // 写入日志字段，用于区分数据库实例。
			Console:       c.loggerConsole,        // 是否输出到控制台。
		}, c.loggerHandle) // loggerHandle 非空时会收到结构化 JSON 日志。

		// stmts 用于缓存 RequestID 对应的命令文本，供结束事件读取。
		var stmts sync.Map
		// 绑定命令监控回调（开始/成功/失败）。
		clientOptions.Monitor = &event.CommandMonitor{
			// Started 在命令开始时触发。
			Started: func(ctx context.Context, e *event.CommandStartedEvent) {
				// 缓存 requestId->command string，供后续成功/失败取回。
				stmts.Store(e.RequestID, e.Command.String())
			},
			// Succeeded 在命令成功时触发。
			Succeeded: func(ctx context.Context, e *event.CommandSucceededEvent) {
				// smt 用于保存命令字符串（若能从 map 中取到）。
				var smt string
				// 通过 RequestID 找到对应的命令文本。
				if v, ok := stmts.Load(e.RequestID); ok {
					// 做类型断言并赋值（失败则保持空字符串）。
					smt, _ = v.(string)
					// 取出后删除，避免 map 增长。
					stmts.Delete(e.RequestID)
				}
				// 记录成功 Trace，err 字符串为空。
				logger.Trace(ctx, e.RequestID, e.Duration, smt, "")
			},
			// Failed 在命令失败时触发。
			Failed: func(ctx context.Context, e *event.CommandFailedEvent) {
				// smt 用于保存命令字符串（若能从 map 中取到）。
				var smt string
				// 通过 RequestID 找到对应的命令文本。
				if v, ok := stmts.Load(e.RequestID); ok {
					smt, _ = v.(string)
					stmts.Delete(e.RequestID)
				}
				// 记录失败 Trace，err 为 driver 提供的失败信息。
				logger.Trace(ctx, e.RequestID, e.Duration, smt, e.Failure)
			},
		}
	}

	// 用构造好的 options 建立客户端连接。
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, err
	}

	// Ping 用于验证连接可用与认证正确。
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		return nil, err
	}

	// 选择默认数据库并返回对应句柄。
	db := client.Database(c.Database)

	return db, nil
}
