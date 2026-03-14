package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fireflycore/go-micro/constant"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/metadata"
)

// ANSI Color Constants
const (
	ColorReset    = "\033[0m"
	ColorYellow   = "\033[33m"
	ColorBlueBold = "\033[34;1m"
	ColorRedBold  = "\033[31;1m"
	ColorGreen    = "\033[32m"
)

// Log Constants
const (
	// LogTypeMongo 为日志类型标识（供下游聚合检索）。
	LogTypeMongo = 6
	// ResultSuccess 为成功执行的结果标记。
	ResultSuccess = "success"
)

// LogLevel 定义日志级别枚举。
type LogLevel uint32

const (
	Info  LogLevel = 1 // Info 普通级别。
	Warn  LogLevel = 2 // Warn 警告级别。
	Error LogLevel = 3 // Error 错误级别。
)

// OperationLogger 表示操作日志。
type OperationLogger struct {
	Database  string `json:"database"`
	Statement string `json:"statement"`
	Result    string `json:"result"`
	Path      string `json:"path"`

	Duration uint64 `json:"duration"`

	Level uint32 `json:"level"`
	Type  uint32 `json:"type"`

	TraceId  string `json:"trace_id"`
	ParentId string `json:"parent_id"`

	TargetAppId string `json:"target_app_id"`
	InvokeAppId string `json:"invoke_app_id"`

	UserId   string `json:"user_id"`
	AppId    string `json:"app_id"`
	TenantId string `json:"tenant_id"`
}

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
}

// Interface 约束 logger 需要提供的能力。
type Interface interface {
	// Trace 记录一次命令的执行信息。
	Trace(ctx context.Context, id int64, elapsed time.Duration, smt string, err string)
}

type logger struct {
	Conf                // Conf 嵌入，复用配置字段。
	traceStr     string // traceStr 为普通 trace 模板。
	traceWarnStr string // traceWarnStr 为慢查询模板。
	traceErrStr  string // traceErrStr 为错误模板。
}

// NewLogger 构造一个新的 logger，并按配置决定输出模板。
func NewLogger(conf *Conf) Interface {
	// baseFormat 为默认输出模板。
	// Info: date, level, db, id, timer, file, smt
	traceStr := "[%s] [%s] [Database:%s] [RequestId:%d] [Duration:%.3fms] [Path:%s]\n%s"
	// Warn: date, level, db, id, timer, file, slowLog, smt
	traceWarnStr := "[%s] [%s] [Database:%s] [RequestId:%d] [Duration:%.3fms] [Path:%s]\n%s\n%s"
	// Error: date, level, db, id, timer, file, err, smt
	traceErrStr := "[%s] [%s] [Database:%s] [RequestId:%d] [Duration:%.3fms] [Path:%s]\n%s\n%s"

	// 彩色输出时替换模板为 ANSI 颜色版本。
	if conf.Colorful {
		// colorPrefix 为彩色前缀模板。
		colorPrefix := "[%s] [%s] " + ColorBlueBold + "[Database:%s] " + ColorBlueBold + "[RequestId:%d] " + ColorYellow + "[Duration:%.3fms] " + ColorGreen + "[Path:%s]\n"
		// 普通日志模板。
		traceStr = colorPrefix + ColorReset + "%s"
		// 慢查询模板。
		traceWarnStr = colorPrefix + ColorYellow + "%s\n" + ColorReset + "%s"
		// 错误模板。
		traceErrStr = colorPrefix + ColorRedBold + "%s\n" + ColorReset + "%s"
	}

	return &logger{
		Conf:         *conf,
		traceStr:     traceStr,
		traceWarnStr: traceWarnStr,
		traceErrStr:  traceErrStr,
	}
}

func (l *logger) Trace(ctx context.Context, id int64, elapsed time.Duration, smt string, err string) {

	date := time.Now().Format(time.DateTime)
	file := fileWithLineNum()
	timer := float64(elapsed.Nanoseconds()) / 1e6

	// 按错误/慢查询/普通信息分支记录日志。
	// 注意：此处不再使用 LogLevel 进行过滤，而是根据执行结果自动标记级别（全链路收集）。
	switch {
	case len(err) > 0: // 错误分支：err 非空。
		if l.Console {
			fmt.Printf(l.traceErrStr+"\n", date, "error", l.Database, id, timer, file, err, smt)
		}
		l.handleLog(ctx, Error, file, smt, err, elapsed)

	case elapsed > l.SlowThreshold && l.SlowThreshold != 0: // 慢查询分支：耗时超过阈值。
		slowLog := fmt.Sprintf("SLOW SQL >= %v", l.SlowThreshold)
		if l.Console {
			fmt.Printf(l.traceWarnStr+"\n", date, "warn", l.Database, id, timer, file, slowLog, smt)
		}
		l.handleLog(ctx, Warn, file, smt, slowLog, elapsed)

	default: // 普通信息分支。
		if l.Console {
			fmt.Printf(l.traceStr+"\n", date, "info", l.Database, id, timer, file, smt)
		}
		l.handleLog(ctx, Info, file, smt, ResultSuccess, elapsed)
	}
}

