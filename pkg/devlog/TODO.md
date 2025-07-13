# 2025-07-13_abstract-slogdevterm-to-devlog

## STEP 1: ABSTRACT

high level abstractions

-   [ ] create a devlog struct that contains json level data of all the info we currently take/need in slogdevterm
-   [ ] remove slog dependacy from slogdevterm by using the devlog struct

IMPORTANT: devlog needs to not jsut support structured logging, but raw logging as well -

IMPORTANT: if there is any standard logging format used by lots of different people, then we should make sure our devlogs always suppoort that and use existing metadata fields to carry extra details as needed.

PROBLEM: we are using slogdevterm in allll our many process right now, and each one is doing all the formatting and coloring and stuff even if they are forwarding the logs via a feww places to different systems.
right now, the containerd-test program is the only one actually logging any slogdevterm logs.
we want to restructure and abstract things so that the other apps can use a new thing called devlog to get the info to containerd but not the formatting, that way containerd-test (or whoever down the line) can decide how to show it to the user.

flow: slog.Handler -> devlog.Producer -> devlog.Consumer -> devlog.Handler

NOTE: lets keep the slogdevterm package for now as reference

NOTE: communication b etween the ingester and exporter can use the grpc service defined in proto/devlog/v1/devlog.proto - regenerate the StructuredLog and RawLog messages to be more useful for devlog and use 'go generate .' to update the go code related to them.

current package structure:

-   runm/pkg/logging/slogdevterm

enhanced package structure:

-   runm/pkg/devlog/... [core types (Handler, Consumer, Producer)]

-   runm/pkg/devlog/console/...[current coloring and other formatting logic with lipgloss currently in slogdevterm] (devlog.Handler)
-   runm/pkg/devlog/slogbridge/ ... [slog handler that converts to devlog and writes it to some devlog producer] (slog.Handler -> devlog.Handler) - a large focus here needs to be on making sure sufficient caller info and stack traces are passed to maintain the existing functionality of slogdevterm.
-   runm/pkg/devlog/stack/... right now the ./pkg/stackerr was kind of thrown together and also includes not just error stuff but caller things as well, we want to make the stack things nativly useful for devlog (don't delete the current one)
-   runm/pkg/devlog/rawbridge/ ... [raw handler that converts to devlog and writes it to some devlog producer] (io.Writer () -> devlog.Handler) - it should inject some metadata into the raw log to make it easier to identify the source of the log, maybe a name is added at creation or something. the time is also important though. it does not need to support arbitrary data - its designed to point at stdout/stderr that is being displayed to the user. metadata can include process id, fd number, etc. - it should be a devlog just with less info obviously. each write should be considered a new log event. the buffer should be resonably large, lets go with
-   runm/pkg/devlog/netingest/... [system for opening a unix or tcp socket and reading devlog json from it] (devlog.Consumer)
    -   this thing needs to convert the json to devlog structs and then write them to a devlog handler
    -   should accept a devlog.Handler as an argument and write to it
    -   should open up a server on a unix or tcp socket and read devlog json from it
    -   it needs to be able to handle multiple clients and multiple connections
    -   IMPORTANT: its getting logs from different places and it should try to pasas them onto the hanlder in the best order based on time as posssible - so it should take a argument (in ms) of time it should wait before writing to the handler. if it sees a log from a different source that is older than another log received then it reorders them. its safe to follow the simple logic of "once the log at the top has waited x ms then write it to the handler" - this feature will need a ton of testing and should be as simple as possible.
-   runm/pkg/devlog/netforward/... [this doesn't really need to do much with devlog, it just needs to forward the json to a tcp/unix socket]
-   runm/pkg/devlog/netexport/... [(devlog.Handler) that forwards to a tcp/unix socket]
    -   this doens't need any fancy framing, it could be a datagram stream or a delimited stream like we are doing now

NOTE: an additional ffield i wiould like to add to all logs is the 'language' or 'runtime' field to signify the language of the log. since other languages could ahve their own devlog implemenations.

## STEP 2: INTEGRATE WITH pkg/logging

-   currently the whole app relies on a complex mess of 'delimited' and 'raw' loggers, we need to move the project over to use devlog instead. almost all the work is needed in the ./pkg/logging package.
-   the ./test/integraiton library also does a lot of forwarding and receiving we need to adjust that to use devlog as well.

## STEP 3: ENHANCEMENTS TO CONSOLE

-   id like the caller to be able to have a constant width that is truncated in smart ways - big thing here is to take the path (before the file name) and truncate it to a constant width in the most useful way possible by shortining the path as needed (things like chainging certain names to a single character) - and then of course if even that does not work then we might need to shorten the file name as well, likely just needing to add ... in the middle.
-
