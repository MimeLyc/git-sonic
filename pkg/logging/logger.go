// Package logging provides structured logging with workflow step tracking.
package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"
)

// contextKey is used for storing logger in context.
type contextKey struct{}

// Logger wraps slog.Logger with workflow-specific functionality.
type Logger struct {
	*slog.Logger
	workflow  string
	startTime time.Time
	stepNum   int
}

// WorkflowError represents an error that occurred during a workflow step.
type WorkflowError struct {
	Workflow string
	Step     string
	StepNum  int
	Op       string
	Err      error
	Stack    string
}

func (e *WorkflowError) Error() string {
	if e.Op != "" {
		return fmt.Sprintf("[%s] step %d (%s) %s: %v", e.Workflow, e.StepNum, e.Step, e.Op, e.Err)
	}
	return fmt.Sprintf("[%s] step %d (%s): %v", e.Workflow, e.StepNum, e.Step, e.Err)
}

func (e *WorkflowError) Unwrap() error {
	return e.Err
}

// Format implements fmt.Formatter for detailed error output.
func (e *WorkflowError) Format(f fmt.State, verb rune) {
	switch verb {
	case 'v':
		if f.Flag('+') {
			fmt.Fprintf(f, "%s\n\nStack trace:\n%s", e.Error(), e.Stack)
			return
		}
		fallthrough
	default:
		fmt.Fprint(f, e.Error())
	}
}

// New creates a new Logger with the specified output format.
func New(jsonFormat bool) *Logger {
	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{Key: "ts", Value: slog.StringValue(a.Value.Time().Format("2006-01-02 15:04:05.000"))}
			}
			return a
		},
	}
	if jsonFormat {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return &Logger{
		Logger: slog.New(handler),
	}
}

// Default returns the default logger.
func Default() *Logger {
	return New(false)
}

// With returns a new Logger with the given attributes.
func (l *Logger) With(args ...any) *Logger {
	return &Logger{
		Logger:    l.Logger.With(args...),
		workflow:  l.workflow,
		startTime: l.startTime,
		stepNum:   l.stepNum,
	}
}

// WithContext returns a new context with the logger attached.
func (l *Logger) WithContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, contextKey{}, l)
}

// FromContext retrieves the logger from context, or returns default logger.
func FromContext(ctx context.Context) *Logger {
	if l, ok := ctx.Value(contextKey{}).(*Logger); ok {
		return l
	}
	return Default()
}

// StartWorkflow creates a new logger for a workflow execution.
func (l *Logger) StartWorkflow(workflowName string, attrs ...any) *Logger {
	newLogger := &Logger{
		Logger:    l.Logger.With(append([]any{"workflow", workflowName}, attrs...)...),
		workflow:  workflowName,
		startTime: time.Now(),
		stepNum:   0,
	}
	newLogger.Info("workflow started")
	return newLogger
}

// Step logs a workflow step and returns a function to log step completion.
func (l *Logger) Step(stepName string, attrs ...any) func(error) {
	l.stepNum++
	stepStart := time.Now()
	stepLogger := l.With(append([]any{"step", stepName, "step_num", l.stepNum}, attrs...)...)
	stepLogger.Info("step started")

	return func(err error) {
		elapsed := time.Since(stepStart)
		if err != nil {
			stepLogger.Error("step failed",
				"error", err.Error(),
				"elapsed_ms", elapsed.Milliseconds(),
			)
		} else {
			stepLogger.Info("step completed",
				"elapsed_ms", elapsed.Milliseconds(),
			)
		}
	}
}

// StepInfo logs a step with additional info message.
func (l *Logger) StepInfo(stepName string, msg string, attrs ...any) {
	l.stepNum++
	l.With(append([]any{"step", stepName, "step_num", l.stepNum}, attrs...)...).Info(msg)
}

// EndWorkflow logs workflow completion.
func (l *Logger) EndWorkflow(err error) {
	elapsed := time.Since(l.startTime)
	if err != nil {
		l.Error("workflow failed",
			"error", err.Error(),
			"elapsed_ms", elapsed.Milliseconds(),
			"total_steps", l.stepNum,
		)
	} else {
		l.Info("workflow completed",
			"elapsed_ms", elapsed.Milliseconds(),
			"total_steps", l.stepNum,
		)
	}
}

// WrapError wraps an error with workflow context and stack trace.
func (l *Logger) WrapError(step, op string, err error) error {
	if err == nil {
		return nil
	}
	return &WorkflowError{
		Workflow: l.workflow,
		Step:     step,
		StepNum:  l.stepNum,
		Op:       op,
		Err:      err,
		Stack:    captureStack(2),
	}
}

// captureStack captures the current stack trace, skipping the specified number of frames.
func captureStack(skip int) string {
	var pcs [32]uintptr
	n := runtime.Callers(skip+1, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])

	var sb strings.Builder
	for {
		frame, more := frames.Next()
		// Skip runtime and testing frames
		if strings.Contains(frame.File, "runtime/") {
			if !more {
				break
			}
			continue
		}
		fmt.Fprintf(&sb, "  %s\n    %s:%d\n", frame.Function, frame.File, frame.Line)
		if !more {
			break
		}
	}
	return sb.String()
}

// Attrs is a helper to create attribute slices.
func Attrs(keyValues ...any) []any {
	return keyValues
}
