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

	// HeaderPrefix Firefly系统自定义头部（统一前缀）
	HeaderPrefix = "x-firefly-"
	// TraceId 为从 metadata 读取 trace id 的 key。
	TraceId = HeaderPrefix + "trace-id"
	// UserId 为从 metadata 读取 user id 的 key。
	UserId = HeaderPrefix + "user-id"
	// AppId 为从 metadata 读取 app id 的 key。
	AppId = HeaderPrefix + "app-id"
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

	TraceId     string `json:"trace_id"`
	UserId      string `json:"user_id"`
	TargetAppId string `json:"target_app_id"`
	InvokeAppId string `json:"invoke_app_id"`
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
	Conf                      // Conf 嵌入，复用配置字段。
	traceStr     string       // traceStr 为普通 trace 模板。
	traceWarnStr string       // traceWarnStr 为慢查询模板。
	traceErrStr  string       // traceErrStr 为错误模板。
	handle       func([]byte) // handle 为结构化日志回调（可为空）。
}

// NewLogger 构造一个新的 logger，并按配置决定输出模板。
func NewLogger(conf *Conf, handle func([]byte)) Interface {
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
		handle:       handle,
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
	if l.handle == nil {
		return
	}

	log := &OperationLogger{
		Database:  l.Database,                     // Database 为库名。
		Statement: smt,                            // Statement 为命令文本。
		Result:    result,                         // Result 为 success/slow/error 等结果标记。
		Duration:  uint64(elapsed.Microseconds()), // Duration 为耗时（微秒），便于统计分析。
		Level:     uint32(level),                  // Level 为日志级别枚举值。
		Path:      path,                           // Path 为调用位置。
		Type:      LogTypeMongo,                   // Type 为日志类型标记。
	}

	md, _ := metadata.FromIncomingContext(ctx)
	if gd := md.Get(TraceId); len(gd) != 0 {
		log.TraceId = gd[0]
	}
	if gd := md.Get(UserId); len(gd) != 0 {
		log.UserId = gd[0]
	}
	if gd := md.Get(AppId); len(gd) != 0 {
		log.InvokeAppId = gd[0]
	}
	if b, err := json.Marshal(log); err == nil {
		l.handle(b)
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