func (l *logger) handleLog(ctx context.Context, level LogLevel, path, smt, result string, elapsed time.Duration) {
	logData := &OperationLogger{
		Database:  l.Database,                     // Database 为库名。
		Statement: smt,                            // Statement 为命令文本。
		Result:    result,                         // Result 为 success/slow/error 等结果标记。
		Duration:  uint64(elapsed.Microseconds()), // Duration 为耗时（微秒），便于统计分析。
		Level:     uint32(level),                  // Level 为日志级别枚举值。
		Path:      path,                           // Path 为调用位置。
		Type:      LogTypeMongo,                   // Type 为日志类型标记。
	}

	// 从 OTel span context 中提取链路字段（优先）
	spanCtx := trace.SpanFromContext(ctx).SpanContext()
	if spanCtx.IsValid() {
		logData.TraceId = spanCtx.TraceID().String()
		logData.ParentId = spanCtx.SpanID().String()
	}

	// 从 gRPC metadata 中提取链路字段（存在则写入结构化日志，作为兼容兜底）
	md, _ := metadata.FromIncomingContext(ctx)
	if gd := md.Get(constant.UserId); len(gd) != 0 {
		logData.UserId = gd[0]
	}
	if gd := md.Get(constant.AppId); len(gd) != 0 {
		logData.AppId = gd[0]
	}
	if gd := md.Get(constant.InvokeServiceAppId); len(gd) != 0 {
		logData.InvokeAppId = gd[0]
	}
	if gd := md.Get(constant.TargetServiceAppId); len(gd) != 0 {
		logData.TargetAppId = gd[0]
	}
	if gd := md.Get(constant.TenantId); len(gd) != 0 {
		logData.TenantId = gd[0]
	}

	l.emitOTelOperationLog(ctx, level, logData)
}

func (l *logger) emitOTelOperationLog(ctx context.Context, level LogLevel, logData *OperationLogger) {
	if logData == nil {
		return
	}

	otelLogger := global.Logger("go-mongo")

	var record log.Record
	record.SetTimestamp(time.Now())
	record.SetSeverity(convertOTelSeverity(level))
	record.SetSeverityText(convertOTelSeverityText(level))

	if b, err := json.Marshal(logData); err == nil {
		record.SetBody(log.StringValue(string(b)))
	} else {
		record.SetBody(log.StringValue(logData.Statement))
	}

	record.AddAttributes(
		log.String("log_type", "operation"),
		log.String("database", logData.Database),
		log.String("statement", logData.Statement),
		log.String("result", logData.Result),
		log.String("path", logData.Path),
		log.Int64("duration", int64(logData.Duration)),
		log.Int64("db_type", int64(logData.Type)),
	)
	if logData.UserId != "" {
		record.AddAttributes(log.String("user_id", logData.UserId))
	}
	if logData.AppId != "" {
		record.AddAttributes(log.String("app_id", logData.AppId))
	}
	if logData.TenantId != "" {
		record.AddAttributes(log.String("tenant_id", logData.TenantId))
	}

	otelLogger.Emit(ctx, record)
}

func convertOTelSeverity(level LogLevel) log.Severity {
	switch level {
	case Error:
		return log.SeverityError
	case Warn:
		return log.SeverityWarn
	case Info:
		return log.SeverityInfo
	default:
		return log.SeverityUndefined
	}
}

func convertOTelSeverityText(level LogLevel) string {
	switch level {
	case Error:
		return "ERROR"
	case Warn:
		return "WARN"
	case Info:
		return "INFO"
	default:
		return ""
	}
}

// filterCache 用于缓存 PC 对应的过滤结果（避免重复的 FuncForPC 和字符串匹配）。
// Key: uintptr (pc), Value: bool (true=保留/返回, false=跳过)
var filterCache sync.Map

func fileWithLineNum() string {
	for i := 2; i < 15; i++ {
		pc, file, line, ok := runtime.Caller(i)
		if !ok {
			continue
		}

		// 1. 查缓存：如果已判定过该 PC，直接使用结果
		if val, hit := filterCache.Load(pc); hit {
			if val.(bool) {
				return file + ":" + strconv.FormatInt(int64(line), 10)
			}
			continue
		}

		// 2. 慢路径：执行完整判定逻辑
		keep := false

		// 规则A: 优先保留测试文件
		if strings.HasSuffix(file, "_test.go") {
			keep = true
		} else if strings.Contains(file, "go.mongodb.org/mongo-driver") {
			// 规则B: 过滤 mongo-driver
			keep = false
		} else {
			// 规则C: 过滤 go-mongo 库自身
			if fn := runtime.FuncForPC(pc); fn != nil {
				if strings.Contains(fn.Name(), "github.com/fireflycore/go-mongo") {
					keep = false
				} else {
					keep = true // 非库内部代码，保留
				}
			} else {
				keep = true // 无法获取函数名，默认保留
			}
		}

		// 写入缓存
		filterCache.Store(pc, keep)

		if keep {
			return file + ":" + strconv.FormatInt(int64(line), 10)
		}
	}
	return ""
}
