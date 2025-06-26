package logging

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
)

var currentMainGoPackage string

func init() {

	inf, ok := debug.ReadBuildInfo()
	if ok {
		currentMainGoPackage = inf.Main.Path
	}

}

func Domain() string {
	return currentMainGoPackage
}

type CallerURI struct {
	Package  string
	File     string
	Line     int
	Function string
}

func GetCurrentCallerURI() CallerURI {
	ptr, _, _, _ := runtime.Caller(1)
	return getCallerURI(ptr)
}

func GetCurrentCallerURIOffset(offset int) CallerURI {
	ptr, _, _, _ := runtime.Caller(1 + offset)
	return getCallerURI(ptr)
}

func getCallerURI(ptr uintptr) CallerURI {
	frames := runtime.CallersFrames([]uintptr{ptr})
	frame, _ := frames.Next()
	pkg := packageName(frame.Function)
	uri := fmt.Sprintf("%s:%d", frame.File, frame.Line)
	return CallerURI{
		Package:  pkg,
		File:     filepath.Base(filepath.Dir(uri)),
		Line:     frame.Line,
		Function: frame.Function,
	}
}

var pkgCache sync.Map // funcName → packageName

func packageName(funcName string) string {
	if v, ok := pkgCache.Load(funcName); ok {
		return v.(string)
	}
	// one-time compute:
	slash := strings.LastIndex(funcName, "/")
	pkg := ""
	if slash >= 0 {
		rem := funcName[slash+1:]
		if dot := strings.IndexByte(rem, '.'); dot >= 0 {
			pkg = funcName[:slash] + "/" + rem[:dot]
		}
	}
	pkgCache.Store(funcName, pkg)
	return pkg
}

func formatErrorStacks(groups []string, a slog.Attr) slog.Attr {
	if a.Key != "error" {
		return a
	}
	errVal, ok := a.Value.Any().(error)
	if !ok {
		return a
	}

	var frame uintptr
	switch v := errVal.(type) {
	case interface{ Frame() uintptr }:
		frame = v.Frame()
	case interface{ StackTrace() []uintptr }:
		frame = v.StackTrace()[0]
	default:
		frame, _, _, _ = runtime.Caller(2)
	}

	fn := runtime.FuncForPC(frame) // step back to the actual call
	fullName := fn.Name()

	file, line := fn.FileLine(frame)

	pkg := packageName(fullName)

	// funcName = fullName minus "pkg."
	funcName := fullName
	if pkg != "" && strings.HasPrefix(fullName, pkg+".") {
		funcName = fullName[len(pkg)+1:]
	}

	// Build file string: "dirname/basename:line"
	dir := filepath.Base(filepath.Dir(file))
	base := filepath.Base(file)
	// simple concat instead of Sprintf
	fileStr := "'" + dir + "/" + base + ":" + strconv.Itoa(line) + "'"

	a.Value = slog.GroupValue(
		slog.Any("payload", errVal),
		slog.String("func", funcName),
		slog.String("package", pkg),
		slog.String("file", fileStr),
	)
	return a
}
