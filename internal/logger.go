package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/grpc/metadata"
)

const (
	// ColorReset 重置 ANSI 颜色。
	ColorReset = "\033[0m"
	// ColorYellow 黄色（耗时/提示）。
	ColorYellow = "\033[33m"
	// ColorBlueBold 蓝色加粗（字段高亮）。
	ColorBlueBold = "\033[34;1m"
	// ColorRedBold 红色加粗（错误高亮）。
	ColorRedBold = "\033[31;1m"

	// LogTypeMongo 为日志类型标识（供下游聚合检索）。
	LogTypeMongo = 6
	// ResultSuccess 为成功执行的结果标记。
	ResultSuccess = "success"

	// Firefly系统自定义头部（统一前缀）
	HeaderPrefix = "x-firefly-"

	// TraceId 为从 metadata 读取 trace id 的 key。
	TraceId = HeaderPrefix + "trace-id"
	// UserId 为从 metadata 读取 user id 的 key。
	UserId = HeaderPrefix + "user-id"
	// AppId 为从 metadata 读取 app id 的 key。
	AppId = HeaderPrefix + "app-id"
)

// LogLevel 定义日志级别枚举。
type LogLevel int

const (
	Silent LogLevel = iota + 1 // Silent 表示不记录任何日志。
	Error                      // Error 表示仅记录错误日志。
	Warn                       // Warn 表示记录慢查询与错误日志。
	Info                       // Info 表示记录所有日志。
)

// Conf 为 logger 的配置。
type Conf struct {
	// Console 控制是否输出到控制台。
	Console bool
	// SlowThreshold 为慢查询阈值。
	SlowThreshold time.Duration
	// Colorful 控制是否启用彩色输出。
	Colorful bool
	// Database 为库名字段，用于检索与聚合。
	Database string
	// LogLevel 为日志级别。
	LogLevel LogLevel
}

// Interface 约束 logger 需要提供的能力。
type Interface interface {
	// Trace 记录一次命令的执行信息。
	Trace(ctx context.Context, id int64, elapsed time.Duration, smt string, err string)
}

type logger struct {
	Conf                      // Conf 嵌入，复用配置字段。
	traceStr     string       // traceStr 为普通 trace 模板。
	traceWarnStr string       // traceWarnStr 为慢查询模板。
	traceErrStr  string       // traceErrStr 为错误模板。
	handle       func([]byte) // handle 为结构化日志回调（可为空）。
}

// NewLogger 构造一个新的 logger，并按配置决定输出模板。
func NewLogger(conf *Conf, handle func([]byte)) Interface {
	// baseFormat 为默认输出模板。
	baseFormat := "[%s] [%s] [Database:%s] [RequestId:%d] [Duration:%.3fms]%s\n%s"
	// traceStr 默认使用 baseFormat。
	traceStr := baseFormat
	// traceWarnStr 默认使用 baseFormat。
	traceWarnStr := baseFormat
	// traceErrStr 为错误输出模板。
	traceErrStr := "[%s] [%s] [Database:%s] [RequestId:%d] [Duration:%.3fms] %s\n%s"

	// 彩色输出时替换模板为 ANSI 颜色版本。
	if conf.Colorful {
		// colorPrefix 为彩色前缀模板。
		colorPrefix := "[%s] [%s] " + ColorBlueBold + "[Database:%s] " + ColorBlueBold + "[RequestId:%d] " + ColorYellow
		// 普通日志模板。
		traceStr = colorPrefix + "[Duration:%.3fms]\n" + ColorReset + "%s"
		// 慢查询模板。
		traceWarnStr = colorPrefix + "[Duration:%.3fms] " + ColorYellow + "%s\n" + ColorReset + "%s"
		// 错误模板。
		traceErrStr = colorPrefix + "[Duration:%.3fms] " + ColorRedBold + "%s\n" + ColorReset + " %s"
	}

	return &logger{
		Conf:         *conf,
		traceStr:     traceStr,
		traceWarnStr: traceWarnStr,
		traceErrStr:  traceErrStr,
		handle:       handle,
	}
}

func (l *logger) Trace(ctx context.Context, id int64, elapsed time.Duration, smt string, err string) {
	// Silent 模式不输出任何日志。
	if l.LogLevel <= Silent {
		return
	}

	date := time.Now().Format(time.DateTime)

	switch { // 按错误/慢查询/普通信息分支记录日志。
	case len(err) > 0 && l.LogLevel >= Error: // 错误分支：err 非空且级别允许输出。
		if l.Console {
			fmt.Printf(l.traceErrStr+"\n", date, "error", l.Database, id, float64(elapsed.Nanoseconds())/1e6, err, smt)
		}
		l.handleLog(ctx, Error, smt, err, elapsed)

	case elapsed > l.SlowThreshold && l.SlowThreshold != 0 && l.LogLevel >= Warn: // 慢查询分支：耗时超过阈值且级别允许输出。
		slowLog := fmt.Sprintf("SLOW SQL >= %v", l.SlowThreshold)
		if l.Console {
			fmt.Printf(l.traceWarnStr+"\n", date, "warn", l.Database, id, float64(elapsed.Nanoseconds())/1e6, slowLog, smt)
		}
		l.handleLog(ctx, Warn, smt, slowLog, elapsed)

	case l.LogLevel >= Info: // 普通信息分支：级别允许输出。
		if l.Console {
			fmt.Printf(l.traceStr+"\n", date, "info", l.Database, id, float64(elapsed.Nanoseconds())/1e6, smt)
		}
		l.handleLog(ctx, Info, smt, ResultSuccess, elapsed)
	}
}

func (l *logger) handleLog(ctx context.Context, level LogLevel, smt, result string, elapsed time.Duration) {
	if l.handle == nil {
		return
	}

	logMap := map[string]interface{}{
		"Database":  l.Database,             // Database 为库名。
		"Statement": smt,                    // Statement 为命令文本。
		"Result":    result,                 // Result 为 success/slow/error 等结果标记。
		"Duration":  elapsed.Microseconds(), // Duration 为耗时（微秒），便于统计分析。
		"Level":     level,                  // Level 为日志级别枚举值。
		"Type":      LogTypeMongo,           // Type 为日志类型标记。
	}

	md, _ := metadata.FromIncomingContext(ctx)
	if gd := md.Get(TraceId); len(gd) != 0 {
		logMap["trace_id"] = gd[0]
	}
	if gd := md.Get(UserId); len(gd) != 0 {
		logMap["user_id"] = gd[0]
	}
	if gd := md.Get(AppId); len(gd) != 0 {
		logMap["invoke_app_id"] = gd[0]
	}
	if b, err := json.Marshal(logMap); err == nil {
		l.handle(b)
	}
}
