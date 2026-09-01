# RPM packaging

Stackfort publishes an `x86_64` `stackfort-release` RPM for Rocky Linux 10. It
carries the immutable release tree under `/usr/lib/stackfort/releases/<version>`
and does not configure the host from an RPM scriptlet. SELinux policy and file
contexts remain owned by the journaled installer because they describe mutable
host integration rather than passive package content.

See the [shared native package contract](../core/README.md).
