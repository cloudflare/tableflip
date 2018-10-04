// Package tableflip implements zero downtime upgrades.
//
// An upgrade spawns a new copy of argv[0] and passes
// file descriptors of used listening sockets to the new process. The old process exits
// once the new process signals readiness. Thus new code can use sockets allocated
// in the old process. This is similar to the approach used by nginx, but
// as a library.
//
// At any point in time there are one or two processes, with at most one of them
// in non-ready state. A successful upgrade fully replaces all old configuration
// and code.
//
// To use this library with systemd you need to use the PIDFile option in the service
// file.
//
//    [Unit]
//    Description=Service using tableflip
//
//    [Service]
//    ExecStart=/path/to/binary
//    PIDFile=/path/to/pid-file
//
// Then pass /path/to/pid-file to New.
//
// NOTE: Requires at least Go 1.9, since there is a race condition on the
// pipes used for communication between parent and child.
package tableflip
