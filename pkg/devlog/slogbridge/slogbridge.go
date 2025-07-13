package slogbridge

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/walteh/runm/pkg/devlog"
	"github.com/walteh/runm/pkg/stackerr"

	devlogv1 "github.com/walteh/runm/proto/devlog/v1"
)

// SlogProducer implements devlog.Producer for slog.Record
type SlogProducer struct {
	consumer    devlog.Consumer
	loggerName  string
	processInfo *devlogv1.ProcessInfo
	groups      []string
	attrs       []slog.Attr
}

// NewSlogProducer creates a new SlogProducer
func NewSlogProducer(consumer devlog.Consumer, loggerName string) *SlogProducer {
	return &SlogProducer{
		consumer:    consumer,
		loggerName:  loggerName,
		processInfo: getProcessInfo(),
		groups:      []string{},
		attrs:       []slog.Attr{},
	}
}

// Produce converts slog.Record to devlog.Entry
func (p *SlogProducer) Produce(ctx context.Context, data interface{}) (*devlog.Entry, error) {
	record, ok := data.(slog.Record)
	if !ok {
		return nil, devlog.ErrUnsupportedSourceType
	}

	// Create structured log entry
	structured := &devlogv1.StructuredLog{
		Timestamp:  timestamppb.New(record.Time),
		Level:      devlog.SlogLevelToDevlogLevel(record.Level),
		Message:    record.Message,
		LoggerName: p.loggerName,
		Process:    p.processInfo,
		Attributes: p.convertAttributes(record),
		TraceId:    extractTraceID(ctx),
		SpanId:     extractSpanID(ctx),
		Labels:     make(map[string]string),
	}

	// Add source information if available
	if record.PC != 0 {
		source := stackerr.NewEnhancedSource(record.PC)
		structured.Source = &devlogv1.SourceInfo{
			FilePath:       source.RawFilePath,
			LineNumber:     int32(source.RawFileLine),
			FunctionName:   source.RawFunc,
			PackageName:    source.EnhancedPkg,
			ModuleName:     getModuleName(source.RawFilePath),
			ProgramCounter: uint64(record.PC),
		}
	}

	// Handle errors specially
	if errorInfo := p.extractErrorInfo(record); errorInfo != nil {
		structured.Error = errorInfo
	}

	entry := &devlog.Entry{
		Type:       devlog.EntryTypeStructured,
		Structured: structured,
	}

	return entry, nil
}

// Close shuts down the producer
func (p *SlogProducer) Close() error {
	return nil
}

// convertAttributes converts slog attributes to devlog attributes
func (p *SlogProducer) convertAttributes(record slog.Record) []*devlogv1.Attribute {
	var attributes []*devlogv1.Attribute

	// Add preformatted attributes from WithAttrs
	for _, attr := range p.attrs {
		if devlogAttr := p.convertAttribute(attr); devlogAttr != nil {
			attributes = append(attributes, devlogAttr)
		}
	}

	// Add record attributes
	record.Attrs(func(attr slog.Attr) bool {
		if devlogAttr := p.convertAttribute(attr); devlogAttr != nil {
			attributes = append(attributes, devlogAttr)
		}
		return true
	})

	return attributes
}

// convertAttribute converts a single slog.Attr to devlog.Attribute
func (p *SlogProducer) convertAttribute(attr slog.Attr) *devlogv1.Attribute {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return nil
	}

	// Build dotted key with groups
	key := p.buildDottedKey(attr.Key)

	value := p.convertAttributeValue(attr.Value)
	if value == nil {
		return nil
	}

	return &devlogv1.Attribute{
		Key:   key,
		Value: value,
	}
}

