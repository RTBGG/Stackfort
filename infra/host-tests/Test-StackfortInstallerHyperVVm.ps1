# SPDX-License-Identifier: AGPL-3.0-or-later

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [ValidateSet('debian-13', 'ubuntu-26.04', 'rocky-10')]
    [string] $ImageId,

    [ValidatePattern('^[a-z0-9][a-z0-9-]{2,62}$')]
    [string] $VmName,

    [ValidatePattern('^[a-z_][a-z0-9_-]{0,31}$')]
    [string] $GuestUser = 'stackfort-test',

    [ValidatePattern('^[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$')]
    [string] $Version = '0.0.0-dev',

    [string] $VmRoot = 'C:\ProgramData\Stackfort\Hyper-V',
    [string] $WafPackageDirectory,
    [TimeSpan] $StartupTimeout = [TimeSpan]::FromMinutes(5),
    [switch] $SkipBuild,
    [switch] $RunPhase1Suite
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if ([string]::IsNullOrWhiteSpace($VmName)) {
    $VmName = "stackfort-$($ImageId.Replace('.', '-'))"
}
$vm = Get-VM -Name $VmName -ErrorAction Stop
if ($vm.State -ne 'Running') {
    Start-VM -Name $VmName
}

$keyPath = Join-Path (Join-Path $VmRoot 'keys') 'stackfort-host-test-ed25519'
if (-not (Test-Path -LiteralPath $keyPath -PathType Leaf)) {
    throw "Stackfort host-test SSH key not found: $keyPath"
}
$knownHosts = Join-Path $VmRoot 'known_hosts'
$sshOptions = @(
    '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=5',
    '-o', 'StrictHostKeyChecking=accept-new', '-o', "UserKnownHostsFile=$knownHosts",
    '-o', "HostKeyAlias=$VmName", '-o', 'LogLevel=ERROR', '-i', $keyPath
)

$deadline = (Get-Date).Add($StartupTimeout)
$address = $null
do {
    $adapter = Get-VMNetworkAdapter -VMName $VmName
    $address = $adapter.IPAddresses |
        Where-Object { $_ -match '^\d{1,3}(\.\d{1,3}){3}$' -and $_ -notlike '169.254.*' } |
        Select-Object -First 1
    if (-not $address) {
        $mac = $adapter.MacAddress -replace '(.{2})(?!$)', '$1-'
        $interfaceAlias = "vEthernet ($($adapter.SwitchName))"
        $address = Get-NetNeighbor -InterfaceAlias $interfaceAlias -AddressFamily IPv4 -ErrorAction SilentlyContinue |
            Where-Object { $_.LinkLayerAddress -eq $mac -and $_.State -ne 'Unreachable' } |
            Select-Object -ExpandProperty IPAddress -First 1
    }
    if (-not $address) { Start-Sleep -Seconds 2 }
} while (-not $address -and (Get-Date) -lt $deadline)
if (-not $address) {
    throw "No IPv4 address was found for $VmName before the startup timeout."
}

$sshReady = $false
do {
    try {
        & ssh.exe @sshOptions "${GuestUser}@${address}" 'true' 2>$null
        $sshReady = $LASTEXITCODE -eq 0
    } catch {
        # A restored guest may publish its previous address before sshd is up.
        $sshReady = $false
    }
    if (-not $sshReady) { Start-Sleep -Seconds 2 }
} while (-not $sshReady -and (Get-Date) -lt $deadline)
if (-not $sshReady) {
    throw "SSH did not become ready for $VmName as $GuestUser before the startup timeout."
}

