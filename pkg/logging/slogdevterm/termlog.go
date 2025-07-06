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

func WithDelimiter(rune rune) TermLoggerOption {
	return func(l *TermLogger) {
		l.delimiter = rune
		l.enableDelimiter = true
	}
}

func newColoredString(s string, color string) lipgloss.Style {
	return lipgloss.NewStyle().SetString(s).Foreground(lipgloss.Color(color)).Bold(true)
}

// simple linux =  \U000F033D"
// OSIconMap maps OS identifiers to unicode icons \udb80\udf3d
var OSIconMap = map[string]lipgloss.Style{
	"darwin":  newColoredString("\uf302", "#00FA9A"), // light green Apple logo for macOS
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

func WithColorProfile(profile termenv.Profile) TermLoggerOption {
	return func(l *TermLogger) {
		l.colorProfile = profile
	}
}

type HyperlinkFunc func(link, renderedText string) string

type TermLogger struct {
	slogOptions                *slog.HandlerOptions
	styles                     *Styles
	writer                     io.Writer
	delimiter                  rune
	enableDelimiter            bool
	renderOpts                 []termenv.OutputOption
	renderer                   *lipgloss.Renderer
	name                       string
	colorProfile               termenv.Profile
	hyperlinkFunc              HyperlinkFunc
	nameColor                  lipgloss.Color            // Map to cache colors for names
	showOSIcon                 bool                      // Whether to show OS icon
	enableNameColors           bool                      // Whether to enable name colors
	enableDebugPatternColoring bool                      // Whether to enable debug pattern coloring
	enableMultilineBoxes       bool                      // Whether to render multiline messages in boxes
	patternColorCache          map[string]lipgloss.Color // Cache for deterministic colors
	// Group and attribute tracking for proper slog group handling
	groups []string    // Stack of group names for dotted notation
	attrs  []slog.Attr // Preformatted attributes from WithAttrs
}

// Generate a deterministic neon color from a string
func generateDeterministicNeonColor(s string) lipgloss.Color {
	// Use FNV hash for a deterministic but distributed value
	h := fnv.New32a()
	h.Write([]byte(s))
	hash := h.Sum32()

	// hash := sha256.Sum256([]byte(s))

	neonColors := []string{
		// Reds & Oranges
		"#FF0000", // Red
		"#FF4500", // OrangeRed
		"#FF6347", // Tomato
		"#FF7F50", // Coral
		"#FFD700", // Gold

		// Pinks & Purples
		"#FF1493", // DeepPink
		"#FF69B4", // HotPink
		"#BA55D3", // MediumOrchid
		"#9370DB", // MediumPurple
		"#DA70D6", // Orchid

		// Yellows & Greens
		"#ADFF2F", // GreenYellow
		"#7FFF00", // Chartreuse
		"#00FF00", // Lime
		"#32CD32", // LimeGreen
		"#00FA9A", // MediumSpringGreen

		// Cyans & Blues
		"#00FFFF", // Aqua
		"#00CED1", // DarkTurquoise
		"#1E90FF", // DodgerBlue
		"#4169E1", // RoyalBlue
		"#6495ED", // CornflowerBlue

		// Teals & Aquas
		"#20B2AA", // LightSeaGreen
		"#40E0D0", // Turquoise
		"#48D1CC", // MediumTurquoise
		"#5F9EA0", // CadetBlue
		"#00BFFF", // DeepSkyBlue

		// Browns & Accents
		"#D2691E", // Chocolate
		"#FF8C00", // DarkOrange
		"#DAA520", // GoldenRod
		"#DC143C", // Crimson
		// "#8B008B", // DarkMagenta
	}

	// Use the hash to select a color
	// index := big.NewInt(0).Mod(big.NewInt(0).SetBytes(hash[:]), big.NewInt(int64(len(neonColors)))).Int64()
	index := int(hash) % len(neonColors)

	return lipgloss.Color(neonColors[index])
}

func NewTermLogger(writer io.Writer, sopts *slog.HandlerOptions, opts ...TermLoggerOption) *TermLogger {
	l := TermLogger{
		writer:                     writer,
		slogOptions:                sopts,
		styles:                     DefaultStyles(),
		renderOpts:                 []termenv.OutputOption{},
		name:                       "",
		hyperlinkFunc:              stackerr.Hyperlink,
		enableNameColors:           false,
		enableDelimiter:            false,
		delimiter:                  '\n',
		colorProfile:               termenv.ANSI256,
		nameColor:                  "",
		showOSIcon:                 false,
		enableDebugPatternColoring: false,
		enableMultilineBoxes:       true, // Enable by default
		patternColorCache:          make(map[string]lipgloss.Color),
		groups:                     []string{},
		attrs:                      []slog.Attr{},
	}
	for _, opt := range opts {
		opt(&l)
	}

	if l.nameColor == "" && l.enableNameColors {
		l.nameColor = generateDeterministicNeonColor(l.name)
	}

	l.renderOpts = append(l.renderOpts, termenv.WithProfile(l.colorProfile))

	l.renderer = lipgloss.NewRenderer(l.writer, l.renderOpts...)
	l.renderer.SetColorProfile(l.colorProfile)

	return &l
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

		color := l.getOrCreateColor(part)

		out.WriteString(l.render(lipgloss.NewStyle().Foreground(color).Bold(true), part))
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
	hasAttrs := len(l.attrs) > 0 || r.NumAttrs() > 0
	if hasAttrs {
		b.WriteByte(' ')

		var attrBuilder strings.Builder

		// First process preformatted attributes from WithAttrs
		for _, attr := range l.attrs {
			// Check for errors first - they get special handling
			if (attr.Key == "error" || attr.Key == "err" || attr.Key == "error.payload") && r.Level > slog.LevelWarn {
				if err, ok := attr.Value.Any().(error); ok {
					appendageBuilder.WriteString(ErrorToTrace(err, r, l.styles, l.renderFunc, l.hyperlinkFunc))
					appendageBuilder.WriteString("\n")

					// Add a placeholder in the main log line
					keyStyle, ok := l.styles.Keys[attr.Key]
					if !ok {
						keyStyle = l.styles.Key
					}
					dottedKey := l.buildDottedKey(attr.Key)
					key := l.render(keyStyle, dottedKey)
					valColored := l.render(l.styles.ValueAppendage, "[error rendered below]")

					attrBuilder.WriteString(key)
					attrBuilder.WriteByte('=')
					attrBuilder.WriteString(valColored)
					attrBuilder.WriteByte(' ')
					continue
				}
			}

			// Regular attribute processing with dotted notation
			keyStyle, ok := l.styles.Keys[attr.Key]
			if !ok {
				keyStyle = l.styles.Key
			}
			valStyle, ok := l.styles.Values[attr.Key]
			if !ok {
				valStyle = l.styles.Value
			}
			l.processAttribute(attr, keyStyle, valStyle, &attrBuilder)
		}

		// Then process record attributes
		r.Attrs(func(a slog.Attr) bool {
			// Check for errors first - they get special handling
			if (a.Key == "error" || a.Key == "err" || a.Key == "error.payload") && r.Level > slog.LevelWarn {
				if err, ok := a.Value.Any().(error); ok {
					appendageBuilder.WriteString(ErrorToTrace(err, r, l.styles, l.renderFunc, l.hyperlinkFunc))
					appendageBuilder.WriteString("\n")

					// Add a placeholder in the main log line
					keyStyle, ok := l.styles.Keys[a.Key]
					if !ok {
						keyStyle = l.styles.Key
					}
					dottedKey := l.buildDottedKey(a.Key)
					key := l.render(keyStyle, dottedKey)
					valColored := l.render(l.styles.ValueAppendage, "[error rendered below]")

					attrBuilder.WriteString(key)
					attrBuilder.WriteByte('=')
					attrBuilder.WriteString(valColored)
					attrBuilder.WriteByte(' ')
					return true
				}
			}

			// Regular attribute processing with dotted notation
			keyStyle, ok := l.styles.Keys[a.Key]
			if !ok {
				keyStyle = l.styles.Key
			}
			valStyle, ok := l.styles.Values[a.Key]
			if !ok {
				valStyle = l.styles.Value
			}
			l.processAttribute(a, keyStyle, valStyle, &attrBuilder)
			return true
		})

		// Add the processed attributes to the main log line
		attrStr := attrBuilder.String()
		if len(attrStr) > 0 {
			// Remove trailing space
			attrStr = strings.TrimRight(attrStr, " ")
			b.WriteString(attrStr)
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
	if err != nil {
		return err
	}

	if appendageBuilder.Len() > 0 {
		_, err = fmt.Fprint(w, appendageBuilder.String())
		if err != nil {
			return err
		}
	}

	if l.enableDelimiter {
		_, err = w.Write([]byte{byte(l.delimiter)})
		if err != nil {
			return err
		}
	}

	return err
}

func (l *TermLogger) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return l
	}

	// Create a copy of the logger with the new attributes
	newLogger := *l
	newLogger.attrs = make([]slog.Attr, len(l.attrs)+len(attrs))
	copy(newLogger.attrs, l.attrs)
	copy(newLogger.attrs[len(l.attrs):], attrs)

	return &newLogger
}

func (l *TermLogger) WithGroup(name string) slog.Handler {
	if name == "" {
		return l
	}

	// Create a copy of the logger with the new group
	newLogger := *l
	newLogger.groups = make([]string, len(l.groups)+1)
	copy(newLogger.groups, l.groups)
	newLogger.groups[len(l.groups)] = name

	return &newLogger
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

// buildDottedKey creates a dotted key from groups and the attribute key
func (l *TermLogger) buildDottedKey(attrKey string) string {
	if len(l.groups) == 0 {
		return attrKey
	}

	// Join groups with dots and append the attribute key
	groupPrefix := strings.Join(l.groups, ".")
	if attrKey == "" {
		return groupPrefix
	}
	return groupPrefix + "." + attrKey
}

// processAttribute processes a single attribute, handling groups and nested structures
func (l *TermLogger) processAttribute(a slog.Attr, keyStyle lipgloss.Style, valStyle lipgloss.Style, result *strings.Builder) {
	// Resolve the attribute value
	a.Value = a.Value.Resolve()

	// Skip empty attributes
	if a.Equal(slog.Attr{}) {
		return
	}

	// Handle group attributes by processing each sub-attribute with extended group path
	if a.Value.Kind() == slog.KindGroup {
		groupAttrs := a.Value.Group()
		if len(groupAttrs) == 0 {
			return // Skip empty groups
		}

		// Create a temporary logger with the extended group path
		tempLogger := *l
		if a.Key != "" {
			// Add this group to the group stack
			tempLogger.groups = make([]string, len(l.groups)+1)
			copy(tempLogger.groups, l.groups)
			tempLogger.groups[len(l.groups)] = a.Key
		}

		// Process each attribute in the group
		for _, groupAttr := range groupAttrs {
			tempLogger.processAttribute(groupAttr, keyStyle, valStyle, result)
		}
		return
	}

	// Create the dotted key for this attribute
	dottedKey := l.buildDottedKey(a.Key)

	// Render the key-value pair
	key := l.render(keyStyle, dottedKey)

	var valColored string

	// Regular value handling - no special global error storage
	val := fmt.Sprint(a.Value)
	valColored = l.render(valStyle, val)

	result.WriteString(key)
	result.WriteByte('=')
	result.WriteString(valColored)
	result.WriteByte(' ')
}
