## go-mongo

基于官方 mongo-driver 的工程化封装，提供：
- MongoDB 初始化（TLS、连接池、探活 Ping）
- Mongo 命令日志输出（控制台/回调，基于 CommandMonitor）
- 常用模型基类（ObjectID 主键 + created_at/updated_at/deleted_at）
- 常用工具（分页、时间过滤、物理删除/软删除）

## 安装

```bash
go get github.com/fireflycore/go-mongo
```

## 快速开始

```go
package main

import (
	"context"

	"github.com/fireflycore/go-mongo"
)

func main() {
	conf := &mongo.Conf{
		Address:  "127.0.0.1:27017",
		Database: "demo",
		Logger:   true,
	}

	conf.WithLoggerConsole(true)
	conf.WithLoggerHandle(func(b []byte) {
		_ = b
	})

	db, err := mongo.New(conf)
	if err != nil {
		panic(err)
	}

	_ = db.Collection("demo").FindOne(context.Background(), map[string]any{})
}
```

## 配置说明

初始化配置为 `mongo.Conf`。

常用字段：
- Address：MongoDB 地址，通常为 host:port（内部会拼接为 mongodb://{Address}）
- Database/Username/Password：连接信息（Username 不为空时启用认证）
- Tls：TLS 配置（见下文）
- MaxOpenConnects：连接池最大连接数（映射到 maxPoolSize）
- ConnMaxLifeTime：连接最大空闲时间（单位：秒，<=0 表示不设置）
- Logger：启用 Mongo 命令日志（配合 WithLoggerConsole / WithLoggerHandle）

说明：
- MaxIdleConnects 目前未设置到 mongo-driver 的 options 中，属于预留字段

### TLS

当 `Conf.Tls` 同时配置了 `CaCert / ClientCert / ClientCertKey` 三个文件路径时启用 TLS，否则视为不启用：

```go
conf := &mongo.Conf{
	Address:  "127.0.0.1:27017",
	Database: "demo",
	Tls: &mongo.TLS{
		CaCert:        "/path/to/ca.pem",
		ClientCert:    "/path/to/client.pem",
		ClientCertKey: "/path/to/client.key",
	},
}
```

### Mongo 命令日志回调

开启 `Logger` 后，你可以把 Mongo 命令日志输出到控制台，或写入自定义回调：

```go
conf := &mongo.Conf{
	Address:  "127.0.0.1:27017",
	Database: "demo",
	Logger:   true,
}

conf.WithLoggerConsole(true)
conf.WithLoggerHandle(func(b []byte) {
	_ = b
})
```

回调收到的是一段 JSON bytes。若你的 Mongo 操作使用了 `collection.Find(ctx, ...)` 等并且 `ctx` 是 gRPC 的 incoming context，则会尝试从 metadata 里读取链路字段：
- trace-id
- user-id
- app-id

## 模型基类

go-mongo 提供 `mongo.Table` 可直接嵌入到你的实体中：

```go
type TestEntity struct {
	mongo.Table
}
```

注意：`BeforeInsert/BeforeUpdate` 需要你在写入前手动调用（本库不会自动注册 driver hook）。

## 常用工具

### 分页

```go
import (
	"go.mongodb.org/mongo-driver/mongo/options"
)

opt := options.Find()
mongo.WithPagination(opt, 1, 20)
```

分页规则：
- page 从 1 开始（0 会被修正为 1）
- size 范围为 [5, 100]（0 会被修正为 5，大于 100 修正为 100）

### 时间过滤

`WithTimerFilter` 会向 filter 追加 `created_at` 的时间范围条件，`start/end` 需要是 Go 的 `time.DateTime` 格式（如 `2006-01-02 15:04:05`）：

```go
import (
	"go.mongodb.org/mongo-driver/bson"
)

filter := bson.D{}
mongo.WithTimerFilter("2026-01-01 00:00:00", "2026-01-02 00:00:00", &filter)
```

### 删除/软删除

```go
// 物理删除
_, err := mongo.DeleteById(ctx, collection, id)

// 软删除：写入 updated_at/deleted_at（UTC）
_, err := mongo.SoftDeleteById(ctx, collection, id)
```