$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
if (-not $SkipBuild) {
    $gitBash = Join-Path $env:ProgramFiles 'Git\bin\bash.exe'
    if (-not (Test-Path -LiteralPath $gitBash -PathType Leaf)) {
        throw "Git Bash is required to build the release: $gitBash"
    }
    $previousVersion = $env:VERSION
    $previousWafPackageDirectory = $env:STACKFORT_WAF_PACKAGE_DIR
    try {
        $env:VERSION = $Version
        if ([string]::IsNullOrWhiteSpace($WafPackageDirectory)) {
            $WafPackageDirectory = Join-Path $repositoryRoot 'infra\host-tests\work\waf-packages'
        }
        $env:STACKFORT_WAF_PACKAGE_DIR = (Resolve-Path -LiteralPath $WafPackageDirectory).Path
        Push-Location $repositoryRoot
        try {
            & $gitBash 'scripts/build-release.sh'
            if ($LASTEXITCODE -ne 0) {
                throw "Release build failed with exit code $LASTEXITCODE."
            }
        } finally {
            Pop-Location
        }
    } finally {
        if ($null -eq $previousVersion) {
            Remove-Item Env:VERSION -ErrorAction SilentlyContinue
        } else {
            $env:VERSION = $previousVersion
        }
        if ($null -eq $previousWafPackageDirectory) {
            Remove-Item Env:STACKFORT_WAF_PACKAGE_DIR -ErrorAction SilentlyContinue
        } else {
            $env:STACKFORT_WAF_PACKAGE_DIR = $previousWafPackageDirectory
        }
    }
}

$phase1BinaryPath = Join-Path (Join-Path $repositoryRoot 'infra\host-tests\work') "stackfort-phase1-$VmName.test"
if ($RunPhase1Suite) {
    New-Item -ItemType Directory -Path (Split-Path -Parent $phase1BinaryPath) -Force | Out-Null
    $previousGoOS = $env:GOOS
    $previousGoArch = $env:GOARCH
    $previousCgoEnabled = $env:CGO_ENABLED
    try {
        $env:GOOS = 'linux'
        $env:GOARCH = 'amd64'
        $env:CGO_ENABLED = '0'
        Push-Location $repositoryRoot
        try {
            & go.exe test -tags=integration -c -o $phase1BinaryPath ./tests/integration
            if ($LASTEXITCODE -ne 0) {
                throw "Phase 1 integration test build failed with exit code $LASTEXITCODE."
            }
        } finally {
            Pop-Location
        }
    } finally {
        foreach ($variable in @(
            @{ Name = 'GOOS'; Value = $previousGoOS },
            @{ Name = 'GOARCH'; Value = $previousGoArch },
            @{ Name = 'CGO_ENABLED'; Value = $previousCgoEnabled }
        )) {
            if ($null -eq $variable.Value) {
                Remove-Item "Env:$($variable.Name)" -ErrorAction SilentlyContinue
            } else {
                Set-Item "Env:$($variable.Name)" $variable.Value
            }
        }
    }
}

$bundle = "stackfort-$Version-linux-amd64"
$archiveName = "$bundle.tar.gz"
$archivePath = Join-Path (Join-Path $repositoryRoot 'dist') $archiveName
if (-not (Test-Path -LiteralPath $archivePath -PathType Leaf)) {
    throw "Release archive not found: $archivePath"
}
$archiveHash = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
$remoteArchive = "/tmp/$archiveName"
$remoteRoot = "/var/tmp/stackfort-installer-$Version"
$remoteSource = "$remoteRoot/$bundle"

