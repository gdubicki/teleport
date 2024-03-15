---
authors: Russell Jones (rjones@goteleport.com) and Bernard Kim (bernard@goteleport.com)
state: draft
---

# RFD 0144 - Automatic Upgrades for Client Tools

## Required Approvers

* Engineering: @sclevine && @bernardjkim && @r0mant
* Product: @klizhentas || @xinding33
* Security: @reedloden

## What

This RFD defines how support for automatic upgrades will be added to Teleport
client tools like `tsh` and `tctl`.

## Why

Automatic upgrades for client tools solves the following problems for users of
`tsh` and `tctl`.

* Security. For Teleport Cloud users, both `tsh` and `tctl` will always be
  running a release with no known security vulnerabilities. For self-hosted
  users, Teleport administrators will be able to quickly push out security
  vulnerability free releases to `tsh` and `tctl` users.
* Compatibility. Users will no longer have to learn about and understand
  [Component compatibility](https://goteleport.com/docs/upgrading/overview/#component-compatibility)
  rules when using Teleport client tools.
* Maintenance. Users will no longer have to perform manual maintenance
  operations (like updating packages) or configure system package managers to
  perform automatic updates to stay on the latest version of client tools.
  However, this will continue to be an option of the user decides.
* Cross-cluster compatibility. Users will no longer have to maintain multiple
  versions of client tools to access clusters running different versions of
  Teleport.
* Reduced download complexity. Users will always get releases of `tsh` and
  `tctl` with all features (like TouchID support) built-in.

## Details

### Use cases

Two main use cases will be supported. Customers that want enroll in automatic
updates for client tools and those that want to manually manage updates.

#### Automatic Updates

The first time a user performs a `tsh login` with a version of `tsh` that
supports automatic updates, the user will be prompted to enroll into automatic
updates.


```
$ tsh --proxy=proxy.example.com login
Enroll client tools like tsh and tctl into automatic updates? [YES/no]
TODO(russjons): Sell automatic updates here.

[...]
```

If the user opts to enroll into automatic updates, on all future logins if the
client tools binary in the users path is not the version recommended by the
server, client tools will automaticall download and install the cirrect
version.

```
$ tsh login --proxy=proxy.example.com
Updating client tools to vX.Y.Z.
Update progress: [▒▒▒▒▒▒     ] (Ctrl-C to Cancel)

[...]
```

The user will be able to cancel client tools update. This may be needed for example
if they are not a LTE connection or public WiFi.

```
$ tsh login --proxy=proxy.example.com
Updating client tools to vX.Y.Z.
Update progress: [▒▒▒▒▒▒     ] (Ctrl-C to Cancel)
WARNING: Client tools failed to update, some functionality maybe broken.

[...]
```




```
$ tsh login --proxy=proxy.example.com

[...]

Your version of tsh is out of date.
Automatically update the version of tsh? [YES/no]

[continue login process]
```

* Users that want to install Teleport and not have to think about the version
  they are running. It's estimated 



* Users that want to pin client tools to a speicific version. This may be
  because client tools are being used in CI/CD infrastuctuer or device
  managment (like Jamf) tightly controls what version of software can be
  installed on a device.




### Implementation

#### Server Side changes

TODO(russjones): Should this endpoint be unauthenticated? Should it just
be served off the point endpoint or should we put it elsewhere?

TODO(russjones): (Teradata) Track state of upgrades within schedule resource?

https://github.com/gravitational/teleport/pull/22850/files
message ClusterMaintenanceConfigSpecV1 {
message AgentUpgradeWindow {
  // UTCStartHour is the start hour of the maintenance window in UTC.
  uint32 UTCStartHour = 1 [(gogoproto.jsontag) = "utc_start_hour"];
  // Weekdays is an optional list of weekdays. If not specified, an agent upgrade window
  // occurs every day.
  repeated string Weekdays = 2 [(gogoproto.jsontag) = "weekdays,omitempty"];


```yaml
kind: version_directive (or version-controller or version-control-config)
version: v3
metadata:
  name: version-directive
spec:
  upgrade_window_start: "08:00:00"


  # server_version is a semver formatted strings that represents the version
  # of Teleport all agents should be running.
  server_version: x.y.z

  # client_version is a semver formatted strings that represents the version
  # of Teleport all agents should be running.
  client_version: a.b.c

  # TODO(russjones): upgrade window?
```

This resource will be replicated to the proxy cache.

TODO(russjones): This resource will be updated from the Cloud CRD to here how?

The `client_version` field will be replicated to the unauthenticated endpoint
`/v1/webapi/ping`. As the snippet below shows, this endpoint already has the
exact server version available, so this does not increase the risk anymore than
already.

The `server_version` is undecided and will be decided in RFD #?

the ping endpoint will be updated to serve a client version.

the ping end point will be updatable via the following resource.

TODO(russjones): Create a new resource. Do we have an exisiting one?

#### client side changes

TODO(russjones): Understand where is the earliest in the login process that we
can perform this, maybe even authenticated?

Users will be able to do the initial install of client tools using whatever
mechanism they prefer (compile, tarball or packages or a package manager).

aiutomatic upgrades will kick in during login process. during login, teleport
will hit the above endpoint. if the version in the endpoint is strictly greater
than the version of the binary, teleport download the apprpriate tarball and
place it in `~/.tsh/bin`.

TODO(russjones): cache
TODO(russjones): .tsh/config
TODO(russjones): environment variables
TODO(russjones): full toolchain.
TODO(russjones): Metrics.

### User experience

If the user does not opt ito automatic updates:

```
$ tsh login --proxy=proxy.example.com

[...]

Your version of tsh is out of date.
Automatically update the version of tsh? [YES/no]

WARNING: Your version of tsh is out of date. Some functionality may not work.
Run "apt-get update teleport" to manually update your package or run
"tsh enroll-updates" to automatically keep your agent updated.

[attempt to login]
```

### Security

The initial version will rely on TLS to establish connection authenticity to
the Tlepeort download server.

Phase 2 will use The Upgrade Framework (TUF) to implement secure upgrades.

### Inspiration

https://go.dev/doc/toolchain
