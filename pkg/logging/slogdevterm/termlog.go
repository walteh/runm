package slogdevterm

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"os"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/walteh/runm/pkg/stackerr"
)

var _ slog.Handler = (*TermLogger)(nil)

// Pattern for special debug log formatting: WORD:WORD[content]
func ParseSegments(s string) ([]string, bool) {
	// Find the first "[" and the last "]"
	open := strings.IndexByte(s, '[')
	close := strings.LastIndexByte(s, ']')
	if open < 0 || close < 0 || close <= open {
		return nil, false
	}

	// Extract prefix and suffix
	prefix := s[:open]
	suffix := s[open+1 : close]

	// Split the prefix on ":" (this will yield all m1…mN)
	parts := strings.Split(prefix, ":")

	// Append the bracketed part as the final element
	parts = append(parts, suffix)
	return parts, true
}

type TermLoggerOption = func(*TermLogger)

func WithStyles(styles *Styles) TermLoggerOption {
	return func(l *TermLogger) {
		l.styles = styles
	}
}

func WithLoggerName(name string) TermLoggerOption {
	return func(l *TermLogger) {
		l.name = name
	}
}

func WithProfile(profile termenv.Profile) TermLoggerOption {
	return func(l *TermLogger) {
		l.renderOpts = append(l.renderOpts, termenv.WithProfile(profile))
	}
}

func WithRenderOption(opt termenv.OutputOption) TermLoggerOption {
	return func(l *TermLogger) {
		l.renderOpts = append(l.renderOpts, opt)
	}
}

func WithHyperlinkFunc(fn func(link, renderedText string) string) TermLoggerOption {
	return func(l *TermLogger) {
		l.hyperlinkFunc = fn
	}
}

func WithOSIcon(enabled bool) TermLoggerOption {
	return func(l *TermLogger) {
		l.showOSIcon = enabled
	}
}

func WithLoggerNameColor(color lipgloss.Color) TermLoggerOption {
	return func(l *TermLogger) {
		l.nameColor = color
	}
}

func WithEnableLoggerNameColor(enabled bool) TermLoggerOption {
	return func(l *TermLogger) {
		l.enableNameColors = enabled
	}
}

func WithDebugPatternColoring(enabled bool) TermLoggerOption {
	return func(l *TermLogger) {
		l.enableDebugPatternColoring = enabled
	}
}

func WithMultilineBoxes(enabled bool) TermLoggerOption {
	return func(l *TermLogger) {
		l.enableMultilineBoxes = enabled
	}
}

func newColoredString(s string, color string) lipgloss.Style {
	return lipgloss.NewStyle().SetString(s).Foreground(lipgloss.Color(color)).Bold(true)
}

// simple linux =  \U000F033D"
// OSIconMap maps OS identifiers to unicode icons \udb80\udf3d
var OSIconMap = map[string]lipgloss.Style{
	"darwin":  newColoredString("\uf302", "#FFD700"), // light green Apple logo for macOS
	"linux":   newColoredString("\ue712", "#FF9E80"), // orange Penguin for Linux
	"windows": newColoredString("\ue70f", "#40DFFF"), // blue Window for Windows
	"freebsd": newColoredString("\uf30c", "white"),   // green BSD Daemon for FreeBSD

	"openbsd": newColoredString("\uf328", "white"),  // purple Pufferfish for OpenBSD
	"android": newColoredString("\ue70e", "white"),  // Robot for Android
	"ios":     newColoredString("\uf0037", "white"), // Mobile phone for iOS
	"js":      newColoredString("\uf2ef", "white"),  // Web for JavaScript runtime
}

// GetOSIcon returns the appropriate icon for the current OS
func GetOSIcon() lipgloss.Style {
	icon, exists := OSIconMap[runtime.GOOS]
	if !exists {
		return newColoredString("\uf059", "white") // question mark for unknown OS
	}
	return icon
}

type HyperlinkFunc func(link, renderedText string) string

type TermLogger struct {
	slogOptions                *slog.HandlerOptions
	styles                     *Styles
	writer                     io.Writer
	renderOpts                 []termenv.OutputOption
	renderer                   *lipgloss.Renderer
	name                       string
	hyperlinkFunc              HyperlinkFunc
	nameColor                  lipgloss.Color            // Map to cache colors for names
	showOSIcon                 bool                      // Whether to show OS icon
	enableNameColors           bool                      // Whether to enable name colors
	enableDebugPatternColoring bool                      // Whether to enable debug pattern coloring
	enableMultilineBoxes       bool                      // Whether to render multiline messages in boxes
	patternColorCache          map[string]lipgloss.Color // Cache for deterministic colors
}