// convertAttributeValue converts slog.Value to devlog.AttributeValue
func (p *SlogProducer) convertAttributeValue(value slog.Value) *devlogv1.AttributeValue {
	switch value.Kind() {
	case slog.KindString:
		return &devlogv1.AttributeValue{
			Value: &devlogv1.AttributeValue_StringValue{
				StringValue: value.String(),
			},
		}
	case slog.KindInt64:
		return &devlogv1.AttributeValue{
			Value: &devlogv1.AttributeValue_IntValue{
				IntValue: value.Int64(),
			},
		}
	case slog.KindUint64:
		return &devlogv1.AttributeValue{
			Value: &devlogv1.AttributeValue_IntValue{
				IntValue: int64(value.Uint64()),
			},
		}
	case slog.KindFloat64:
		return &devlogv1.AttributeValue{
			Value: &devlogv1.AttributeValue_DoubleValue{
				DoubleValue: value.Float64(),
			},
		}
	case slog.KindBool:
		return &devlogv1.AttributeValue{
			Value: &devlogv1.AttributeValue_BoolValue{
				BoolValue: value.Bool(),
			},
		}
	case slog.KindTime:
		return &devlogv1.AttributeValue{
			Value: &devlogv1.AttributeValue_StringValue{
				StringValue: value.Time().Format(time.RFC3339Nano),
			},
		}
	case slog.KindGroup:
		// Handle groups by flattening them
		groupAttrs := value.Group()
		if len(groupAttrs) == 0 {
			return nil
		}
		// For now, convert to a string representation
		// TODO: Better group handling
		return &devlogv1.AttributeValue{
			Value: &devlogv1.AttributeValue_StringValue{
				StringValue: "[group]",
			},
		}
	default:
		// Fallback to string representation
		return &devlogv1.AttributeValue{
			Value: &devlogv1.AttributeValue_StringValue{
				StringValue: value.String(),
			},
		}
	}
}

// extractErrorInfo extracts error information from the log record
func (p *SlogProducer) extractErrorInfo(record slog.Record) *devlogv1.ErrorInfo {
	var foundError error

	// Look for error in attributes
	record.Attrs(func(attr slog.Attr) bool {
		if isErrorKey(attr.Key) {
			if err, ok := attr.Value.Any().(error); ok {
				foundError = err
				return false // Stop iteration
			}
		}
		return true
	})

	if foundError == nil {
		return nil
	}

	errorInfo := &devlogv1.ErrorInfo{
		Message: foundError.Error(),
		Type:    getErrorType(foundError),
	}

	// Add stack trace if available
	if stackErr, ok := foundError.(interface{ Stack() []byte }); ok {
		errorInfo.StackTrace = string(stackErr.Stack())
	}

	return errorInfo
}

// buildDottedKey creates a dotted key from groups and the attribute key
func (p *SlogProducer) buildDottedKey(attrKey string) string {
	if len(p.groups) == 0 {
		return attrKey
	}

	groupPrefix := ""
	for i, group := range p.groups {
		if i > 0 {
			groupPrefix += "."
		}
		groupPrefix += group
	}

	if attrKey == "" {
		return groupPrefix
	}
	return groupPrefix + "." + attrKey
}

// Helper functions
func getProcessInfo() *devlogv1.ProcessInfo {
	return &devlogv1.ProcessInfo{
		Pid:      int32(os.Getpid()),
		Hostname: getHostname(),
		Runtime:  "go",
		Os:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Version:  runtime.Version(),
	}
}

func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

func getModuleName(filePath string) string {
	// TODO: Extract module name from file path
	return "github.com/walteh/runm"
}

func getErrorType(err error) string {
	// Use reflection to get the concrete type
	return err.Error() // Simplified for now
}

func isErrorKey(key string) bool {
	return key == "error" || key == "err" || key == "error.payload"
}

func extractTraceID(ctx context.Context) string {
	// TODO: Extract trace ID from context (OpenTelemetry, etc.)
	return ""
}

func extractSpanID(ctx context.Context) string {
	// TODO: Extract span ID from context (OpenTelemetry, etc.)
	return ""
}