$phpUnit = switch ($ImageId) {
    'debian-13' { 'php8.4-fpm.service' }
    'ubuntu-26.04' { 'php8.5-fpm.service' }
    'rocky-10' { 'php-fpm.service' }
}
$phpBinary = if ($ImageId -eq 'debian-13') { '/usr/sbin/php-fpm8.4' } elseif ($ImageId -eq 'ubuntu-26.04') { '/usr/sbin/php-fpm8.5' } else { '/usr/sbin/php-fpm' }
$phpPackageCheck = if ($ImageId -eq 'rocky-10') { 'sudo rpm -q php-fpm >/dev/null' } elseif ($ImageId -eq 'debian-13') { 'dpkg-query -W -f=''${db:Status-Abbrev}'' php8.4-fpm | grep -q ''^ii''' } else { 'dpkg-query -W -f=''${db:Status-Abbrev}'' php8.5-fpm | grep -q ''^ii''' }
$phpMyAdminPackageCheck = if ($ImageId -eq 'rocky-10') {
    '! sudo rpm -q phpMyAdmin >/dev/null 2>&1; sudo test -f /usr/share/stackfort/phpmyadmin/index.php'
} else {
    'dpkg-query -W -f=''${db:Status-Abbrev}'' phpmyadmin | grep -q ''^ii''; sudo test -f /usr/share/phpmyadmin/index.php'
}
$phpMyAdminConfigCheck = if ($ImageId -eq 'rocky-10') {
    "sudo cmp -s '$remoteSource/phpmyadmin/config.inc.php' /usr/share/stackfort/phpmyadmin/config.inc.php"
} else {
    "sudo cmp -s '$remoteSource/phpmyadmin-integration/config.inc.php' /etc/phpmyadmin/conf.d/99-stackfort.php"
}
$phpMyAdminWorker = if ($ImageId -eq 'rocky-10') { 'nginx' } else { 'www-data' }
$wafPackageCheck = if ($ImageId -eq 'rocky-10') {
    'sudo rpm -q stackfort-waf >/dev/null; sudo rpm -V stackfort-waf >/tmp/stackfort-waf-drift; test ! -s /tmp/stackfort-waf-drift'
} else {
    'dpkg-query -W -f=''${db:Status-Abbrev}'' stackfort-waf | grep -q ''^ii''; sudo dpkg --verify stackfort-waf >/tmp/stackfort-waf-drift; test ! -s /tmp/stackfort-waf-drift'
}
$wafWorker = if ($ImageId -eq 'rocky-10') { 'nginx' } else { 'www-data' }
$wafModule = if ($ImageId -eq 'rocky-10') { '/usr/lib64/nginx/modules/ngx_http_coraza_module.so' } else { '/usr/lib/nginx/modules/ngx_http_coraza_module.so' }
$wafLoader = if ($ImageId -eq 'rocky-10') { '/usr/share/nginx/modules/50-stackfort-coraza.conf' } else { '/etc/nginx/modules-enabled/50-stackfort-coraza.conf' }

& scp.exe @sshOptions $archivePath "${GuestUser}@${address}:${remoteArchive}"
if ($LASTEXITCODE -ne 0) {
    throw "Copying the release archive failed with exit code $LASTEXITCODE."
}

$securityChecks = if ($ImageId -eq 'rocky-10') {
    'sudo test "$(getenforce)" = Enforcing; ' +
    'sudo matchpathcon -V /usr/share/stackfort/web/index.html /usr/share/stackfort/phpmyadmin/index.php /var/lib/stackfort-agent/acme-http01 /etc/stackfort/panel-tls/bootstrap.pem /etc/stackfort/phpmyadmin /var/lib/stackfort-phpmyadmin /var/lib/stackfort-phpmyadmin-broker /etc/stackfort/php /run/stackfort-php; ' +
    'sudo semodule -l | grep -Eq "^stackfort_nginx_panel([[:space:]]|$)"; ' +
    'sudo semanage port -l | grep -E "^stackfort_api_port_t[[:space:]]+tcp" | grep -q 8080; ' +
    'sudo semanage port -l | grep -E "^stackfort_api_port_t[[:space:]]+tcp" | grep -q 8081; ' +
    'test "$(getsebool httpd_can_network_connect | awk ''{print $3}'')" = off; ' +
    'test "$(getsebool httpd_can_network_relay | awk ''{print $3}'')" = off; ' +
    'sudo systemctl is-active --quiet firewalld.service; sudo systemctl is-enabled --quiet firewalld.service; ' +
    'sudo firewall-cmd --query-port=80/tcp; sudo firewall-cmd --query-port=443/tcp; sudo firewall-cmd --query-port=8443/tcp; ' +
    'sudo firewall-cmd --permanent --query-port=80/tcp; sudo firewall-cmd --permanent --query-port=443/tcp; sudo firewall-cmd --permanent --query-port=8443/tcp; '
} else {
    'sudo grep -q "^stackfort-api (enforce)" /sys/kernel/security/apparmor/profiles; ' +
    'sudo systemctl is-active --quiet stackfort-firewall.service; sudo systemctl is-enabled --quiet stackfort-firewall.service; ' +
    'sudo systemctl reload stackfort-firewall.service; ' +
    'sudo nft list table inet stackfort >/dev/null; ' +
    'sudo systemctl show --property=MainPID --value stackfort-api.service | ' +
    'xargs -I{} sudo grep -q "^stackfort-api" /proc/{}/attr/current; '
}

