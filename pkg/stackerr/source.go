package stackerr

import (
	"errors"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
)

var knownNestedProjects = []string{
	"github.com/moby/sys",
	"gitlab.com/tozd/go",
}

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

type EnhancedSource struct {
	ptr             *uintptr
	RawFunc         string `json:"raw_func"`
	RawFilePath     string `json:"raw_file_path"`
	RawFileLine     int    `json:"raw_file_line"`
	EnhancedFunc    string `json:"enhanced_func"`
	EnhancedPkg     string `json:"enhanced_pkg"`
	EnhancedProject string `json:"enhanced_project"`
	EnhancedFullPkg string `json:"enhanced_full_pkg"`
}

func (e *EnhancedSource) UnsafePtr() (uintptr, error) {
	if e.ptr == nil {
		return 0, errors.New("pointer does not exist, this enhanced source was not created in this process (probably json marshalled)")
	}
	return *e.ptr, nil
}

func NewEnhancedSource(pc uintptr) *EnhancedSource {
	frame, _ := runtime.CallersFrames([]uintptr{pc}).Next()

	fullpkg, pkg, function := GetPackageAndFuncFromFuncName(frame.Function)

	return &EnhancedSource{
		ptr:             &pc,
		RawFunc:         frame.Function,
		RawFilePath:     frame.File,
		RawFileLine:     frame.Line,
		EnhancedFunc:    function,
		EnhancedPkg:     pkg,
		EnhancedProject: GetProjectFromPackage(fullpkg),
		EnhancedFullPkg: fullpkg,
	}
}

func GetProjectFromPackage(pkg string) string {
	slashes := 3
	for _, project := range knownNestedProjects {
		if strings.HasPrefix(pkg, project) {
			slashes = strings.Count(pkg, "/") + 1
			break
		}
	}

	// if at least 3 slashes, return the first 2
	splt := strings.Split(pkg, "/")
	if len(splt) >= slashes {
		return strings.Join(splt[:slashes], "/")
	}

	return pkg
}

func GetPackageAndFuncFromFuncName(pc string) (fullpkg, pkg, function string) {
	funcName := pc
	lastSlash := strings.LastIndexByte(funcName, '/')
	if lastSlash < 0 {
		lastSlash = 0
	}

	firstDot := strings.IndexByte(funcName[lastSlash:], '.') + lastSlash

	pkg = funcName[:firstDot]
	fname := funcName[firstDot+1:]

	if strings.Contains(pkg, ".(") {
		splt := strings.Split(pkg, ".(")
		pkg = splt[0]
		fname = "(" + splt[1] + "." + fname
	}

	fullpkg = pkg
	pkg = strings.TrimPrefix(pkg, currentMainGoPackage+"/")

	return fullpkg, pkg, fname
}

func FileNameOfPath(path string) string {
	tot := strings.Split(path, "/")
	if len(tot) > 1 {
		return tot[len(tot)-1]
	}

	return path
}

func GetEnhancedSourcesFromError(err error) []*EnhancedSource {
	if err == nil {
		return nil
	}
	var sources []*EnhancedSource

	st, isStackTracer := err.(interface {
		StackTrace() []uintptr
	})
	if isStackTracer {
		frames := slices.DeleteFunc(ConvertStackTraceToFrames(st.StackTrace()), IsNotRelevantFrame)
		if len(frames) == 0 {
			return nil
		}

		for _, frame := range frames {
			sources = append(sources, NewEnhancedSource(frame.PC))
		}

		return sources
	}

	se, isStackedEncodableError := err.(*StackedEncodableError)
	if isStackedEncodableError {
		sources = append(sources, se.Source)
		for se.Next != nil {
			sources = append(sources, se.Next.Source)
			se = se.Next
		}
	}

	return sources

}
