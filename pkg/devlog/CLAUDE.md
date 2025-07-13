# Devlog Abstraction Project Progress

## Project Overview

This project abstracts the current `slogdevterm` package into a more general `devlog` system that separates data collection from formatting. The goal is to improve efficiency by avoiding duplicate formatting across multiple processes.

**Current State Analysis (2025-07-13):**
- Examined `pkg/logging/slogdevterm/termlog.go` - comprehensive terminal logger with styling, coloring, and complex formatting
- Reviewed `proto/devlog/v1/devlog.proto` - currently has minimal empty StructuredLog and RawLog messages  
- The slogdevterm package handles: time formatting, level styling, source info, caller info, error tracing, multiline boxes, debug pattern coloring, attribute processing with groups

**Target Architecture:**
```
slog.Handler -> devlog.Producer -> devlog.Consumer -> devlog.Handler
```

## Progress Tracking

### ✅ Completed
- [x] Examine current slogdevterm implementation 
- [x] Review proto/devlog/v1/devlog.proto definitions
- [x] Create progress tracking CLAUDE.md
- [x] Update protobuf definitions with comprehensive log data structure
- [x] Create pkg/devlog/ core types (Handler, Consumer, Producer interfaces)
- [x] Create pkg/devlog/slogbridge/ for slog.Handler integration

### 🚧 In Progress
- [ ] Create pkg/devlog/console/ with formatting logic from slogdevterm

### 📋 Todo
- [ ] Create pkg/devlog/stack/ utilities (refactor from pkg/stackerr)
- [ ] Create pkg/devlog/rawbridge/ for raw log handling
- [ ] Create pkg/devlog/netingest/ with time-based log ordering (100ms default, configurable)
- [ ] Create pkg/devlog/netforward/ and pkg/devlog/netexport/
- [ ] Create pkg/devlog/otel/ for OTEL export functionality
- [ ] Integrate devlog with pkg/logging package
- [ ] Update test/integration library to use devlog
- [ ] Enhance console caller display with smart path truncation

## Key Data Requirements from slogdevterm Analysis

The devlog system needs to capture:

1. **Basic Log Data:**
   - Level (Debug, Info, Warn, Error)
   - Timestamp 
   - Message (with multiline support)
   - Logger name
   - Language/runtime identifier

2. **Source Information:**
   - Caller information (file, line, function)
   - Stack traces for errors
   - Enhanced source formatting

3. **Attributes:**
   - Key-value pairs with grouping support
   - Error objects with special handling
   - JSON value detection
   - Complex object serialization

4. **Styling Context:**
   - Debug pattern matching for coloring
   - OS icons
   - Color profiles
   - Hyperlink support

5. **Process Metadata (for raw logs):**
   - PID, hostname, timestamp
   - File descriptor info
   - stdout/stderr detection

## Design Decisions (Answered)

1. **Network Protocol:** Support both streaming and batch processing in gRPC service
2. **Time Ordering:** 100ms default timeout for netingest, configurable via options pattern (like pkg/taskgroup)
3. **Raw Log Metadata:** Include PID, hostname, timestamp, fd info, stdout/stderr detection
4. **Backwards Compatibility:** Keep slogdevterm and stackerr untouched, add new funcs in pkg/logging
5. **Standard Formats:** Build for seamless OTEL support with native log types, add otel export package later

## Enhanced Architecture Plan

```
                           ┌─────────────────┐
                           │   slog.Record   │
                           └─────────┬───────┘
                                     │
                           ┌─────────▼───────┐
                           │ devlog.Producer │
                           └─────────┬───────┘
                                     │
                           ┌─────────▼───────┐     ┌──────────────┐
                           │ devlog.Consumer │────▶│ devlog.OTEL  │
                           └─────────┬───────┘     └──────────────┘
                                     │
                           ┌─────────▼───────┐
                           │ devlog.Handler  │
                           └─────────────────┘
                                     │
                    ┌────────────────┼────────────────┐
                    ▼                ▼                ▼
            ┌─────────────┐  ┌─────────────┐  ┌─────────────┐
            │   Console   │  │   Network   │  │    File     │
            └─────────────┘  └─────────────┘  └─────────────┘
```

## Next Session Actions

1. **Priority 1:** Update protobuf definitions to include all necessary fields
2. **Priority 2:** Create core devlog interfaces (Handler, Consumer, Producer)  
3. **Priority 3:** Create slogbridge to convert slog.Record to devlog format
4. **Priority 4:** Implement options pattern for configuration (like pkg/taskgroup)