$remoteCommand = @"
set -eu
sudo cloud-init status --wait --long
printf '$archiveHash  $remoteArchive\n' | sha256sum --check --strict -
sudo rm -rf -- '$remoteRoot'
sudo install -d -m 0755 '$remoteRoot'
sudo tar -xzf '$remoteArchive' -C '$remoteRoot' --same-owner --same-permissions
sudo chmod 0755 '$remoteSource/bin/stackfort-api' '$remoteSource/bin/stackfort-agent' '$remoteSource/bin/stackfort-installer'
if ! sudo '$remoteSource/bin/stackfort-installer' install --source-dir='$remoteSource' --yes --format=json >/tmp/stackfort-install-first.json 2>/tmp/stackfort-install-first.err; then
  cat /tmp/stackfort-install-first.err /tmp/stackfort-install-first.json
  exit 1
fi
grep -q '"status": "complete"' /tmp/stackfort-install-first.json
sudo cp /var/lib/stackfort-installer/install-state.json /tmp/stackfort-install-journal-before
if ! sudo '$remoteSource/bin/stackfort-installer' install --source-dir='$remoteSource' --yes --format=json >/tmp/stackfort-install-second.json 2>/tmp/stackfort-install-second.err; then
  cat /tmp/stackfort-install-second.err /tmp/stackfort-install-second.json
  exit 1