// Generate a deterministic neon color from a string
func generateDeterministicNeonColor(s string) lipgloss.Color {
	// Use FNV hash for a deterministic but distributed value
	h := fnv.New32a()
	h.Write([]byte(s))
	hash := h.Sum32()

	// Enhanced color palette - larger variety of vibrant colors
	neonColors := []string{

		"#FF79E1", // Neon Pink
		"#7FFFD4", // Aquamarine
		"#FFD700", // Gold
		"#1E90FF", // Dodger Blue
		"#00FA9A", // Medium Spring Green
		"#FA8072", // Salmon
		"#E6FF00", // Acid Green
		"#FF73B3", // Tickle Me Pink

		// Vibrant Pastels
		"#FF9E80", // Coral
		"#F740FF", // Fuchsia
		"#40DFFF", // Electric Blue
		"#9D00FF", // Medium Purple
		"#00BFFF", // Deep Sky Blue
		"#CCFF00", // Electric Lime
		"#FF6037", // Outrageous Orange
		"#00CCCC", // Caribbean Green
		"#B3FF00", // Spring Bud
		"#AE00FB", // Purple Pizzazz

	}

	// Use the hash to select a color
	index := hash % uint32(len(neonColors))

	return lipgloss.Color(neonColors[index])
}

func NewTermLogger(writer io.Writer, sopts *slog.HandlerOptions, opts ...TermLoggerOption) *TermLogger {
	l := &TermLogger{
		writer:                     writer,
		slogOptions:                sopts,
		styles:                     DefaultStyles(),
		renderOpts:                 []termenv.OutputOption{},
		name:                       "",
		hyperlinkFunc:              stackerr.Hyperlink,
		enableNameColors:           false,
		nameColor:                  "",
		showOSIcon:                 false,
		enableDebugPatternColoring: false,
		enableMultilineBoxes:       true, // Enable by default
		patternColorCache:          make(map[string]lipgloss.Color),
	}
	for _, opt := range opts {
		opt(l)
	}

	if l.nameColor == "" && l.enableNameColors {
		l.nameColor = generateDeterministicNeonColor(l.name)
	}

	l.renderer = lipgloss.NewRenderer(l.writer, l.renderOpts...)

	return l
}

// Enabled implements slog.Handler.
func (l *TermLogger) Enabled(ctx context.Context, level slog.Level) bool {
	return l.slogOptions.Level.Level() <= level.Level()
}

func (l *TermLogger) render(s lipgloss.Style, strs ...string) string {
	return s.Renderer(l.renderer).Render(strs...)
}

func (l *TermLogger) renderFunc(s lipgloss.Style, strs string) string {
	return s.Renderer(l.renderer).Render(strs)
}

// getOrCreateColor gets a color from cache or creates a new one
func (l *TermLogger) getOrCreateColor(key string) lipgloss.Color {
	color, exists := l.patternColorCache[key]
	if !exists {
		color = generateDeterministicNeonColor(key)
		l.patternColorCache[key] = color
	}
	return color
}

// colorizeDebugPattern applies special coloring to debug messages matching pattern
func (l *TermLogger) colorizeDebugPattern(message string, maxWidth int, force bool) (outstr string, ok bool) {

	fallback := func() (string, bool) {

		if maxWidth <= 0 {
			return message, false
		}
		if maxWidth > len(message) {
			return message + strings.Repeat(" ", maxWidth-len(message)), false
		}
		return message[:maxWidth], false
	}

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Recovered in f [message: %s] [maxWidth: %d] [force: %t] [r: %v]\n", message, maxWidth, force, r)
			debug.PrintStack()
			outstr, ok = fallback()
		}
	}()

	if !l.enableDebugPatternColoring {
		return fallback()
	}

	parts, ok := ParseSegments(message)
	if !ok {
		if force {
			fbs, _ := fallback()
			return l.render(lipgloss.NewStyle().MaxWidth(maxWidth).Foreground(l.getOrCreateColor(fbs)).Bold(true), fbs), true
		}
		return fallback()
	}

	if len(parts) == 1 {

		return fallback()
	}

	overflow := (-1 * maxWidth) - len(parts) - 1
	if maxWidth > 0 {
		for _, part := range parts {
			overflow += len(part)
		}
	}

	// cut parts off starting with the first on, convertint it to one character
	for i := 0; overflow >= 0 && i < len(parts); i++ {
		saved := len(parts[i]) - 1
		parts[i] = parts[i][:1]
		overflow -= saved
		if overflow <= 0 {
			break
		}
	}

	if overflow > 0 {
		// give up and just return the first maxWidth characters
		return fallback()
	}

	out := strings.Builder{}

	charsout := 0
	for i, part := range parts {
		if i == len(parts)-1 {
			out.WriteString("[")
			charsout += 1
		}

		out.WriteString(l.render(lipgloss.NewStyle().Foreground(l.getOrCreateColor(part)).Bold(true), part))
		charsout += len(part)

		if i == len(parts)-1 {
			charsout += 1
			out.WriteString("]")
		} else if i < len(parts)-2 {
			out.WriteByte(':')
			charsout += 1
		}
	}

	if charsout < maxWidth {
		out.WriteString(strings.Repeat(" ", maxWidth-charsout))
	}

	return out.String(), true
}

