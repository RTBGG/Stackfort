# SPDX-License-Identifier: AGPL-3.0-or-later
# Test-only installed-product smoke. Dot-source this file; it performs no work on import.
# The caller owns certificate pinning, fresh login, the cookie jar and secret lifetime.
# Do not enable PowerShell tracing/transcription around credentials or HTTP objects.

function Invoke-StackfortInstalledApiSmoke {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)][System.Net.Http.HttpClient] $Client,
        [Parameter(Mandatory)][Uri] $BaseUri,
        [Parameter(Mandatory)][string] $CsrfToken
    )

    Set-StrictMode -Version Latest
    $ErrorActionPreference = 'Stop'
    if (-not $BaseUri.IsAbsoluteUri -or $BaseUri.Scheme -cne 'https' -or
        $BaseUri.UserInfo -ne '' -or $BaseUri.Query -ne '' -or $BaseUri.Fragment -ne '' -or
        $BaseUri.AbsolutePath -ne '/' -or $CsrfToken.Length -lt 16 -or $CsrfToken.Length -gt 512 -or
        $CsrfToken -notmatch '^[A-Za-z0-9_-]+$') {
        throw 'Installed API smoke requires an HTTPS authority and an in-memory CSRF token.'
    }
    # Only this generated namespace is mutated. Fixtures intentionally remain for
    # the caller's same-release rerun and normal-reboot persistence checks.
    $fixture = 'sf-candidate-' + [Guid]::NewGuid().ToString('N').Substring(0, 12)
    $stage = 'initialization'
    $started = [DateTimeOffset]::UtcNow
    $deadline = $started.AddMinutes(15)
    $checks = [System.Collections.Generic.List[string]]::new()
    $diagnostic = @{ Note = '' }
    $utf8 = [Text.UTF8Encoding]::new($false, $true)
    $publicHandler = [System.Net.Http.HttpClientHandler]::new()
    $publicHandler.UseCookies = $false
    $publicHandler.AllowAutoRedirect = $false
    $publicHandler.UseProxy = $false
    $publicClient = [System.Net.Http.HttpClient]::new($publicHandler)
    $publicClient.Timeout = [TimeSpan]::FromSeconds(30)
    $publicOrigin = [UriBuilder]::new('http', $BaseUri.Host, 80).Uri

    function Assert-SfSmoke([bool] $Condition, [string] $Label) {
        if (-not $Condition) {
            $diagnostic.Note = " Assertion failed: $Label."
            throw 'Installed API smoke assertion failed.'
        }
    }
    function Get-SfSmokeID($Value) {
        if ($Value -isnot [string] -or $Value -cnotmatch '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$') {
            throw 'Installed API smoke received an invalid resource identifier.'
        }
        return $Value
    }
    function Get-SfSmokeSHA([byte[]] $Bytes) {
        return [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($Bytes)).ToLowerInvariant()
    }
    function Invoke-SfSmokeRequest {
        param(
            [string] $Method, [string] $Path, [int] $Expected = 200,
            $Body = $null, [byte[]] $Bytes = $null, [string] $Key = '',
            [string] $SiteHost = '', [string] $Agent = 'Stackfort-Candidate-Smoke/1.0',
            [string] $Cookie = '', [switch] $Binary
        )
        if ([DateTimeOffset]::UtcNow -gt $deadline) { throw 'Installed API smoke exceeded its total deadline.' }
        if ($Path -notmatch '^/[^/]' -or $Path.Contains('://')) { throw 'Invalid smoke request path.' }
        $isPublic = $SiteHost -ne ''
        if ($isPublic -and ($SiteHost -cnotmatch ('^' + [regex]::Escape($fixture) + '-(static|php)\.example\.test$') -or $Method -cne 'GET')) {
            throw 'Invalid public smoke request target.'
        }
        $origin = if ($isPublic) { $publicOrigin } else { $BaseUri }
        $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::new($Method), [Uri]::new($origin, $Path))
        $response = $null
        $stream = $null
        $memory = [IO.MemoryStream]::new()
        $timeout = [Threading.CancellationTokenSource]::new([TimeSpan]::FromSeconds(120))
        try {
            $request.Headers.UserAgent.ParseAdd($Agent)
            if ($isPublic) {
                $request.Headers.Host = $SiteHost
                if ($Cookie -ne '') { [void]$request.Headers.TryAddWithoutValidation('Cookie', $Cookie) }
                $http = $publicClient
            } else {
                $http = $Client
                [void]$request.Headers.TryAddWithoutValidation('X-Request-ID', $fixture + '-' + [Guid]::NewGuid().ToString('N'))
                if ($Method -cne 'GET') {
                    [void]$request.Headers.TryAddWithoutValidation('X-CSRF-Token', $CsrfToken)
                    if ($Key -eq '') { $Key = $fixture + '-' + [Guid]::NewGuid().ToString('N') }
                    [void]$request.Headers.TryAddWithoutValidation('Idempotency-Key', $Key)
                }
            }
            if ($null -ne $Bytes) {
                $request.Content = [System.Net.Http.ByteArrayContent]::new($Bytes)
                $request.Content.Headers.ContentType = [System.Net.Http.Headers.MediaTypeHeaderValue]::new('application/octet-stream')
                [void]$request.Headers.TryAddWithoutValidation('Upload-Offset', '0')
            } elseif ($null -ne $Body) {
                $request.Content = [System.Net.Http.StringContent]::new(($Body | ConvertTo-Json -Depth 12 -Compress), $utf8, 'application/json')
            }
            $response = $http.SendAsync($request, [System.Net.Http.HttpCompletionOption]::ResponseHeadersRead, $timeout.Token).GetAwaiter().GetResult()
            $stream = $response.Content.ReadAsStreamAsync($timeout.Token).GetAwaiter().GetResult()
            $buffer = [byte[]]::new(8192)
            while (($count = $stream.ReadAsync($buffer, 0, $buffer.Length, $timeout.Token).GetAwaiter().GetResult()) -gt 0) {
                if ($memory.Length + $count -gt 1048576) { throw 'Installed API smoke response exceeded its byte bound.' }
                $memory.Write($buffer, 0, $count)
            }
            $status = [int]$response.StatusCode
            if ($status -ne $Expected) {
                # Never include raw server bodies, headers, exception chains or
                # HttpRequestMessage objects: those can carry credentials.
                $diagnostic.Note = " Expected HTTP $Expected, received $status."
                throw 'Installed API smoke received an unexpected HTTP status.'
            }
            $data = $memory.ToArray()
            $cache = ''
            if ($response.Headers.Contains('X-Stackfort-Cache')) { $cache = [string]::Join(',', $response.Headers.GetValues('X-Stackfort-Cache')) }
            if ($Binary -or $isPublic) { return [pscustomobject]@{ Bytes = $data; Cache = $cache; Status = $status } }
            return ($utf8.GetString($data) | ConvertFrom-Json -Depth 30)
        } finally {
            if ($null -ne $stream) { $stream.Dispose() }
            if ($null -ne $response) { $response.Dispose() }
            $request.Dispose()
            $timeout.Dispose()
            $memory.Dispose()
        }
    }
    function Wait-SfSmokeOperation([string] $AccountID, [string] $OperationID, [switch] $Database) {
        $accountID = Get-SfSmokeID $AccountID
        $operationID = Get-SfSmokeID $OperationID
        $kind = if ($Database) { 'database-operations' } else { 'operations' }
        $until = [DateTimeOffset]::UtcNow.AddMinutes(3)
        do {
            $operation = Invoke-SfSmokeRequest GET "/api/v1/accounts/$accountID/$kind/$operationID"
            Assert-SfSmoke ($operation.id -ceq $operationID) 'operation identity'
            if ($operation.status -ceq 'succeeded') { return }
            if ($operation.status -notin @('pending', 'running')) {
                $diagnostic.Note = ' Queued operation reached a non-success terminal status.'
                throw 'Installed API smoke operation did not succeed.'
            }
            Start-Sleep -Milliseconds 500
        } while ([DateTimeOffset]::UtcNow -lt $until)
        throw 'Installed API smoke operation timed out.'
    }
    function Send-SfSmokeFile([string] $AccountID, [string] $Directory, [string] $Name, [byte[]] $Payload) {
        Assert-SfSmoke ($Payload.Length -gt 0 -and $Payload.Length -le 16384) 'fixture upload bound'
        $hash = Get-SfSmokeSHA $Payload
        $inputBody = @{ directory = $Directory; name = $Name; sizeBytes = $Payload.Length; expectedSha256 = $hash }
        $upload = Invoke-SfSmokeRequest POST "/api/v1/accounts/$AccountID/file-uploads" -Expected 201 -Body $inputBody
        $uploadID = Get-SfSmokeID $upload.uploadId
        $chunk = Invoke-SfSmokeRequest PUT "/api/v1/accounts/$AccountID/file-uploads/$uploadID" -Bytes $Payload
        Assert-SfSmoke ($chunk.receivedBytes -eq $Payload.Length) 'upload received bytes'
        $complete = Invoke-SfSmokeRequest POST "/api/v1/accounts/$AccountID/file-uploads/$uploadID/complete" -Body $inputBody
        Assert-SfSmoke ($complete.completed -eq $true -and $complete.sha256 -ceq $hash) 'completed upload digest'
        $path = [Uri]::EscapeDataString("$Directory/$Name")
        $download = Invoke-SfSmokeRequest GET "/api/v1/accounts/$AccountID/files/download?path=$path" -Binary
        Assert-SfSmoke ((Get-SfSmokeSHA $download.Bytes) -ceq $hash) 'download digest'
        return $hash
    }
    function Set-SfSmokeDomain([string] $AccountID, [string] $DomainID, $Changes) {
        $queued = Invoke-SfSmokeRequest PATCH "/api/v1/accounts/$AccountID/domains/$DomainID" -Expected 202 -Body $Changes
        Wait-SfSmokeOperation $AccountID $queued.operationId
    }

    try {
        $stage = 'host-capabilities'
        $capabilities = Invoke-SfSmokeRequest GET '/api/v1/admin/host/capabilities'
        $phpVersions = @($capabilities.managedPhpVersions | Where-Object { $_ -cmatch '^[0-9]+\.[0-9]+$' })
        Assert-SfSmoke ($phpVersions.Count -gt 0) 'managed PHP runtime available'
        $phpVersion = [string]$phpVersions[0]

        $stage = 'package-and-account'
        $package = Invoke-SfSmokeRequest POST '/api/v1/admin/packages' -Expected 201 -Body @{
            name = $fixture; slug = $fixture; limits = @{
                maxDomains = 4; maxDatabases = 2; maxDatabaseUsers = 2; maxScheduledJobs = 0; maxOciApplications = 0
                cpuQuotaPercent = 50; memoryBytes = 268435456; swapBytes = 0; processLimit = 128
                storageBytes = 134217728; storageInodes = 4096; backupStorageBytes = 67108864
                allowedPhpVersions = @($phpVersion); features = @{ customRedirects = $true }
            }
        }
        $packageID = Get-SfSmokeID $package.id
        $account = Invoke-SfSmokeRequest POST '/api/v1/admin/accounts' -Expected 201 -Body @{ name = $fixture; slug = $fixture; packageId = $packageID }
        $accountID = Get-SfSmokeID $account.id
        Wait-SfSmokeOperation $accountID $account.provisioningOperationId
        $accounts = Invoke-SfSmokeRequest GET '/api/v1/admin/accounts'
        $owned = @($accounts.accounts | Where-Object { $_.id -ceq $accountID })
        Assert-SfSmoke ($owned.Count -eq 1 -and $owned[0].hostReady -eq $true) 'account host-ready'
        $checks.Add('installed-api-account-provisioning')

        $stage = 'domains-and-files'
        $staticHost = $fixture + '-static.example.test'
        $phpHost = $fixture + '-php.example.test'
        $staticBody = @{ name = $staticHost; canonicalMode = 'serve_both'; disableTls = $true; target = @{ type = 'static' }; wafMode = 'off' }
        $staticKey = $fixture + '-static-create'
        $static = Invoke-SfSmokeRequest POST "/api/v1/accounts/$accountID/domains" -Expected 202 -Body $staticBody -Key $staticKey
        $staticID = Get-SfSmokeID $static.domainId
        Wait-SfSmokeOperation $accountID $static.operationId
        $replayed = Invoke-SfSmokeRequest POST "/api/v1/accounts/$accountID/domains" -Expected 202 -Body $staticBody -Key $staticKey
        Assert-SfSmoke ($replayed.operationId -ceq $static.operationId -and $replayed.domainId -ceq $staticID) 'domain idempotency replay'
        $php = Invoke-SfSmokeRequest POST "/api/v1/accounts/$accountID/domains" -Expected 202 -Body @{
            name = $phpHost; canonicalMode = 'serve_both'; disableTls = $true; wafMode = 'off'; cachePreset = 'disabled'
            target = @{ type = 'php'; phpVersion = $phpVersion; rootMode = 'custom'; documentRoot = 'php-smoke' }
        }
        $phpID = Get-SfSmokeID $php.domainId
        Wait-SfSmokeOperation $accountID $php.operationId
        $staticBytes = $utf8.GetBytes($fixture + " static fixture`n")
        $staticSHA = Send-SfSmokeFile $accountID 'public_html' 'index.html' $staticBytes
        $served = Invoke-SfSmokeRequest GET '/index.html' -SiteHost $staticHost
        Assert-SfSmoke ((Get-SfSmokeSHA $served.Bytes) -ceq $staticSHA) 'static virtual host serves uploaded bytes'
        $phpSource = '<?php header("Content-Type: text/plain"); header("Cache-Control: public, max-age=300"); echo "FIXTURE|" . bin2hex(random_bytes(16));'
        $phpBytes = $utf8.GetBytes($phpSource.Replace('FIXTURE', $fixture))
        $phpSHA = Send-SfSmokeFile $accountID 'php-smoke' 'index.php' $phpBytes
        $directA = Invoke-SfSmokeRequest GET '/index.php' -SiteHost $phpHost
        $directB = Invoke-SfSmokeRequest GET '/index.php' -SiteHost $phpHost
        Assert-SfSmoke ($utf8.GetString($directA.Bytes).StartsWith($fixture + '|') -and
            (Get-SfSmokeSHA $directA.Bytes) -cne (Get-SfSmokeSHA $directB.Bytes) -and $directA.Cache -ceq '') 'uncached PHP execution'
        $checks.Add('installed-api-static-php-upload-download')
        $checks.Add('installed-api-domain-idempotency')

        $stage = 'database-provisioning'
        $database = Invoke-SfSmokeRequest POST "/api/v1/accounts/$accountID/databases/wizard" -Expected 202 -Body @{
            databaseAlias = 'candidate_smoke'; newUserAlias = 'candidate_smoke'; preset = 'read_write'
        }
        $databaseID = Get-SfSmokeID $database.databaseId
        $databaseUserID = Get-SfSmokeID $database.databaseUserId
        Wait-SfSmokeOperation $accountID $database.operationId -Database
        $workspace = Invoke-SfSmokeRequest GET "/api/v1/accounts/$accountID/databases"
        Assert-SfSmoke (@($workspace.databases | Where-Object { $_.id -ceq $databaseID -and $_.status -ceq 'active' }).Count -eq 1) 'database active'
        Assert-SfSmoke (@($workspace.users | Where-Object { $_.id -ceq $databaseUserID -and $_.status -ceq 'active' }).Count -eq 1) 'database user active'
        $checks.Add('installed-api-database-wizard')

        $stage = 'backup-restore'
        $created = Invoke-SfSmokeRequest POST "/api/v1/accounts/$accountID/backups" -Expected 201 -Body @{ scope = 'document_root'; sourcePath = 'public_html' }
        $backupID = Get-SfSmokeID $created.backup.backupId
        $verified = Invoke-SfSmokeRequest POST "/api/v1/accounts/$accountID/backups/$backupID/verify" -Body @{}
        Assert-SfSmoke ($verified.backup.payloadVerified -eq $true -and $verified.backup.manifestAuthenticated -eq $true) 'backup authenticated and verified'
        $backupDownload = Invoke-SfSmokeRequest GET "/api/v1/accounts/$accountID/backups/$backupID/download" -Binary
        Assert-SfSmoke ((Get-SfSmokeSHA $backupDownload.Bytes) -ceq $verified.backup.payloadSha256) 'backup download digest'
        $trashed = Invoke-SfSmokeRequest POST "/api/v1/accounts/$accountID/file-trash" -Expected 201 -Body @{ directory = 'public_html'; name = 'index.html' }
        [void](Get-SfSmokeID $trashed.trashId)
        $restored = Invoke-SfSmokeRequest POST "/api/v1/accounts/$accountID/backups/$backupID/restore" -Body @{ confirmation = $backupID }
        Assert-SfSmoke ($restored.completed -eq $true) 'backup restore completed'
        $served = Invoke-SfSmokeRequest GET '/index.html' -SiteHost $staticHost
        Assert-SfSmoke ((Get-SfSmokeSHA $served.Bytes) -ceq $staticSHA) 'backup restored original served bytes'
        $checks.Add('installed-api-document-root-backup-restore')

        $stage = 'fastcgi-and-waf'
        Set-SfSmokeDomain $accountID $phpID @{ cachePreset = 'fastcgi_respect_origin' }
        $miss = Invoke-SfSmokeRequest GET '/index.php' -SiteHost $phpHost
        $hit = Invoke-SfSmokeRequest GET '/index.php' -SiteHost $phpHost
        Assert-SfSmoke ($miss.Cache -ceq 'MISS' -and $hit.Cache -ceq 'HIT' -and
            (Get-SfSmokeSHA $miss.Bytes) -ceq (Get-SfSmokeSHA $hit.Bytes)) 'FastCGI MISS then same-body HIT'
        $bypass = Invoke-SfSmokeRequest GET '/index.php' -SiteHost $phpHost -Cookie 'candidate_session=private'
        Assert-SfSmoke ($bypass.Cache -ceq 'BYPASS' -and (Get-SfSmokeSHA $bypass.Bytes) -cne (Get-SfSmokeSHA $hit.Bytes)) 'cookie cache bypass'
        foreach ($mode in @('off', 'detection_only', 'blocking_pl1')) {
            Set-SfSmokeDomain $accountID $phpID @{ wafMode = $mode }
            $null = Invoke-SfSmokeRequest GET '/index.php' -SiteHost $phpHost
            $warm = Invoke-SfSmokeRequest GET '/index.php' -SiteHost $phpHost
            Assert-SfSmoke ($warm.Cache -ceq 'HIT') 'cache warm before WAF probe'
            $expected = if ($mode -ceq 'blocking_pl1') { 403 } else { 200 }
            $probe = Invoke-SfSmokeRequest GET '/index.php' -SiteHost $phpHost -Agent 'sqlmap/1.0' -Expected $expected
            if ($expected -eq 200) { Assert-SfSmoke ($probe.Cache -ceq 'HIT') 'allowed scanner request uses warm cache' }
        }
        $purge = Invoke-SfSmokeRequest POST "/api/v1/accounts/$accountID/domains/$phpID/cache/purge" -Expected 202 -Body @{ pathPrefix = '/' }
        Wait-SfSmokeOperation $accountID $purge.operationId
        $purged = Invoke-SfSmokeRequest GET '/index.php' -SiteHost $phpHost
        Assert-SfSmoke ($purged.Cache -ceq 'MISS' -and (Get-SfSmokeSHA $purged.Bytes) -cne (Get-SfSmokeSHA $warm.Bytes)) 'FastCGI purge replaces cached representation'
        Set-SfSmokeDomain $accountID $phpID @{ cachePreset = 'disabled' }
        $offA = Invoke-SfSmokeRequest GET '/index.php' -SiteHost $phpHost
        $offB = Invoke-SfSmokeRequest GET '/index.php' -SiteHost $phpHost
        Assert-SfSmoke ($offA.Cache -ceq '' -and $offB.Cache -ceq '' -and
            (Get-SfSmokeSHA $offA.Bytes) -cne (Get-SfSmokeSHA $offB.Bytes)) 'FastCGI disabled per domain'
        # Leave a protected cached PHP fixture for the caller's reboot checks.
        Set-SfSmokeDomain $accountID $phpID @{ cachePreset = 'fastcgi_respect_origin' }
        $checks.Add('installed-api-fastcgi-toggle-hit-bypass-purge')
        $checks.Add('installed-api-waf-off-detection-blocking-before-cache')

        return [pscustomobject]@{
            schemaVersion = 1; kind = 'installed-product-api-smoke'; fixturePrefix = $fixture
            startedAt = $started.ToString('o'); completedAt = [DateTimeOffset]::UtcNow.ToString('o')
            checks = $checks.ToArray(); packageId = $packageID; accountId = $accountID
            staticDomainId = $staticID; staticHost = $staticHost; staticSHA256 = $staticSHA
            phpDomainId = $phpID; phpHost = $phpHost; phpVersion = $phpVersion; phpSourceSHA256 = $phpSHA
            databaseId = $databaseID; databaseUserId = $databaseUserID; backupId = $backupID; backupSHA256 = $verified.backup.payloadSha256
            fixturesRetained = $true
            exclusions = @('tenant authorization/isolation', 'database SQL/credential/phpMyAdmin', 'OCI deployment', 'performance benchmark', 'full-account/database backup', 'product uninstall')
        }
    } catch {
        # Preserve only this locally selected stage, never the caught HTTP or
        # PowerShell exception (which may contain caller-bound secret objects).
        # The outer C# credential owner deliberately discards exception chains;
        # emit this already-sanitized diagnostic before crossing that boundary.
        Write-Host "STACKFORT_QUALIFICATION_FAILURE stage=$stage fixture=$fixture$($diagnostic.Note)"
        throw "Installed product API smoke failed at stage '$stage'; candidate fixtures '$fixture' are retained for inspection.$($diagnostic.Note)"
    } finally {
        $publicClient.Dispose()
        $publicHandler.Dispose()
    }
}

