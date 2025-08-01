package slogdevterm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"reflect"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/walteh/runm/pkg/stackerr"
)

var _ slog.Handler = (*TermLogger)(nil)

// Pattern for special debug log formatting: WORD:WORD[content]
func ParseSegments(s string) ([]string, string, bool) {
	// Find the first "[" and the last "]"
	open := strings.IndexByte(s, '[')
	close := strings.LastIndexByte(s, ']')
	if open < 0 || close < 0 || close <= open {
		return nil, "", false
	}

	// Extract prefix and suffix
	prefix := s[:open]
	suffix := s[open+1 : close]
	var remaining string
	if close < len(s) {
		remaining = s[close+1:]
	}

	// Split the prefix on ":" (this will yield all m1…mN)
	parts := strings.Split(prefix, ":")

	// Append the bracketed part as the final element
	parts = append(parts, suffix)
	return parts, remaining, true
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
	"darwin":  newColoredString("D", "#00FA9A"), // light green Apple logo for macOS
	"linux":   newColoredString("L", "#FF9E80"), // orange Penguin for Linux
	"windows": newColoredString("W", "#40DFFF"), // blue Window for Windows
	// "darwin":  newColoredString("\uf302", "#00FA9A"), // light green Apple logo for macOS
	// "linux":   newColoredString("\ue712", "#FF9E80"), // orange Penguin for Linux
	// "windows": newColoredString("\ue70f", "#40DFFF"), // blue Window for Windows
	"freebsd": newColoredString("\uf30c", "white"), // green BSD Daemon for FreeBSD

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
	pattenColorCacheMutex      sync.Mutex
	// Group and attribute tracking for proper slog group handling
	groups []string    // Stack of group names for dotted notation
	attrs  []slog.Attr // Preformatted attributes from WithAttrs
}

// Generate a deterministic neon color from a string

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
		pattenColorCacheMutex:      sync.Mutex{},
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
	l.pattenColorCacheMutex.Lock()
	defer l.pattenColorCacheMutex.Unlock()

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

	parts, remaining, ok := ParseSegments(message)
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

	return out.String() + remaining, true
}

const (
	timeFormat    = "15:04:05.0000 MST"
	maxNameLength = 15
	indentation   = "      ↳ "
)

// smartWrapText wraps text intelligently at word boundaries with visual indicators

// MessageToBox renders a multiline message in a box similar to error display
func MessageToBox(message string, styles *Styles, render renderFunc) string {
	// Apply smart text wrapping with proper width accounting for padding
	maxContentWidth := 140
	wrappedMessage := smartWrapText(message, maxContentWidth)

	// Format the message with proper styling
	content := wrappedMessage

	// If the message is already styled, don't apply additional styling
	if !strings.Contains(message, "\x1b[") {
		contentStyle := lipgloss.NewStyle().Foreground(MessageColor)
		content = render(contentStyle, wrappedMessage)
	}

	// Render the content in a container with rounded borders
	boxStyle := lipgloss.NewStyle().
		Padding(1, 2).
		Margin(1, 0).
		MaxWidth(150).
		BorderForeground(TreeBorderColor).
		BorderStyle(lipgloss.RoundedBorder())

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
		b.WriteString(RenderEnhancedSourceWIthTrim(source, l.styles, l.renderFunc, l.hyperlinkFunc, true))
		b.WriteByte(' ')
	}

	// 4. Message with special formatting for debug level if enabled
	var msg string
	// Check if message has newlines and should be rendered in a box
	if HasNewlines(r.Message) && l.enableMultilineBoxes {
		// For multiline messages, add a placeholder to the main log line
		// msg = l.render(levelStyle.UnsetString().UnsetMaxWidth().UnsetBold(), "[multiline message below]")
		msg = l.render(l.styles.ValueAppendage, "[multiline message below]")

		// Add the boxed content to the appendage
		appendageBuilder.WriteString(ValueToBox(r.Message, "message", l.styles, l.renderFunc))
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
			l.processAttribute(attr, keyStyle, valStyle, &attrBuilder, &appendageBuilder)
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
				if isErrorKey(a.Key) {
					valStyle = l.styles.Values["error"]
				} else {
					valStyle = l.styles.Value
				}
			}
			l.processAttribute(a, keyStyle, valStyle, &attrBuilder, &appendageBuilder)
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

