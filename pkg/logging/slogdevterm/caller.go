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
	localIcon        = "\ue725 "
	gitFolderIcon    = "\ue702 "
	goIcon           = "\ue65e "
	goXIcon          = "\ue702 "
	goStandardIcon   = "\uf07ef "
	gitlabIcon       = "\ue7eb "
	externalIcon     = "\uf14c "
	executableIcon   = "\ueba6 " // Terminal/executable icon for main packages
)

func RenderEnhancedSource(e *stackerr.EnhancedSource, styles *Styles, render renderFunc, hyperlink HyperlinkFunc) string {
	return RenderEnhancedSourceWIthTrim(e, styles, render, hyperlink, false)
}
func RenderEnhancedSourceWIthTrim(e *stackerr.EnhancedSource, styles *Styles, render renderFunc, hyperlink HyperlinkFunc, withTrim bool) string {

	filename := filepath.Base(e.RawFilePath)
	filenameStyle := styles.Caller.File

	num := fmt.Sprintf("%d", e.RawFileLine)
	numStyle := styles.Caller.Line

	sep := ":"
	sepStyle := styles.Caller.Sep

	icon := getIcon(e)
	iconStyle := styles.Caller.Icon

	pkg := filepath.Base(e.FullPackageModulePath)
	pkgStyle := styles.Caller.Pkg

	var eprojStyle lipgloss.Style
	var eproj string
	if filepath.Base(e.ProjectName) == "main" {
		eprojStyle = styles.Caller.CurrentProjectMainPkg
		eproj = stackerr.ExecutableName()
	} else if e.IsLocalProjectFile() {
		eprojStyle = styles.Caller.CurrentProjectPkg
		eproj = filepath.Base(e.ProjectName)
	} else {
		eprojStyle = styles.Caller.ExternalProjectPackage
		eproj = filepath.Base(e.ProjectName)
	}

	tester := fmt.Sprintf("%s%s%s%s%s%s%s%s", icon, eproj, sep, pkg, sep, filename, sep, num)

	fullStyle := lipgloss.NewStyle()

	if withTrim {
		fullStyle = fullStyle.Width(trimWidth)

		if len(tester) > trimWidth {
			// Components to trim in order of priority (least to most important)
			components := []*string{&pkg, &filename, &eproj, &num}
			trimComponents(components, len(tester), trimWidth)
		}
	}

	finalWithStyle := fmt.Sprintf("%s%s%s%s%s%s%s%s",
		render(iconStyle, icon),
		render(eprojStyle, eproj),
		render(sepStyle, sep),
		render(pkgStyle, pkg),
		render(sepStyle, sep),
		render(filenameStyle, filename),
		render(sepStyle, sep),
		render(numStyle, num))

	return hyperlink("vscode://file/"+e.RawFilePath+":"+fmt.Sprintf("%d", e.RawFileLine), render(fullStyle, finalWithStyle))
}

func getIcon(e *stackerr.EnhancedSource) string {

	if e.IsLocalProjectFile() {
		return localIcon
	}

	splt := strings.Split(e.ProjectName, "/")
	first := splt[0]

	if first == "github.com" {
		return githubIcon
	} else if first == "gitlab.com" {
		return gitlabIcon
	} else if first == "main" {
		return executableIcon
	} else if !strings.Contains(first, ".") {
		return goIcon
	} else {
		return externalIcon
	}

}

const trimWidth = 40

// trimComponents trims string components in priority order to fit within maxLen
func trimComponents(components []*string, totalLen, maxLen int) {
	excess := totalLen - maxLen

	for _, component := range components {
		if excess <= 0 {
			break
		}
		if excess >= len(*component) {
			excess -= len(*component)
			*component = ""
		} else {
			// Keep the first (len - excess) characters and add "~"
			keepChars := len(*component) - excess
			*component = (*component)[:keepChars] + "~"
			excess = 0
		}
	}
}

// trimMiddle trims a string to maxLen by removing characters from the middle
// and replacing them with "...". Returns original string if no trimming needed.
func trimMiddle(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	if maxLen <= 3 {
		return "..."[:maxLen]
	}

	// Calculate how many chars to keep (excluding the 3 dots)
	keepChars := maxLen - 3
	leftChars := keepChars / 2
	rightChars := keepChars - leftChars

	return s[:leftChars] + "..." + s[len(s)-rightChars:]
}

const ansi = "[\u001B\u009B][[\\]()#;?]*(?:(?:(?:[a-zA-Z\\d]*(?:;[a-zA-Z\\d]*)*)?\u0007)|(?:(?:\\d{1,4}(?:;\\d{0,4})*)?[\\dA-PRZcf-ntqry=><~]))"

var re = regexp.MustCompile(ansi)

func Strip(str string) string {
	return re.ReplaceAllString(str, "")
}