fi
sudo cmp -s /tmp/stackfort-install-journal-before /var/lib/stackfort-installer/install-state.json
grep -q '"status": "complete"' /tmp/stackfort-install-second.json
grep -q '"alreadyInstalled": true' /tmp/stackfort-install-second.json
grep -q '"changed": false' /tmp/stackfort-install-second.json
sudo stat -Lc '%a %u:%g' /var/lib/stackfort-installer/install-state.json | grep -qx '600 0:0'
sudo stat -Lc '%a %U:%G' /usr/local/bin/stackfort-api | grep -qx '755 root:root'
sudo stat -Lc '%a %U:%G' /usr/local/sbin/stackfort-agent | grep -qx '755 root:root'
sudo stat -Lc '%a %U:%G' /etc/stackfort/stackfort.env | grep -qx '640 root:stackfort'
sudo stat -Lc '%a %U:%G' /etc/stackfort | grep -qx '751 root:stackfort'
sudo stat -Lc '%a %U:%G' /etc/stackfort/php | grep -qx '750 root:root'
sudo stat -Lc '%a %U:%G' /etc/stackfort/phpmyadmin | grep -qx '750 root:stackfort-pma'
sudo stat -Lc '%a %U:%G' /var/lib/stackfort-phpmyadmin | grep -qx '750 stackfort-pma:stackfort-pma'
sudo stat -Lc '%a %U:%G' /var/lib/stackfort-phpmyadmin/sessions | grep -qx '700 stackfort-pma:stackfort-pma'
sudo stat -Lc '%a %U:%G' /var/lib/stackfort-phpmyadmin/tmp | grep -qx '700 stackfort-pma:stackfort-pma'
sudo stat -Lc '%a %U:%G' /var/lib/stackfort-phpmyadmin-broker | grep -qx '750 stackfort:stackfort-pma'
sudo stat -Lc '%a %U:%G %s' /var/lib/stackfort-phpmyadmin-broker/broker.key | grep -qx '640 stackfort:stackfort-pma 32'
sudo stat -Lc '%a %U:%G %s' /var/lib/stackfort-phpmyadmin/blowfish.key | grep -qx '600 stackfort-pma:stackfort-pma 32'
sudo stat -Lc '%a %U:%G' /etc/stackfort/panel-tls/bootstrap.pem | grep -qx '600 root:root'
sudo stat -Lc '%a %U:%G' /run/stackfort-php | grep -qx '755 root:root'
sudo stat -Lc '%a %U:%G' /etc/nginx/stackfort/panel-enabled/00-panel.conf | grep -qx '640 root:root'
sudo stat -Lc '%a %U:%G' /usr/share/stackfort/web/index.html | grep -qx '644 root:root'
sudo systemctl is-active --quiet stackfort-agent.service stackfort-api.service stackfort-phpmyadmin.service nginx.service mariadb.service
sudo systemctl is-enabled --quiet stackfort-agent.service stackfort-api.service stackfort-phpmyadmin.service nginx.service mariadb.service
sudo test -x /usr/bin/mariadb
$wafPackageCheck
sudo stat -Lc '%a %U:%G' '$wafModule' | grep -qx '755 root:root'
sudo stat -Lc '%a %U:%G' '$wafLoader' | grep -qx '644 root:root'
sudo stat -Lc '%a %U:%G' /usr/share/doc/stackfort-waf/qualification/manifest.json | grep -qx '644 root:root'
sudo stat -Lc '%a %U:%G' /var/cache/stackfort/coraza | grep -qx '755 root:root'
sudo stat -Lc '%a %U:%G' /var/cache/stackfort/coraza/data | grep -qx '700 ${wafWorker}:${wafWorker}'
sudo nginx -t
$phpPackageCheck
$phpMyAdminPackageCheck
$phpMyAdminConfigCheck
sudo test -x '$phpBinary'
! sudo systemctl is-active --quiet '$phpUnit'
! sudo systemctl is-enabled --quiet '$phpUnit'
sudo systemctl show --property=NoNewPrivileges --value stackfort-agent.service | grep -qx yes
sudo systemctl show --property=NoNewPrivileges --value stackfort-api.service | grep -qx yes
sudo systemctl show --property=PrivateDevices --value stackfort-agent.service | grep -qx yes
sudo systemctl show --property=PrivateDevices --value stackfort-api.service | grep -qx yes
sudo systemctl show --property=NoNewPrivileges --value stackfort-phpmyadmin.service | grep -qx yes
sudo systemctl show --property=PrivateDevices --value stackfort-phpmyadmin.service | grep -qx yes
sudo systemctl show --property=ProtectSystem --value stackfort-phpmyadmin.service | grep -qx strict
sudo systemctl show --property=User --value stackfort-phpmyadmin.service | grep -qx stackfort-pma
sudo systemctl show --property=Group --value stackfort-phpmyadmin.service | grep -qx '${phpMyAdminWorker}'
sudo stat -Lc '%a %U:%G' /run/stackfort-phpmyadmin/phpmyadmin.sock | grep -qx '660 stackfort-pma:${phpMyAdminWorker}'
sudo /usr/bin/php -l /etc/stackfort/phpmyadmin/signon.php >/dev/null
sudo /usr/bin/php -l /etc/stackfort/phpmyadmin/stackfort-launch.php >/dev/null
sudo ss -ltnH | grep -Eq '127[.]0[.]0[.]1:8081([[:space:]]|$)'
sudo runuser --user stackfort -- /usr/bin/curl --fail --silent --max-time 2 --unix-socket /run/stackfort/agent.sock http://localhost/v1/health >/dev/null
curl --fail --silent --max-time 2 http://127.0.0.1:8080/api/v1/health >/dev/null
sudo curl --fail --silent --max-time 2 --cacert /etc/stackfort/panel-tls/bootstrap.pem https://127.0.0.1:8443/ | grep -q 'id="app"'
sudo curl --fail --silent --max-time 2 --cacert /etc/stackfort/panel-tls/bootstrap.pem https://127.0.0.1:8443/api/v1/health | grep -q '"status":"ok"'
sudo curl --silent --max-time 2 --cacert /etc/stackfort/panel-tls/bootstrap.pem --output /dev/null --dump-header /tmp/stackfort-phpmyadmin.headers https://127.0.0.1:8443/phpmyadmin/stackfort-launch.php
grep -Eq '^HTTP/[0-9.]+ 303' /tmp/stackfort-phpmyadmin.headers
grep -Eiq '^Location: /[[:space:]]*$' /tmp/stackfort-phpmyadmin.headers
grep -Eiq '^Cache-Control: no-store' /tmp/stackfort-phpmyadmin.headers
$securityChecks
cat /tmp/stackfort-install-first.json
cat /tmp/stackfort-install-second.json
"@