# Reuse the original fixture after the public rerun or a normal reboot. This
# helper sends only GET requests; ordinary access/audit/cache effects still occur.
function Test-StackfortInstalledApiPersistence {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)][System.Net.Http.HttpClient] $Client,
        [Parameter(Mandatory)][Uri] $BaseUri,
        [Parameter(Mandatory)] $Evidence
    )
    Set-StrictMode -Version Latest
    $ErrorActionPreference = 'Stop'
    if (-not $BaseUri.IsAbsoluteUri -or $BaseUri.Scheme -cne 'https' -or $BaseUri.UserInfo -ne '' -or
        $BaseUri.AbsolutePath -ne '/' -or $BaseUri.Query -ne '' -or $BaseUri.Fragment -ne '') {
        throw 'Persistence checks require the original pinned HTTPS authority.'
    }
    if ($Evidence.schemaVersion -ne 1 -or $Evidence.kind -cne 'installed-product-api-smoke' -or
        $Evidence.fixturePrefix -cnotmatch '^sf-candidate-[0-9a-f]{12}$' -or
        $Evidence.staticHost -cne ($Evidence.fixturePrefix + '-static.example.test') -or
        $Evidence.phpHost -cne ($Evidence.fixturePrefix + '-php.example.test')) {
        throw 'Persistence checks require original bounded smoke evidence.'
    }
    foreach ($field in @('packageId', 'accountId', 'staticDomainId', 'phpDomainId', 'databaseId', 'databaseUserId', 'backupId')) {
        if ($Evidence.$field -isnot [string] -or $Evidence.$field -cnotmatch '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$') {
            throw 'Persistence evidence contains an invalid resource identifier.'
        }
    }
    foreach ($field in @('staticSHA256', 'phpSourceSHA256', 'backupSHA256')) {
        if ($Evidence.$field -isnot [string] -or $Evidence.$field -cnotmatch '^[0-9a-f]{64}$') {
            throw 'Persistence evidence contains an invalid content digest.'
        }
    }
    $stage = 'session'
    $deadline = [DateTimeOffset]::UtcNow.AddMinutes(3)
    $utf8 = [Text.UTF8Encoding]::new($false, $true)
    $publicHandler = [System.Net.Http.HttpClientHandler]::new()
    $publicHandler.UseCookies = $false
    $publicHandler.AllowAutoRedirect = $false
    $publicHandler.UseProxy = $false
    $publicClient = [System.Net.Http.HttpClient]::new($publicHandler)
    $publicClient.Timeout = [TimeSpan]::FromSeconds(30)
    $publicOrigin = [UriBuilder]::new('http', $BaseUri.Host, 80).Uri
    function Get-SfPersistenceSHA([byte[]] $Bytes) {
        return [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($Bytes)).ToLowerInvariant()
    }
    function Assert-SfPersistence([bool] $Condition) {
        if (-not $Condition) { throw 'Installed persistence assertion failed.' }
    }
    function Get-SfPersistence {
        param([string] $Path, [string] $SiteHost = '', [switch] $Binary, [switch] $Scanner)
        if ([DateTimeOffset]::UtcNow -gt $deadline -or $Path -notmatch '^/[^/]' -or $Path.Contains('://')) {
            throw 'Persistence request is outside its bounds.'
        }
        $isPublic = $SiteHost -ne ''
        if ($isPublic -and $SiteHost -cne $Evidence.staticHost -and $SiteHost -cne $Evidence.phpHost) {
            throw 'Persistence request is outside the original fixture.'
        }
        if ($Scanner -and (-not $isPublic -or $SiteHost -cne $Evidence.phpHost)) { throw 'Invalid persistence scanner target.' }
        $origin = if ($isPublic) { $publicOrigin } else { $BaseUri }
        $http = if ($isPublic) { $publicClient } else { $Client }
        $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Get, [Uri]::new($origin, $Path))
        $timeout = [Threading.CancellationTokenSource]::new([TimeSpan]::FromSeconds(30))
        $response = $null; $stream = $null
        $memory = [IO.MemoryStream]::new()
        try {
            if ($isPublic) { $request.Headers.Host = $SiteHost }
            $agent = if ($Scanner) { 'sqlmap/1.0' } else { 'Stackfort-Candidate-Smoke/1.0' }
            $request.Headers.UserAgent.ParseAdd($agent)
            $response = $http.SendAsync($request, [System.Net.Http.HttpCompletionOption]::ResponseHeadersRead, $timeout.Token).GetAwaiter().GetResult()
            $expected = if ($Scanner) { 403 } else { 200 }
            Assert-SfPersistence ([int]$response.StatusCode -eq $expected)
            $stream = $response.Content.ReadAsStreamAsync($timeout.Token).GetAwaiter().GetResult()
            $buffer = [byte[]]::new(8192)
            while (($count = $stream.ReadAsync($buffer, 0, $buffer.Length, $timeout.Token).GetAwaiter().GetResult()) -gt 0) {
                if ($memory.Length + $count -gt 1048576) { throw 'Persistence response exceeds its byte bound.' }
                $memory.Write($buffer, 0, $count)
            }
            $bytes = $memory.ToArray()
            $cache = ''
            if ($response.Headers.Contains('X-Stackfort-Cache')) { $cache = [string]::Join(',', $response.Headers.GetValues('X-Stackfort-Cache')) }
            if ($Binary -or $isPublic) { return [pscustomobject]@{ Bytes = $bytes; Cache = $cache } }
            return ($utf8.GetString($bytes) | ConvertFrom-Json -Depth 30)
        } finally {
            if ($null -ne $stream) { $stream.Dispose() }
            if ($null -ne $response) { $response.Dispose() }
            $request.Dispose(); $timeout.Dispose(); $memory.Dispose()
        }
    }
    try {
        $null = Get-SfPersistence '/api/v1/session'
        $stage = 'package-account-domains'
        $packages = Get-SfPersistence '/api/v1/admin/packages'
        Assert-SfPersistence (@($packages.packages | Where-Object { $_.id -ceq $Evidence.packageId -and $_.slug -ceq $Evidence.fixturePrefix }).Count -eq 1)
        $accounts = Get-SfPersistence '/api/v1/admin/accounts'
        Assert-SfPersistence (@($accounts.accounts | Where-Object { $_.id -ceq $Evidence.accountId -and $_.hostReady -eq $true -and $_.packageId -ceq $Evidence.packageId }).Count -eq 1)
        $accountRoot = '/api/v1/accounts/' + $Evidence.accountId
        $domains = Get-SfPersistence "$accountRoot/domains"
        Assert-SfPersistence (@($domains.domains).Count -eq 2)
        Assert-SfPersistence (@($domains.domains | Where-Object { $_.id -ceq $Evidence.staticDomainId -and $_.name.ascii -ceq $Evidence.staticHost -and $_.status -ceq 'active' -and $_.target.type -ceq 'static' }).Count -eq 1)
        Assert-SfPersistence (@($domains.domains | Where-Object { $_.id -ceq $Evidence.phpDomainId -and $_.name.ascii -ceq $Evidence.phpHost -and $_.status -ceq 'active' -and $_.target.type -ceq 'php' -and $_.waf.mode -ceq 'blocking_pl1' -and $_.cache.preset -ceq 'fastcgi_respect_origin' }).Count -eq 1)

        $stage = 'files-and-backup'
        $static = Get-SfPersistence "$accountRoot/files/download?path=public_html%2Findex.html" -Binary
        Assert-SfPersistence ((Get-SfPersistenceSHA $static.Bytes) -ceq $Evidence.staticSHA256)
        $phpSource = Get-SfPersistence "$accountRoot/files/download?path=php-smoke%2Findex.php" -Binary
        Assert-SfPersistence ((Get-SfPersistenceSHA $phpSource.Bytes) -ceq $Evidence.phpSourceSHA256)
        $backupRoot = "$accountRoot/backups/" + $Evidence.backupId
        $backup = Get-SfPersistence $backupRoot
        # Inspect authenticates the manifest but deliberately does not claim a
        # new full payload verification. Hash the actual downloaded bytes here.
        Assert-SfPersistence ($backup.backup.backupId -ceq $Evidence.backupId -and $backup.backup.manifestAuthenticated -eq $true -and $backup.backup.payloadSha256 -ceq $Evidence.backupSHA256)
        $backupBytes = Get-SfPersistence "$backupRoot/download" -Binary
        Assert-SfPersistence ((Get-SfPersistenceSHA $backupBytes.Bytes) -ceq $Evidence.backupSHA256)

        $stage = 'database'
        $database = Get-SfPersistence "$accountRoot/databases"
        Assert-SfPersistence (@($database.databases | Where-Object { $_.id -ceq $Evidence.databaseId -and $_.status -ceq 'active' }).Count -eq 1)
        Assert-SfPersistence (@($database.users | Where-Object { $_.id -ceq $Evidence.databaseUserId -and $_.status -ceq 'active' }).Count -eq 1)

        $stage = 'public-php-waf-cache'
        $served = Get-SfPersistence '/index.html' -SiteHost $Evidence.staticHost
        Assert-SfPersistence ((Get-SfPersistenceSHA $served.Bytes) -ceq $Evidence.staticSHA256)
        $first = Get-SfPersistence '/index.php' -SiteHost $Evidence.phpHost
        $second = Get-SfPersistence '/index.php' -SiteHost $Evidence.phpHost
        Assert-SfPersistence ($first.Cache -cin @('MISS', 'HIT') -and $second.Cache -ceq 'HIT' -and
            $utf8.GetString($second.Bytes).StartsWith($Evidence.fixturePrefix + '|') -and
            (Get-SfPersistenceSHA $first.Bytes) -ceq (Get-SfPersistenceSHA $second.Bytes))
        $null = Get-SfPersistence '/index.php' -SiteHost $Evidence.phpHost -Scanner
        return [pscustomobject]@{
            schemaVersion = 1; kind = 'installed-product-persistence'; fixturePrefix = $Evidence.fixturePrefix
            completedAt = [DateTimeOffset]::UtcNow.ToString('o'); accountId = $Evidence.accountId
            checks = @('existing-session', 'package-account-domains', 'file-digests', 'backup-digest', 'database-inventory', 'static-php-waf-fastcgi')
            configurationMutations = $false
        }
    } catch {
        throw "Installed product persistence check failed at stage '$stage'; no configuration mutations or new fixtures were requested."
    } finally {
        $publicClient.Dispose(); $publicHandler.Dispose()
    }
}
