package state

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/walteh/runm/pkg/syncmap"
)

// buildStringTable creates a formatted table for string-keyed resources
func buildStringTable[T any](title string, m *syncmap.Map[string, T], status string) string {
	var table strings.Builder
	table.WriteString(fmt.Sprintf("%s:\n", title))
	table.WriteString("Reference ID\tType\tStatus\n")
	table.WriteString("------------\t----\t------\n")

	count := 0
	m.Range(func(key string, value T) bool {
		table.WriteString(fmt.Sprintf("%s\t%T\t%s\n", key, value, status))
		count++
		return true
	})

	if count == 0 {
		table.WriteString("(none)\n")
	}

	return table.String()
}

// buildUint32Table creates a formatted table for uint32-keyed resources
func buildUint32Table[T any](title string, m *syncmap.Map[uint32, T], status string) string {
	var table strings.Builder
	table.WriteString(fmt.Sprintf("%s:\n", title))
	table.WriteString("Port\tType\tStatus\tDetails\n")
	table.WriteString("----\t----\t------\t-------\n")

	count := 0
	m.Range(func(key uint32, value T) bool {
		details := fmt.Sprintf("port=%d", key)
		table.WriteString(fmt.Sprintf("%d\t%T\t%s\t%s\n", key, value, status, details))
		count++
		return true
	})

	if count == 0 {
		table.WriteString("(none)\n")
	}

	return table.String()
}

func (s *State) LogCurrentStateReport(ctx context.Context) {
	var result strings.Builder

	// Summary counts
	result.WriteString("=== RUNTIME STATE SUMMARY ===\n")
	result.WriteString(fmt.Sprintf("IOs: %d open, %d closed\n", s.openIOs.Len(), s.closedIOs.Len()))
	result.WriteString(fmt.Sprintf("Consoles: %d open, %d closed\n", s.openConsoles.Len(), s.closedConsoles.Len()))
	result.WriteString(fmt.Sprintf("Vsock Connections: %d open, %d closed\n", s.openVsockConnections.Len(), s.closedVsockConnections.Len()))
	result.WriteString(fmt.Sprintf("Unix Connections: %d open, %d closed\n", s.openUnixConnections.Len(), s.closedUnixConnections.Len()))
	result.WriteString("\n")

	// Detailed tables for each resource type
	result.WriteString("=== DETAILED RESOURCE TABLES ===\n\n")

	// IO resources
	result.WriteString(buildStringTable("Open IOs", s.openIOs, "active"))
	result.WriteString("\n")
	result.WriteString(buildStringTable("Closed IOs", s.closedIOs, "closed"))
	result.WriteString("\n")

	// Console resources
	result.WriteString(buildStringTable("Open Consoles", s.openConsoles, "active"))
	result.WriteString("\n")
	result.WriteString(buildStringTable("Closed Consoles", s.closedConsoles, "closed"))
	result.WriteString("\n")

	// Vsock resources
	result.WriteString(buildUint32Table("Open Vsock Connections", s.openVsockConnections, "active"))
	result.WriteString("\n")
	result.WriteString(buildUint32Table("Closed Vsock Connections", s.closedVsockConnections, "closed"))
	result.WriteString("\n")

	// Unix resources
	result.WriteString(buildStringTable("Open Unix Connections", s.openUnixConnections, "active"))
	result.WriteString("\n")
	result.WriteString(buildStringTable("Closed Unix Connections", s.closedUnixConnections, "closed"))

	stateReport := result.String()

	// Log the comprehensive state report
	slog.InfoContext(ctx, "runtime state report",
		"report", stateReport,
		"summary", map[string]interface{}{
			"openIOs":                s.openIOs.Len(),
			"closedIOs":              s.closedIOs.Len(),
			"openConsoles":           s.openConsoles.Len(),
			"closedConsoles":         s.closedConsoles.Len(),
			"openVsockConnections":   s.openVsockConnections.Len(),
			"closedVsockConnections": s.closedVsockConnections.Len(),
			"openUnixConnections":    s.openUnixConnections.Len(),
			"closedUnixConnections":  s.closedUnixConnections.Len(),
		})

}
