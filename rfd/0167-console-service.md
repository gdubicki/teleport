---
authors: Gabriel Corado (gabriel.oliveira@goteleport.com)
state: draft
---

# RFD 0167 - Console service

## Required Approvals

* Engineering: @r0mant && @greedy52
* Security: @reedloden || @jentfoo

## What
This RFD proposes a mechanism for Teleport administrators to switch log-level
and enable profile consumption without restarting the instance.

## Why
During incidents in production environments, debug logs and diagnostic profiles
(such as CPU and Memory allocation) are often unavailable for timely
troubleshooting. To enable those, administrators need to restart the instance,
potentially disrupting the application state and obscuring the issue they're
trying to solve.

## Details

A separate service will be introduced specifically for troubleshooting
purposes. This will ensure the current diagnosis service is kept as is, not
breaking current integrations or introducing behavior changes. The new service
will also always be available (contrasting with the diagnosis service, which
is optional and disabled by default), so in scenarios where users need to use
it, they don't need to restart Teleport.

In addition, the new service will listen into a Unix socket instead of TCP. This
will make discoverability easier and discourage external usage, as the API is
designed for internal usage.

The `pprof` endpoints on diagnostics will still be available for existent
integrations, such as the usage of
`go tool pprof http://diag-addr/debug/pprof/profile`.

### New service

Teleport will start listening using a Unix socket located at
`/tmp/teleport-diag-<node-id>.sock`.

Having the Node ID on the socket name will also cover scenarios where multiple
instances of running on the same machine exist. In this case, the consumers can
rely on the Teleport configuration to locate the data directory and retrieve
the ID.

#### Endpoints

In addition to the `pprof` endpoints, the service will also have an endpoint to
change the applications's log level.

`POST /set-log-level` will receive the new log level on its body as text. The
the level will be parsed using `UnmarshalText` from `slog.Level`, meaning the
the provided level must follow the `slog` format.

Example of usage using `curl`:
```bash
$ curl -x POST -d 'DEBUG' <diag-addr>/set-log-level
OK
```

The log level change will consist on updating `slog` log level and `logrus`
logger (legacy):
- `slog`: Pass a `slog.LevelVar` to the handler. The `slog.LevelVar` is then
   stored where the endpoint can modify it.
- `logrus`: Both `Config` and default logger will need to be updated using
  `SetLevel`.

### `teleport console` command

A new set of commands will be introduced to `teleport` to consume the new
service.

Those commands will have the instance configuration as a common argument. This
is so they can load the configuration to locate the data directory and later
read the Node ID (used for generating the socket name).

Commands will have a common argument for receiving the instance configuration.
This allows loading the configuration to locate the data directory, and later
read the Node ID for generating the socket name.

#### Connecting to the server

Since the service will listen into a Unix domain socket. There will be a few
differences while initializing/configuring the `http.Client`. The client will
need to use a custom transport, which will dial to the socket.

<details>
<summary>Example of HTTP Client/Server connecting through Unix domain socket<summary>

```go
// Server will look like a regular HTTP server, the only difference is that the
// listener will use "unix" network instead of "tcp".
func startServer(socketAddr string) {
	listener, _ := net.Listen("unix", socketAddr)

	// Setup the http server with mux.
	mux := http.NewServeMux()
	// mux.HandleFunc(...)

	server := http.Server{Handler: mux}
	server.Serve(listener)
}

// The client however, requires a different transport. The differences is on the
// DialContext implementation.
func createClient(socketAddr string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketAddr)
			},
		}
	}
}

// While making requests using the client, the users will still require to
// provide the full URL since the client always validate them.
res, err := client.Get("http://console-service/debug/pprof/heap")
```
</details>

#### Changing log level command

Changes the application log level.

`teleport [-c config-path] console set-log-level [LEVEL]`

|Flag|Description|Default value|
|----|-----------|-------------|
|`-c,--config`|Teleport configuration path.|`/etc/teleport.yaml`|
|`LEVEL`|Log level (case-insensitive). Any of: `DEBUG`, `INFO`, `WARN`, `ERROR`|``|

Usage examples:
```bash
$ teleport console set-log-level DEBUG
$ teleport -c /random/teleport.yaml console set-log-level INFO
```

#### Capture `pprof` profiles command

Export the application profile (`pprof` format).

`teleport [-c config-path] console profile [--seconds=] [PROFILE_NAME]`

The `PROFILE_NAME` values follow the Golang's definition on `runtime.Profile`
plus the profiles defined by `net/http/pprof`:
- `allocs`: A sampling of all past memory allocations
- `block`: Stack traces that led to blocking on synchronization primitives
- `cmdline`: The command line invocation of the current program
- `goroutine`: Stack traces of all current goroutines. Use debug=2 as a query parameter to export in the same format as an unrecovered panic.
- `heap`: A sampling of memory allocations of live objects. You can specify the gc GET parameter to run GC before taking the heap sample.
- `mutex`: Stack traces of holders of contended mutexes
- `profile`: CPU profile. You can specify the duration in the seconds GET parameter. After you get the profile file, use the go tool pprof command to investigate the profile.
- `threadcreate`: Stack traces that led to the creation of new OS threads
- `trace`: A trace of execution of the current program. You can specify the duration in the seconds GET parameter. After you get the trace file, use the go tool trace command to investigate the trace.

Note: `--seconds` argument has the same effect as [`seconds` query string](https://pkg.go.dev/net/http/pprof#hdr-Parameters).

|Flag|Description|Default value|
|----|-----------|-------------|
|`-c,--config`|Teleport configuration path.|`/etc/teleport.yaml`|
|`-s,--seconds`|For CPU and trace profiles, profile for the given duration. For other profiles, return a delta profile.|None|
|`PROFILE_NAME`|Profile to be exported. Any of: `allocs`, `block`, `cmdline`, `goroutine`, `heap`, `mutex`, `profile`, `threadcreate`, `trace`|Required|

### Security

#### CPU and Memory consumption during profiling

Profiling, especially CPU and memory profiling, can be resource-intensive. While
capturing these profiles helps diagnose performance bottlenecks, attackers could
leverage them to launch denial-of-service (DoS) attacks. An attacker could
consume significant resources by repeatedly triggering profiles, potentially
slowing down or crashing the Teleport instance.

Given the ease of collecting profiles when using the new service, even regular
usage can impact the instance. With that being said, we’re going to add rate
limiting to the profiling endpoints. To maintain the current debug flow and not
restrict the scenarios where this tooling can be used to solve issues, we have
decided not to impose any limitations on the profiling duration (sampling).

#### Disk space consumption

Extensive logging (particularly debug-level logs) can consume significant disk
space. An attacker could fill the disk, impacting not only Teleport but other
services on the system. To address this, we recommend always turning the log
level back to what is present on the configuration after the troubleshooting
session. Adding a predefined timeout to return it automatically could affect the
debug as there might not be a precise time necessary for the issue to present
itself.
