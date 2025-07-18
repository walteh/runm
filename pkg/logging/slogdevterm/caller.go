package slogdevterm

import (
	"fmt"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/walteh/runm/pkg/stackerr"
)

var knownNestedProjects = []string{
	"github.com/moby/sys",
	"gitlab.com/tozd/go",
}

var currentMainGoPackage string

func init() {
	// zerolog.TimeFieldFormat = time.RFC3339Nano
	// zerolog.CallerMarshalFunc = ZeroLogCallerMarshalFunc

	inf, ok := debug.ReadBuildInfo()
	if ok {
		currentMainGoPackage = inf.Main.Path
		// for _, dep := range inf.Deps {
		// 	pkgMap[dep.Path] = dep
		// }
	}

}

const (
	githubIcon       = "\ue709 "
	githubIconSquare = "\uf092 "
	gitIcon          = "\ue725 "
	gitFolderIcon    = "\ue702 "
	goIcon           = "\ue65e "
	goXIcon          = "\ue702 "
	goStandardIcon   = "\uf07ef "
	gitlabIcon       = "\ue7eb "
	externalIcon     = "\uf14c "
	executableIcon   = "\uf489 " // Terminal/executable icon for main packages
)

func RenderEnhancedSource(e *stackerr.EnhancedSource, styles *Styles, render renderFunc, hyperlink HyperlinkFunc) string {

	pkgNoProject := strings.TrimPrefix(e.EnhancedFullPkg, e.EnhancedProject+"/")
	if e.EnhancedProject == e.EnhancedFullPkg {
		pkgNoProject = ""
	}

	var isCurrentMainGoPackage bool

	if e.EnhancedProject == currentMainGoPackage {
		isCurrentMainGoPackage = true
	}

	var projIcon string

	// pkg = filepath.Base(pkg)
	filePath := render(styles.Caller.File, stackerr.FileNameOfPath(e.RawFilePath))
	num := render(styles.Caller.Line, fmt.Sprintf("%d", e.RawFileLine))
	sep := render(styles.Caller.Sep, ":")

	if !isCurrentMainGoPackage {
		splt := strings.Split(e.EnhancedProject, "/")
		first := splt[0]
		// var lasts []string
		// if len(splt) > 1 {
		// 	lasts = strings.Split(e.enhancedProject, "/")[1:]
		// }

		if first == "github.com" {
			projIcon = githubIcon
		} else if first == "gitlab.com" {
			projIcon = gitlabIcon
		} else if first == "main" {
			projIcon = executableIcon
		} else if !strings.Contains(first, ".") {
			projIcon = goIcon
		} else {
			projIcon = externalIcon
		}
	} else {
		projIcon = gitIcon
	}

	pkgsplt := strings.Split(pkgNoProject, "/")
	// last := pkgsplt[len(pkgsplt)-1]

	var pkg string
	if pkgNoProject == "" || filepath.Base(e.EnhancedProject) == "main" {
		pkg = ""
	} else {
		// For main executables, don't show the package name at all (it's shown in eproj)
		pkg = render(styles.Caller.Pkg, strings.Join(pkgsplt, "/")) + sep
	}

	// var pkg string
	// if len(pkgsplt) > 1 {
	// 	pkg = styles.Caller.Pkg.Render(strings.Join(pkgsplt[:len(pkgsplt)-2], "/") + "/")
	// 	pkg += styles.Caller.Pkg.Bold(true).Render(last)
	// } else {
	// 	pkg = styles.Caller.Pkg.Render(last)
	// }

	// [icon] [package]

	var eproj string
	if filepath.Base(e.EnhancedProject) == "main" {
		eproj = " " + render(styles.Caller.MainPkg, stackerr.ExecutableName()) + " "
	} else if e.EnhancedProject == currentMainGoPackage {
		eproj = " "
	} else {
		eproj = " " + render(styles.Caller.Project, filepath.Base(e.EnhancedProject)) + " "
	}

	return hyperlink("cursor://file/"+e.RawFilePath+":"+fmt.Sprintf("%d", e.RawFileLine), fmt.Sprintf("%s%s%s%s%s%s", projIcon, eproj, pkg, filePath, sep, num))
}