const (
	timeFormat    = "15:04:05.0000 MST"
	maxNameLength = 15
)

// MessageToBox renders a multiline message in a box similar to error display
func MessageToBox(message string, styles *Styles, render renderFunc) string {
	// Format the message nicely in a box
	content := message

	// If the message is already styled, don't apply additional styling
	if !strings.Contains(message, "\x1b[") {
		// Apply moderate styling to the content for better readability
		content = render(lipgloss.NewStyle().Foreground(MessageColor), message)
	}

	// Render the content in a container with rounded borders
	boxStyle := styles.Error.Container.Copy().
		BorderForeground(TreeBorderColor)

	return render(boxStyle, content)
}

// HasNewlines checks if a string contains newlines
func HasNewlines(s string) bool {
	return strings.Contains(s, "\n")
}

func (l *TermLogger) Handle(ctx context.Context, r slog.Record) error {
	// Build a pretty, human-friendly log line.

	var b strings.Builder
	var appendageBuilder strings.Builder

	// 0. Name.
	if l.name != "" {
		// name := l.name
		// if len(name) > maxNameLength {
		// 	name = name[:maxNameLength]
		// }

		name := strings.ToUpper(l.name)

		coloredName, _ := l.colorizeDebugPattern(name, maxNameLength, true)

		b.WriteString(coloredName)

		// Add OS icon if enabled
		if l.showOSIcon {
			b.WriteString(" ")
			b.WriteString(l.render(GetOSIcon(), ""))
		}

		b.WriteByte(' ')
	}

	// 1 Level.
	levelStyle, ok := l.styles.Levels[r.Level]
	if !ok {
		levelStyle = l.styles.Levels[slog.LevelInfo]
	}
	b.WriteString(l.render(levelStyle, r.Level.String()))
	b.WriteByte(' ')

	// 2. Timestamp.
	ts := r.Time
	if ts.IsZero() {
		ts = time.Now()
	}

	b.WriteString(l.render(l.styles.Timestamp, ts.Format(timeFormat)))
	b.WriteByte(' ')

	// 3. Source (if requested).
	if l.slogOptions != nil && l.slogOptions.AddSource {
		source := stackerr.NewEnhancedSource(r.PC)
		// r.AddAttrs(slog.Attr{
		// 	Key:   "source.raw_func",
		// 	Value: slog.StringValue(source.RawFunc),
		// }, slog.Attr{
		// 	Key:   "source.raw_file_path",
		// 	Value: slog.StringValue(source.RawFilePath),
		// }, slog.Attr{
		// 	Key:   "source.raw_file_line",
		// 	Value: slog.IntValue(source.RawFileLine),
		// }, slog.Attr{
		// 	Key:   "source.enhanced_func",
		// 	Value: slog.StringValue(source.EnhancedFunc),
		// }, slog.Attr{
		// 	Key:   "source.enhanced_pkg",
		// 	Value: slog.StringValue(source.EnhancedPkg),
		// })
		b.WriteString(RenderEnhancedSource(source, l.styles, l.renderFunc, l.hyperlinkFunc))
		b.WriteByte(' ')
	}

	// 4. Message with special formatting for debug level if enabled
	var msg string
	// Check if message has newlines and should be rendered in a box
	if HasNewlines(r.Message) && l.enableMultilineBoxes {
		// For multiline messages, add a placeholder to the main log line
		msg = l.render(levelStyle.UnsetString().UnsetMaxWidth().UnsetBold(), "[multiline message below]")
		// Add the boxed content to the appendage
		appendageBuilder.WriteString(MessageToBox(r.Message, l.styles, l.renderFunc))
		appendageBuilder.WriteString("\n")
	} else if r.Level == slog.LevelDebug && l.enableDebugPatternColoring {
		msg, _ = l.colorizeDebugPattern(r.Message, -1, false)
	} else {
		msg = l.render(levelStyle.UnsetString().UnsetMaxWidth().UnsetBold(), r.Message)
	}
	b.WriteString(msg)

	// 5. Attributes (key=value ...).
	if r.NumAttrs() > 0 {
		b.WriteByte(' ')
		r.Attrs(func(a slog.Attr) bool {
			// Key styling (supports per-key overrides).
			keyStyle, ok := l.styles.Keys[a.Key]
			if !ok {
				keyStyle = l.styles.Key
			}

			key := l.render(keyStyle, a.Key)

			var valColored string

			// Check for errors first - they get special handling
			if (a.Key == "error" || a.Key == "err" || a.Key == "error.payload") && r.Level > slog.LevelWarn {
				// Special handling for error values - use beautiful error trace display
				if err, ok := a.Value.Any().(error); ok {
					appendageBuilder.WriteString(ErrorToTrace(err, r, l.styles, l.renderFunc, l.hyperlinkFunc))
					appendageBuilder.WriteString("\n")
					valColored = l.render(l.styles.ValueAppendage, "[error rendered below]")
				} else {

					// Fallback for non-error values in "error" key
					valStyle, ok := l.styles.Values[a.Key]
					if !ok {
						valStyle = l.styles.Value
					}
					val := fmt.Sprintf("type: %T - %v", a.Value.Any(), a.Value.Any())
					valColored = l.render(valStyle, val)
				}
			} else {
				// Value styling (supports per-key overrides).
				valStyle, ok := l.styles.Values[a.Key]
				if !ok {
					valStyle = l.styles.Value
				}

				// Resolve slog.Value to an interface{} and stringify.
				val := fmt.Sprint(a.Value)
				valColored = l.render(valStyle, val)

			}

			b.WriteString(key)
			b.WriteByte('=')
			b.WriteString(valColored)
			b.WriteByte(' ')

			return true
		})
		// Remove trailing space that was added inside the loop.
		if b.Len() > 0 && b.String()[b.Len()-1] == ' ' {
			str := b.String()
			b.Reset()
			b.WriteString(strings.TrimRight(str, " "))
		}
	}

	// 6. Final newline.
	b.WriteByte('\n')

	// Determine output writer (defaults to stdout).
	w := l.writer
	if w == nil {
		w = os.Stdout
	}

	_, err := fmt.Fprint(w, b.String())
	if appendageBuilder.Len() > 0 {
		_, err = fmt.Fprint(w, appendageBuilder.String())
	}
	return err
}

