package slogdevterm

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/walteh/runm/pkg/stackerr"
)

var _ slog.Handler = (*TermLogger)(nil)

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

type HyperlinkFunc func(link, renderedText string) string

type TermLogger struct {
	slogOptions   *slog.HandlerOptions
	styles        *Styles
	writer        io.Writer
	renderOpts    []termenv.OutputOption
	renderer      *lipgloss.Renderer
	name          string
	hyperlinkFunc HyperlinkFunc
	nameColors    map[string]lipgloss.Color // Map to cache colors for names
}

// Generate a deterministic neon color from a string
func generateDeterministicNeonColor(s string) lipgloss.Color {
	// Use FNV hash for a deterministic but distributed value
	h := fnv.New32a()
	h.Write([]byte(s))
	hash := h.Sum32()

	// Enhanced color palette - larger variety of vibrant colors
	neonColors := []string{
		// Primary Neons
		"#FF00FF", // Magenta
		"#00FFFF", // Cyan
		"#FF0000", // Red
		"#00FF00", // Green
		"#0000FF", // Blue
		"#FFFF00", // Yellow

		// Secondary Neons
		"#FF4500", // Orange Red
		"#9D00FF", // Purple
		"#FF0080", // Hot Pink
		"#00FF80", // Spring Green
		"#00B0FF", // Bright Blue
		"#80FF00", // Lime Green

		// Tertiary Neons
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
		"#8B78E6", // Medium Purple
		"#00BFFF", // Deep Sky Blue
		"#CCFF00", // Electric Lime
		"#FF6037", // Outrageous Orange
		"#00CCCC", // Caribbean Green
		"#B3FF00", // Spring Bud
		"#FF4ADE", // Purple Pizzazz

		// Rich Jewel Tones
		"#3300FF", // Ultramarine
		"#00FF9C", // Caribbean Green
		"#FF3800", // Coquelicot
		"#56FF0D", // Screamin' Green
		"#AE00FB", // Electric Violet
	}

	// Use the hash to select a color
	index := hash % uint32(len(neonColors))

	return lipgloss.Color(neonColors[index])
}

func NewTermLogger(writer io.Writer, sopts *slog.HandlerOptions, opts ...TermLoggerOption) *TermLogger {
	l := &TermLogger{
		writer:        writer,
		slogOptions:   sopts,
		styles:        DefaultStyles(),
		renderOpts:    []termenv.OutputOption{},
		name:          "",
		hyperlinkFunc: stackerr.Hyperlink,
		nameColors:    make(map[string]lipgloss.Color),
	}
	for _, opt := range opts {
		opt(l)
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

const (
	timeFormat    = "15:04:05.0000 MST"
	maxNameLength = 10
)

func (l *TermLogger) Handle(ctx context.Context, r slog.Record) error {
	// Build a pretty, human-friendly log line.

	var b strings.Builder
	var appendageBuilder strings.Builder

	enableNameColors := false

	// 0. Name.
	if l.name != "" {
		name := l.name
		if len(name) > maxNameLength {
			name = name[:maxNameLength]
		}

		prefixStyle := l.styles.Prefix

		if enableNameColors {

			// Get or generate a color for this name
			nameColor, exists := l.nameColors[name]
			if !exists {
				// Generate a deterministic color based on the name
				nameColor = generateDeterministicNeonColor(name)
				l.nameColors[name] = nameColor
			}

			prefixStyle = prefixStyle.Foreground(nameColor).Bold(true).Faint(true)
		}

		// Create a style with the deterministic color
		b.WriteString(l.render(prefixStyle, strings.ToUpper(name)))
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
		b.WriteString(RenderEnhancedSource(stackerr.NewEnhancedSource(r.PC), l.styles, l.renderFunc, l.hyperlinkFunc))
		b.WriteByte(' ')
	}

	// 4. Message.
	msg := l.render(levelStyle.UnsetString().UnsetMaxWidth().UnsetBold(), r.Message)
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

			// if pv, ok := a.Value.Any().(valuelog.PrettyRawJSONValue); ok {
			// 	appendageBuilder.WriteString(JSONToTree(a.Key, pv.RawJSON(), l.styles, l.renderFunc))
			// 	appendageBuilder.WriteString("\n")
			// 	valColored = l.render(l.styles.ValueAppendage, "󰘦 "+a.Key)
			// } else if pv, ok := a.Value.Any().(valuelog.PrettyAnyValue); ok {
			// 	appendageBuilder.WriteString(StructToTreeWithTitle(pv.Any(), a.Key, l.styles, l.renderFunc))
			// 	appendageBuilder.WriteString("\n")
			// 	valColored = l.render(l.styles.ValueAppendage, "󰙅 "+a.Key)
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
