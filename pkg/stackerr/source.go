package stackerr

import (
	"errors"
	"os"
	"path/filepath"
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
var executableName string

// ExecutableName returns the current executable name for display purposes
func ExecutableName() string {
	return executableName
}

func init() {

	inf, ok := debug.ReadBuildInfo()
	if ok {
		currentMainGoPackage = inf.Main.Path

		executableName, _ = os.Executable()
		executableName = filepath.Base(executableName)
	}

}

func Domain() string {
	return currentMainGoPackage
}

type EnhancedSource struct {
	ptr                      *uintptr
	RawGoFunc                string `json:"raw_func"`
	RawFilePath              string `json:"raw_file_path"`
	RawFileLine              int    `json:"raw_file_line"`
	FunctionName             string `json:"enhanced_func"`
	ProjectName              string `json:"enhanced_project"`
	FullPackageModulePath    string `json:"enhanced_full_pkg"`
	TrimmedPackageModulePath string `json:"trimmed_package_path"`
}

func (e *EnhancedSource) IsLocalProjectFile() bool {
	return e.ProjectName == currentMainGoPackage
}

func (e *EnhancedSource) UnsafePtr() (uintptr, error) {
	if e.ptr == nil {
		return 0, errors.New("pointer does not exist, this enhanced source was not created in this process (probably json marshalled)")
	}
	return *e.ptr, nil
}

func NewEnhancedSource(pc uintptr) *EnhancedSource {
	frame, _ := runtime.CallersFrames([]uintptr{pc}).Next()

	fullpkg, function := GetPackageAndFuncFromFuncName(frame.Function)

	projectName := GetProjectFromPackage(fullpkg)

	pkgNoProject := strings.TrimPrefix(fullpkg, projectName+"/")

	return &EnhancedSource{
		ptr:                      &pc,
		RawGoFunc:                frame.Function,
		RawFilePath:              frame.File,
		RawFileLine:              frame.Line,
		FunctionName:             function,
		ProjectName:              GetProjectFromPackage(fullpkg),
		FullPackageModulePath:    fullpkg,
		TrimmedPackageModulePath: pkgNoProject,
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

func GetPackageAndFuncFromFuncName(pc string) (string, string) {
	funcName := pc
	lastSlash := strings.LastIndexByte(funcName, '/')
	if lastSlash < 0 {
		lastSlash = 0
	}

	firstDot := strings.IndexByte(funcName[lastSlash:], '.') + lastSlash

	pkg := funcName[:firstDot]
	fname := funcName[firstDot+1:]

	if strings.Contains(pkg, ".(") {
		splt := strings.Split(pkg, ".(")
		pkg = splt[0]
		fname = "(" + splt[1] + "." + fname
	}

	return pkg, fname
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