func (l *TermLogger) WithAttrs(attrs []slog.Attr) slog.Handler {
	return l
}

func (l *TermLogger) WithGroup(name string) slog.Handler {
	return l
}

// isJSONString checks if a string/bytes value looks like JSON
// by checking for the [nospace{] prefix mentioned by the user
func isJSONString(v interface{}) (string, bool) {
	var data string

	switch val := v.(type) {
	case string:
		data = val
	case []byte:
		data = string(val)
	default:
		return "", false
	}

	// Check for JSON-like patterns (object or array)
	trimmed := strings.TrimSpace(data)
	if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
		(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {

		// Try to validate it's actually valid JSON
		var temp interface{}
		if err := json.Unmarshal([]byte(trimmed), &temp); err == nil {
			return trimmed, true
		}
	}

	return "", false
}

// isStructLike checks if a value is a struct, map, slice, or other complex type worth dumping
func isStructLike(v interface{}) bool {
	if v == nil {
		return false
	}

	val := reflect.ValueOf(v)

	// Dereference pointers
	for val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return false
		}
		val = val.Elem()
	}

	switch val.Kind() {
	case reflect.Struct, reflect.Map, reflect.Slice, reflect.Array:
		return true
	case reflect.Interface:
		if !val.IsNil() {
			return isStructLike(val.Interface())
		}
	}

	return false
}
