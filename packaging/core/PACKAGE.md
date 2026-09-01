# Stackfort release carrier

This native package installs one immutable Stackfort release under
`/usr/lib/stackfort/releases/<version>` and the root-only orchestration entry
point `/usr/sbin/stackfort-install`.

Installing the package does not configure the host, start services, create
accounts, or alter `/etc`, `/var`, `/srv`, or existing Stackfort data. Run the
read-only preflight first, then explicitly confirm installation:

```sh
sudo stackfort-install preflight
sudo stackfort-install --yes
```

Removing this carrier package removes only its packaged release source and
wrapper. It deliberately does not uninstall a configured Stackfort instance or
delete customer data.
