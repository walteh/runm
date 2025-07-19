package slogdevterm

import (
	"fmt"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/charmbracelet/lipgloss"
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
	return RenderEnhancedSourceWIthTrim(e, styles, render, hyperlink, false)
}
func RenderEnhancedSourceWIthTrim(e *stackerr.EnhancedSource, styles *Styles, render renderFunc, hyperlink HyperlinkFunc, withTrim bool) string {

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
	filename := render(styles.Caller.File, stackerr.FileNameOfPath(e.RawFilePath))
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
		pkg = render(styles.Caller.Pkg, strings.Join(pkgsplt, "/"))
	}

	var eproj string
	if filepath.Base(e.EnhancedProject) == "main" {
		eproj = " " + render(styles.Caller.MainPkg, stackerr.ExecutableName()) + " "
	} else if e.EnhancedProject == currentMainGoPackage {
		eproj = " "
	} else {
		eproj = " " + render(styles.Caller.Project, filepath.Base(e.EnhancedProject)) + " "
	}

	pkgsep := ""
	if pkg != "" {
		pkgsep = sep
	}

	str := fmt.Sprintf("%s%s%s%s%s%s%s", projIcon, eproj, pkg, pkgsep, filename, sep, num)

	if withTrim {
		noColorString := Strip(str)

		if len(noColorString) > 55 && withTrim && pkg != "" {
			pkgNoAnsi := Strip(pkg)
			diff := len(noColorString) - 55 + 3 // +3 for the "..." we'll add
			if diff >= len(pkgNoAnsi) {
				// If we need to trim more than the entire pkg, just remove pkg entirely
				str = fmt.Sprintf("%s%s%s%s%s", projIcon, eproj, filename, sep, num)
			} else {
				// Trim from the beginning of pkg and add "..."
				trimmedPkg := render(styles.Caller.Pkg, "..."+pkgNoAnsi[diff:])
				str = fmt.Sprintf("%s%s%s%s%s%s%s", projIcon, eproj, trimmedPkg, sep, filename,
					sep, num)
			}
		}
		str = render(lipgloss.NewStyle().Width(55), str)
	}

	return hyperlink("cursor://file/"+e.RawFilePath+":"+fmt.Sprintf("%d", e.RawFileLine), str)
}

const ansi = "[\u001B\u009B][[\\]()#;?]*(?:(?:(?:[a-zA-Z\\d]*(?:;[a-zA-Z\\d]*)*)?\u0007)|(?:(?:\\d{1,4}(?:;\\d{0,4})*)?[\\dA-PRZcf-ntqry=><~]))"

var re = regexp.MustCompile(ansi)

func Strip(str string) string {
	return re.ReplaceAllString(str, "")
}
