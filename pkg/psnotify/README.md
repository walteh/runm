# Process Notification Detector

This package provides enhanced process notifications by wrapping the `psnotify` package.

## Features

-   **Enhanced Events**: Enriches process events with additional metadata such as command line arguments, executable path, cgroup information, and parent-child relationships.
-   **Combined Events**: Provides a combined fork+exec event to track process creation more effectively.
-   **Process Lifecycle**: Detects when processes are fully complete using "done" events that wait for file descriptors to close.
-   **Go-Runc Integration**: Optional integration with go-runc's exit channel.
-   **Cross-Platform Support**: Primary functionality on Linux with graceful degradation on other platforms.

## Usage

```go
// Create a new detector
detector, err := psnotify.NewDetector(psnotify.DetectorConfig{})
if err != nil {
    return err
}
defer detector.Close()

// Watch a process for events
err = detector.Watch(pid, psnotify.PROC_EVENT_FORK | psnotify.PROC_EVENT_EXEC | psnotify.PROC_EVENT_EXIT)
if err != nil {
    return err
}

// Listen for events
for {
    select {
    // Combined fork+exec events
    case event := <-detector.ForkExec:
        fmt.Printf("Process %d forked and exec'd child %d\n",
            event.Fork.ParentPid, event.Fork.ChildPid)

        // Access child info
        if event.ChildInfo != nil {
            fmt.Printf("Child command: %v\n", event.ChildInfo.Argv)
            fmt.Printf("Child executable: %s\n", event.ChildInfo.Argc)
        }

    // Process completion events
    case event := <-detector.Done:
        fmt.Printf("Process %d exited with code %d\n",
            event.Exit.Pid, event.Exit.ExitCode)

    // Error events
    case err := <-detector.Error:
        fmt.Printf("Error: %v\n", err)
    }
}
```

## Integration with go-runc

The detector can be configured to send process exit events to a go-runc exit channel:

```go
// Create exit channel
exitChan := make(chan gorunc.Exit, 10)

// Create detector with exit channel function
detector, err := psnotify.NewDetector(psnotify.DetectorConfig{
    ExitChannelFunc: func() chan gorunc.Exit { return exitChan },
})

// Now both detector.Done and exitChan will receive process exit events
```

## Platform Support

-   **Linux**: Full functionality with cgroup information and reliable file descriptor tracking.
-   **Other platforms**: Basic functionality with graceful degradation of Linux-specific features.

## Testing

The package includes a mock watcher for testing in non-Linux environments:

```go
// Create a mock watcher
mock := psnotify.NewMockWatcher()

// Create detector with mock
detector, _ := psnotify.NewDetector(psnotify.DetectorConfig{
    Watcher: mock,
})

// Send test events to the detector
mock.SendFork(&psnotify.ProcEventFork{...})
mock.SendExec(&psnotify.ProcEventExec{...})
mock.SendExit(&psnotify.ProcEventExit{...})
```

## License

Copyright (c) 2012 VMware, Inc.