func isErrorKey(key string) bool {
	if strings.Contains(key, "stderr") {
		return false
	}

	return key == "error" ||
		key == "err" ||
		key == "error.payload" ||
		strings.HasSuffix(key, "error") ||
		strings.HasSuffix(key, "err") ||
		strings.HasPrefix(key, "error") ||
		strings.HasPrefix(key, "err")
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

// isSimpleMap checks if a value is a simple map that should be flattened
func (l *TermLogger) isSimpleMap(v interface{}) bool {
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

	// Check if it's a map with string keys and simple values
	if val.Kind() == reflect.Map {
		keyType := val.Type().Key()
		valueType := val.Type().Elem()

		// Only handle maps with string keys
		if keyType.Kind() != reflect.String {
			return false
		}

		// Check if all values are simple types (string, numbers, bool)
		switch valueType.Kind() {
		case reflect.String, reflect.Bool,
			reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			return true
		case reflect.Interface:
			// For interface{} values, we need to check each value individually
			// We'll be conservative and allow it, checking in processMapAttribute
			return true
		}
	}

	return false
}

// processMapAttribute processes a map by flattening it into dotted notation
func (l *TermLogger) processMapAttribute(a slog.Attr, keyStyle lipgloss.Style, valStyle lipgloss.Style, result *strings.Builder, appendage *strings.Builder) {
	val := reflect.ValueOf(a.Value.Any())

	// Dereference pointers
	for val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return
		}
		val = val.Elem()
	}

	if val.Kind() != reflect.Map {
		return
	}

	// Create a temporary logger with the extended group path
	tempLogger := *l
	if a.Key != "" {
		// Add this map key to the group stack
		tempLogger.groups = make([]string, len(l.groups)+1)
		copy(tempLogger.groups, l.groups)
		tempLogger.groups[len(l.groups)] = a.Key
	}

	// Process each key-value pair in the map
	for _, key := range val.MapKeys() {
		mapValue := val.MapIndex(key)
		keyStr := key.String()

		// Check if the value is a simple type
		if l.isSimpleValue(mapValue.Interface()) {
			// Create an attribute for this key-value pair
			attr := slog.Attr{
				Key:   keyStr,
				Value: slog.AnyValue(mapValue.Interface()),
			}
			tempLogger.processAttribute(attr, keyStyle, valStyle, result, appendage)
		}
	}
}

// isSimpleValue checks if a value is a simple type that can be displayed inline
func (l *TermLogger) isSimpleValue(v interface{}) bool {
	if v == nil {
		return true
	}

	val := reflect.ValueOf(v)

	// Dereference pointers
	for val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return true
		}
		val = val.Elem()
	}

	switch val.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}

	return false
}

// func convertSlogValueToString(v slog.Value) string {
// 	switch v.Kind() {
// 	case slog.KindDuration:
// 		if v.Duration()%time.Second != 0 {
// 			return v.Duration().Truncate(time.Microsecond).String()
// 		} else if v.Duration()%time.Millisecond != 0 {
// 			return v.Duration().Truncate(time.Nanosecond).String()
// 		}
// 		return v.Duration().String()
// 	case slog.KindFloat64:
// 		return fmt.Sprintf("%f", v.Float64())
// 	case slog.KindInt64:
// 		return fmt.Sprintf("%d", v.Int64())
// 	}
// 	return fmt.Sprint(v)
// }

