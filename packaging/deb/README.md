# Debian packaging

Stackfort publishes one architecture-specific `stackfort-release` DEB for both
Debian 13 and Ubuntu 26.04 LTS. It carries the immutable release tree under
`/usr/lib/stackfort/releases/<version>` and does not configure the host from a
package maintainer script.

See the [shared native package contract](../core/README.md). Distribution-bound
Coraza and Vinyl packages continue to be built separately and are selected by
the journaled installer after host detection.
