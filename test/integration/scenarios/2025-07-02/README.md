# 2025-07-02 Complex TypeScript Scenario

This scenario tests a comprehensive container runtime environment with a complex TypeScript project that exercises multiple aspects of the container runtime.

## What This Tests

### 🏗️ **Multi-Module TypeScript Project**

-   `src/index.ts` - Main entry point with async orchestration
-   `src/calculator.ts` - Mathematical computations (Fibonacci, prime factorization)
-   `src/fileProcessor.ts` - File system operations and JSON handling
-   `tests/calculator.test.ts` - Unit tests using Bun's test framework

### 🔄 **Full Development Workflow**

1. **Dependency Installation** - `bun install` downloads chalk package
2. **Testing** - `bun test` runs unit tests
3. **Compilation** - `bun build` compiles TypeScript to JavaScript
4. **Execution** - `bun run start` runs the compiled application

### 🎯 **Container Runtime Features Tested**

-   **File System Operations**: Creating/reading 50 JSON files
-   **Memory Usage**: Processing arrays and concurrent operations
-   **CPU Usage**: Mathematical computations and compilation
-   **Network Simulation**: Promise-based async operations (no external network)
-   **Container Mounting**: Host directory mounted into container
-   **TypeScript Compilation**: Full build pipeline within container

## Expected Output

The scenario should complete within 30 seconds and produce output showing:

-   📦 Dependency installation progress
-   🧪 Test results (all tests passing)
-   🔨 Build completion
-   🚀 Application execution with progress indicators
-   📊 Computational results (fibonacci calculations)
-   📁 File operation results (50 files created)
-   ✅ Simulated API call responses
-   🎉 Completion message

## Files Structure

```
test/integration/scenarios/2025-07-02/
├── package.json              # Project configuration and dependencies
├── src/
│   ├── index.ts              # Main application entry point
│   ├── calculator.ts         # Mathematical operations module
│   └── fileProcessor.ts      # File system operations module
└── tests/
    └── calculator.test.ts    # Unit tests for calculator module
```

This scenario provides a realistic development workload that thoroughly tests the container runtime's ability to handle complex applications with file I/O, computation, and build processes.