func renderDuration(v slog.Value, styles *Styles, render renderFunc) string {
	d := v.Duration()
	dStr := d.String()

	// Determine the appropriate color based on the primary time unit
	var color lipgloss.CompleteAdaptiveColor
	switch {
	case d >= time.Hour:
		color = DurationLargeColor // Red (minutes, hours, days)
	case d >= time.Minute:
		color = DurationSecondsColor // Red (minutes, hours, days)
	case d >= time.Second:
		color = DurationMillisColor // Orange (seconds)
	case d >= time.Millisecond:
		color = DurationMicrosColor // Yellow (milliseconds)
	case d >= time.Microsecond:
		color = DurationNanosColor // Chartreuse (microseconds)
	default:
		color = DurationNanosColor // Green (nanoseconds)
	}

	// Replace time units with bold+colored versions
	unitStyle := lipgloss.NewStyle().Faint(true).Foreground(color)
	result := dStr

	// Check for each unit and replace only if present (using TrimSuffix for speed)
	if strings.HasSuffix(result, "µs") {
		result = strings.TrimSuffix(result, "µs") + render(unitStyle, "µs")
	} else if strings.HasSuffix(result, "ms") {
		result = strings.TrimSuffix(result, "ms") + render(unitStyle, "ms")
	} else if strings.HasSuffix(result, "ns") {
		result = strings.TrimSuffix(result, "ns") + render(unitStyle, "ns")
	} else if strings.HasSuffix(result, "h") {
		result = strings.TrimSuffix(result, "h") + render(unitStyle, "h")
	} else if strings.HasSuffix(result, "m") {
		result = strings.TrimSuffix(result, "m") + render(unitStyle, "m")
	} else if strings.HasSuffix(result, "s") {
		result = strings.TrimSuffix(result, "s") + render(unitStyle, "s")
	}

	return render(styles.Value, result)
}

// processAttribute processes a single attribute, handling groups and nested structures
func (l *TermLogger) processAttribute(a slog.Attr, keyStyle lipgloss.Style, valStyle lipgloss.Style, result *strings.Builder, appendage *strings.Builder) {
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
			tempLogger.processAttribute(groupAttr, keyStyle, valStyle, result, appendage)
		}
		return
	}

	// Handle map attributes by flattening them into dotted notation
	if l.isSimpleMap(a.Value.Any()) {
		l.processMapAttribute(a, keyStyle, valStyle, result, appendage)
		return
	}

	// Create the dotted key for this attribute
	dottedKey := l.buildDottedKey(a.Key)

	// Render the key
	key := l.render(keyStyle, dottedKey)

	var valColored string

	// Get the string representation of the value
	val := fmt.Sprint(a.Value)

	// Check if the value has newlines and should be rendered in a box
	if HasNewlines(val) && l.enableMultilineBoxes {
		// Add the boxed content to the appendage
		appendage.WriteString(ValueToBox(val, dottedKey, l.styles, l.renderFunc))
		appendage.WriteString("\n")

		// Add a placeholder in the main log line
		valColored = l.render(l.styles.ValueAppendage, "[multiline value below]")
	} else if a.Value.Kind() == slog.KindDuration {
		valColored = renderDuration(a.Value, l.styles, l.renderFunc)
	} else {
		// Regular value handling
		valColored = l.render(valStyle, val)
	}

	result.WriteString(key)
	result.WriteByte('=')
	result.WriteString(valColored)
	result.WriteByte(' ')
}

// ValueToBox renders a multiline value in a box with the attribute key as title
func ValueToBox(value string, keyName string, styles *Styles, render renderFunc) string {
	// Apply smart text wrapping with proper width accounting for padding
	// maxContentWidth := 140
	// wrappedValue := smartWrapText(value, maxContentWidth)

	// // Format the value with proper styling
	// content := wrappedValue

	// // Create the title header with proper styling
	// titleStyle := lipgloss.NewStyle().
	// 	Foreground(TreeKeyColor).
	// 	Bold(true)

	// // Create the box style with proper dimensions
	// boxStyle := lipgloss.NewStyle().
	// 	Padding(1, 2).
	// 	Margin(1, 0).
	// 	MaxWidth(150).
	// 	BorderForeground(TreeBorderColor).
	// 	BorderStyle(lipgloss.RoundedBorder())

	// If the value doesn't have existing styling, apply moderate styling for readability
	// if !strings.Contains(value, "\x1b[") {
	// 	contentStyle := lipgloss.NewStyle().Foreground(MessageColor)
	// 	value = render(contentStyle, value)
	// }

	// header := render(titleStyle, keyName)
	// fullContent := header + "\n" + content

	box := NewDefaultBoxWithLabel(render)

	return box.Render(keyName, value, 150)
}

func isPrintableASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func escapeString(s string) string {
	if isPrintableASCII(s) {
		return s // Fast path for normal strings
	}

	// Use Go's quote function which handles all escape sequences properly
	quoted := strconv.Quote(s)
	// Remove the surrounding quotes that strconv.Quote adds
	if len(quoted) >= 2 && quoted[0] == '"' && quoted[len(quoted)-1] == '"' {
		return quoted[1 : len(quoted)-1]
	}
	return quoted
}
