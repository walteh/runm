package slogdevterm

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type BoxWithLabel struct {
	BoxStyle   lipgloss.Style
	LabelStyle lipgloss.Style
	RenderFunc renderFunc
}

func NewDefaultBoxWithLabel(render renderFunc) BoxWithLabel {
	return BoxWithLabel{
		LabelStyle: lipgloss.NewStyle().
			Foreground(TreeBorderColor).
			Bold(true).
			PaddingTop(0).
			PaddingBottom(0).
			PaddingLeft(1).
			PaddingRight(1),

		// You could, of course, also set background and foreground colors here
		// as well.
		BoxStyle: lipgloss.NewStyle().
			Padding(1).
			BorderForeground(TreeNullColor).
			// Bold(true).
			Border(lipgloss.RoundedBorder()),
		// PaddingTop(0).
		// PaddingBottom(0).
		// PaddingLeft(1).
		// PaddingRight(1),
		RenderFunc: render,
	}
}

func (b BoxWithLabel) Render(label, content string, width int) string {
	var (
		// Query the box style for some of its border properties so we can
		// essentially take the top border apart and put it around the label.
		border          lipgloss.Border = b.BoxStyle.GetBorderStyle()
		topBorderStyler                 = lipgloss.NewStyle().
				Foreground(b.BoxStyle.GetBorderTopForeground()).
				Background(b.BoxStyle.GetBorderTopBackground())
		topLeft  string = b.RenderFunc(topBorderStyler, border.TopLeft)
		topRight string = b.RenderFunc(topBorderStyler, border.TopRight)

		renderedLabel string = b.RenderFunc(b.LabelStyle, label)
	)

	// wrappedValue := smartWrapText(content, width-10)

	// replace all indentation with some color

	// Render top row with the label
	borderWidth := b.BoxStyle.GetHorizontalBorderSize()
	cellsShort := max(0, width+borderWidth-lipgloss.Width(topLeft+topRight+renderedLabel))
	gap := strings.Repeat(border.Top, cellsShort)
	top := topLeft + renderedLabel + b.RenderFunc(topBorderStyler, gap) + topRight

	// Render the rest of the box
	bottom := b.RenderFunc(b.BoxStyle.
		BorderTop(false).
		BorderLeft(true).
		BorderRight(true).
		BorderBottom(true).
		Width(width), strings.TrimSpace(content))

	// Stack the pieces
	return "\n" + top + "\n" + bottom + "\n"
}

func smartWrapText(text string, maxWidth int) string {
	var result strings.Builder
	lines := strings.Split(text, "\n")

	for lineIdx, line := range lines {
		if lineIdx > 0 {
			result.WriteString("\n")
		}

		// If line is short enough, use it as-is
		if len(line) <= maxWidth {
			result.WriteString(line)
			continue
		}

		// Smart wrapping: try to break at word boundaries
		words := strings.Fields(line)
		if len(words) == 0 {
			// If no words, fall back to character wrapping with proper breaks
			for len(line) > maxWidth {
				result.WriteString(line[:maxWidth])
				result.WriteString("\n      ↳ ")
				line = line[maxWidth:]
			}
			result.WriteString(line)
			continue
		}

		currentLine := ""
		for wordIdx, word := range words {
			// If a single word is too long, break it
			if len(word) > maxWidth {
				// Write current line if it has content
				if currentLine != "" {
					result.WriteString(currentLine)
					result.WriteString("\n")
					currentLine = ""
				}
				// Break the long word
				for len(word) > maxWidth {
					result.WriteString(indentation)
					result.WriteString(word[:maxWidth-8]) // Account for continuation indicator
					result.WriteString("\n")
					word = word[maxWidth-8:]
				}
				// Add remaining part of word
				if len(word) > 0 {
					result.WriteString(indentation)
					currentLine = word
				}
				continue
			}

			testLine := currentLine
			if testLine != "" {
				testLine += " "
			}
			testLine += word

			// If adding this word would exceed the limit
			if len(testLine) > maxWidth {
				// Write the current line if it has content
				if currentLine != "" {
					result.WriteString(currentLine)
					result.WriteString("\n")
					// Add continuation indicator for wrapped lines
					result.WriteString(indentation)
					currentLine = word
				} else {
					// Single word is too long, break it (shouldn't happen due to check above)
					result.WriteString(word)
					if wordIdx < len(words)-1 {
						result.WriteString("\n" + indentation)
					}
					currentLine = ""
				}
			} else {
				currentLine = testLine
			}
		}

		// Write any remaining content
		if currentLine != "" {
			result.WriteString(currentLine)
		}
	}

	return result.String()
}