$validationScript = $remoteCommand.Replace("`r", '')
$validationEncoded = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($validationScript))
$validationOutput = & ssh.exe @sshOptions "${GuestUser}@${address}" `
    "printf %s $validationEncoded | base64 --decode | bash"
if ($LASTEXITCODE -ne 0) {
    $validationOutput | Write-Output
    throw "Installer validation failed with exit code $LASTEXITCODE."
}
$validationOutput | Write-Output

$phase1Status = 'not-run'
if ($RunPhase1Suite) {
    $remotePhase1Binary = '/tmp/stackfort-phase1.test'
    & scp.exe @sshOptions $phase1BinaryPath "${GuestUser}@${address}:${remotePhase1Binary}"
    if ($LASTEXITCODE -ne 0) {
        throw "Copying the Phase 1 integration test failed with exit code $LASTEXITCODE."
    }
    $phase1Output = & ssh.exe @sshOptions "${GuestUser}@${address}" `
        "chmod 0755 '$remotePhase1Binary' && sudo env STACKFORT_DISPOSABLE_HOST_TEST=1 '$remotePhase1Binary' -test.v -test.timeout=12m" 2>&1
    $phase1ExitCode = $LASTEXITCODE
    $phase1Output | Write-Output
    if ($phase1ExitCode -ne 0) {
        throw "Phase 1 qualification failed with exit code $phase1ExitCode."
    }
    $phase1Text = $phase1Output -join "`n"
    foreach ($evidence in @(
        'STACKFORT_QUALIFICATION cross-account-domain-isolation=passed',
        'STACKFORT_QUALIFICATION filesystem-isolation-and-quota=passed',
        'STACKFORT_QUALIFICATION file-manager-navigation=passed',
        'STACKFORT_QUALIFICATION php-account-pool-isolation=passed',
        'STACKFORT_QUALIFICATION php-account-pool-observability=passed',
        'STACKFORT_QUALIFICATION mariadb-tenant-lifecycle=passed',
        'STACKFORT_QUALIFICATION injected-nginx-promotion-recovery=passed',
        'STACKFORT_QUALIFICATION private-acme-agent-rpc-lifecycle=passed',
        '"name":"nginx-static"',
        '"name":"control-api-health"'
    )) {
        if (-not $phase1Text.Contains($evidence)) {
            throw "Phase 1 qualification output lacks required evidence: $evidence"
        }
    }
    $phase1Status = 'passed'
}

[pscustomobject]@{
    VMName = $VmName
    ImageId = $ImageId
    IPv4 = $address
    ReleaseVersion = $Version
    FirstInstall = 'passed'
    JournalResumeContract = 'passed'
    NoOpRerun = 'passed'
    FileMetadata = 'passed'
    SystemdSandbox = 'passed'
    Firewall = 'passed'
    MandatoryAccessControl = 'passed'
    ServiceHealth = 'passed'
    WAFNativePackage = 'passed'
    Phase1DomainLifecycle = $phase1Status
    Phase1AccountIsolation = $phase1Status
    Phase2PHPAccountPools = $phase1Status
    Phase2MariaDBLifecycle = $phase1Status
    Phase2PHPMyAdminSignon = 'passed'
    Phase1InjectedRecovery = $phase1Status
    Phase1PrivateAcmeLifecycle = $phase1Status
    Phase1PerformanceBaseline = $phase1Status
}
