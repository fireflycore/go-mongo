package internal

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/fireflycore/go-micro/service"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/embedded"
	"go.opentelemetry.io/otel/log/global"
	"google.golang.org/grpc/metadata"
)

type captureLogger struct {
	embedded.Logger
	records []log.Record
}

func (l *captureLogger) Emit(_ context.Context, r log.Record)              { l.records = append(l.records, r) }
func (*captureLogger) Enabled(context.Context, log.EnabledParameters) bool { return true }

type captureProvider struct {
	embedded.LoggerProvider
	logger *captureLogger
}

func (p captureProvider) Logger(string, ...log.LoggerOption) log.Logger { return p.logger }

func TestOperationLogUsesTrustedContext(t *testing.T) {
	previous := global.GetLoggerProvider()
	t.Cleanup(func() { global.SetLoggerProvider(previous) })
	capture := &captureLogger{}
	global.SetLoggerProvider(captureProvider{logger: capture})
	logger := NewLogger(&Conf{Database: "test"})
	// 原始 metadata 不能建立身份；只有框架已校验的上下文可以投影到操作日志。
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("user-id", "forged", "x-firefly-user-id", "forged"))
	logger.Trace(ctx, 1, time.Millisecond, "find items", "")
	ctx = service.WithContext(ctx, &service.Context{
		ServiceAppId: "local", InvokeAppId: "caller",
		UserContext: &service.UserContext{UserId: "trusted", AppId: "login-app", TenantId: "tenant"},
	})
	logger.Trace(ctx, 2, time.Millisecond, "find items", "database error")
	if len(capture.records) != 2 {
		t.Fatalf("records = %d", len(capture.records))
	}
	var anonymous, trusted OperationLogger
	if err := json.Unmarshal([]byte(capture.records[0].Body().AsString()), &anonymous); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(capture.records[1].Body().AsString()), &trusted); err != nil {
		t.Fatal(err)
	}
	if anonymous.UserId != "" || anonymous.AppId != "" || anonymous.TenantId != "" {
		t.Fatalf("raw metadata trusted: %+v", anonymous)
	}
	if trusted.UserId != "trusted" || trusted.AppId != "login-app" || trusted.TenantId != "tenant" || trusted.InvokeAppId != "caller" || trusted.TargetAppId != "local" || trusted.Result != "database error" {
		t.Fatalf("unexpected log: %+v", trusted)
	}
	if capture.records[1].Severity() != log.SeverityError {
		t.Fatal("error severity lost")
	}
}
