# SPDX-License-Identifier: AGPL-3.0-or-later
# Local-only structural/contract checks. Never invokes the real driver body,
# Hyper-V, SSH, setup issuance, or its controlling-terminal consent parser.
#requires -Version 7.2
[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$contractPath = Join-Path $PSScriptRoot 'Test-StackfortNativeOnboardHyperVVm.ps1'
$contractTokens = $null; $contractErrors = $null
$contractAst = [System.Management.Automation.Language.Parser]::ParseFile($contractPath, [ref] $contractTokens, [ref] $contractErrors)
if ($contractErrors.Count -ne 0) { throw 'Driver syntax check failed.' }
$contractDefinition = $contractAst.FindAll({ param($node)
    $node -is [System.Management.Automation.Language.StringConstantExpressionAst] -and $node.Value.StartsWith('using System;')
}, $true) | Select-Object -First 1
if (-not ('StackfortNativeOnboardProbeV1' -as [type])) { Add-Type -TypeDefinition $contractDefinition.Value }
$contractPwsh = (Get-Process -Id $PID).Path
$contractRun = [StackfortNativeOnboardProbeV1]::Run($contractPwsh, @('-NoProfile', '-Command', '[Console]::Out.Write("bounded-ok")'), 15)
if ($contractRun.ExitCode -ne 0 -or $contractRun.Output -cne 'bounded-ok') { throw 'Bounded process result mismatch.' }
foreach ($command in @('[Console]::Out.Write("x" * 300000)', 'Start-Sleep -Seconds 10')) {
    $rejected = $false
    try { [void] [StackfortNativeOnboardProbeV1]::Run($contractPwsh, @('-NoProfile', '-Command', $command), 1) } catch { $rejected = $true }
    if (-not $rejected) { throw 'Process output/deadline bound was not enforced.' }
}

# Extract only the receipt polling function. All possible external calls below
# are replaced by in-memory fixtures before invoking it.
$contractWait = $contractAst.FindAll({ param($node)
    $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Wait-OnboardCompleted'
}, $true) | Select-Object -First 1
. ([scriptblock]::Create($contractWait.Extent.Text))
function Get-OnboardAddress { return '192.0.2.10' }
function Start-Sleep { param([int] $Seconds) }
$Version = '0.1.0-beta.4'
$taskInitialBoot = '11111111-1111-4111-8111-111111111111'
$taskIdentity = @($taskInitialBoot, '22222222-2222-4222-8222-222222222222', '33333333-3333-4333-8333-333333333333', '44444444-4444-4444-8444-444444444444')
$taskCapture = [pscustomobject]@{ OperationID = '55555555-5555-4555-8555-555555555555'; SetupSHA256 = ('a' * 64) }
$taskFinalBoot = '66666666-6666-4666-8666-666666666666'
$script:contractCurrentBoot = $taskFinalBoot
$script:contractTransient = $true
$script:contractRecords = @{}
$script:contractRecords['storage-state.json'] = @{
    phase = 'ready'; resumeBootId = $taskFinalBoot; armAttempts = 1
    plan = @{ operationId = $taskCapture.OperationID; version = $Version; sourceDigest = ('b' * 64)
        previousBootId = $taskInitialBoot; machineId = $taskIdentity[1]; rootUuid = $taskIdentity[2]; partitionUuid = $taskIdentity[3] }
} | ConvertTo-Json -Depth 5 -Compress
$script:contractRecords['install-state.json'] = @{ status = 'complete'; version = $Version; sourceDigest = ('b' * 64) } | ConvertTo-Json -Compress
$script:contractRecords['native-setup.json'] = (@{ schemaVersion = 1; operationId = $taskCapture.OperationID; tokenSHA256 = $taskCapture.SetupSHA256; releaseSHA256 = ('c' * 64) } | ConvertTo-Json -Compress) + "`n"
$contractSetupDigest = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($script:contractRecords['native-setup.json']))).ToLowerInvariant()
$contractCreated = [DateTimeOffset]::UtcNow
$script:contractRecords['native-setup-registered.json'] = @{
    schemaVersion = 1; operationId = $taskCapture.OperationID; setupSHA256 = $contractSetupDigest
    capability = @{ id = '77777777-7777-4777-8777-777777777777'; createdAt = $contractCreated.ToString('o'); expiresAt = $contractCreated.AddHours(1).ToString('o'); alreadyRegistered = $false }
} | ConvertTo-Json -Depth 5 -Compress
function Invoke-OnboardSSH([string] $Address, [string] $Command, [int] $Seconds = 30) {
    if ($Address -cne '192.0.2.10') { throw 'Mock address mismatch.' }
    if ($script:contractTransient) { $script:contractTransient = $false; throw 'Synthetic transient SSH timeout.' }
    if ($Command.StartsWith('cat /proc/sys/kernel/random/boot_id && ')) { return [pscustomobject]@{ ExitCode = 0; Output = $script:contractCurrentBoot + "`n" } }
    if ($Command.StartsWith('sudo -n cat /var/lib/stackfort-installer/')) {
        $name = $Command.Substring('sudo -n cat /var/lib/stackfort-installer/'.Length)
        return [pscustomobject]@{ ExitCode = 0; Output = $script:contractRecords[$name] }
    }
    throw 'Unexpected mock SSH command.'
}
$taskCompletion = Wait-OnboardCompleted -PreviousBoot $taskInitialBoot -RequireActiveSetup
if ($taskCompletion.ConversionBootID -cne $taskFinalBoot -or $script:contractTransient) { throw 'Initial receipt/transient poll contract failed.' }
$script:contractCurrentBoot = '88888888-8888-4888-8888-888888888888'
$contractReboot = Wait-OnboardCompleted -PreviousBoot $taskFinalBoot -ConversionBoot $taskFinalBoot
if ($contractReboot.ConversionBootID -cne $taskFinalBoot -or $contractReboot.BootID -cne $script:contractCurrentBoot -or $contractReboot.CapabilityExpiresAt -cne $taskCompletion.CapabilityExpiresAt) { throw 'Normal reboot receipt contract failed.' }
$script:contractRecords['native-setup.json'] += ' '
$rejected = $false
try { [void] (Wait-OnboardCompleted -PreviousBoot $taskFinalBoot -ConversionBoot $taskFinalBoot) } catch {
    if ($_.Exception.Message -notlike 'Completed native/setup receipts are malformed*') { throw 'Receipt mismatch did not fail with redacted error.' }
    $rejected = $true
}
if (-not $rejected) { throw 'Whole setup commitment digest mismatch was accepted.' }

# Extract the real callback, but inject a non-network client and mocks for its
# product/SSH/reboot boundaries. This checks same-process session handoff/order.
$contractCallback = $contractAst.FindAll({ param($node)
    $node -is [System.Management.Automation.Language.AssignmentStatementAst] -and $node.Left.Extent.Text -ceq '$taskSmoke'
}, $true) | Select-Object -First 1
$taskAddress = '192.0.2.10'; $taskCommand = 'synthetic exact-bootstrap-command'
$script:contractCallOrder = [Collections.Generic.List[string]]::new()
$contractClient = [Net.Http.HttpClient]::new()
$contractBaseUri = [Uri] 'https://192.0.2.10:8443/'
function Invoke-StackfortInstalledApiSmoke($Client, $BaseUri, $CsrfToken) {
    if (-not [object]::ReferenceEquals($Client, $contractClient) -or $BaseUri -ne $contractBaseUri -or $CsrfToken -cne 'synthetic-csrf') { throw 'Callback session handoff mismatch.' }
    $script:contractCallOrder.Add('smoke')
    return [pscustomobject]@{ fixturePrefix = 'synthetic-fixture' }
}
function Invoke-OnboardSSH([string] $Address, [string] $Command, [int] $Seconds = 30) {
    if ($Address -cne $taskAddress) { throw 'Callback address mismatch.' }
    if ($Command -ceq $taskCommand -and $Seconds -eq 480) { $script:contractCallOrder.Add('rerun'); return [pscustomobject]@{ ExitCode = 0; Output = 'Stackfort is already installed. Live native admission checks passed;' } }
    if ($Command -ceq 'cat /proc/sys/kernel/random/boot_id' -and $Seconds -eq 15) { $script:contractCallOrder.Add('same-boot'); return [pscustomobject]@{ ExitCode = 0; Output = $taskFinalBoot + "`n" } }
    if ($Command -ceq 'sudo -n systemctl reboot' -and $Seconds -eq 30) { $script:contractCallOrder.Add('reboot'); return [pscustomobject]@{ ExitCode = 255; Output = '' } }
    throw 'Unexpected callback SSH command.'
}
function Wait-OnboardCompleted([string] $PreviousBoot, [string] $ConversionBoot, [int] $Minutes) {
    if ($PreviousBoot -cne $taskFinalBoot -or $ConversionBoot -cne $taskCompletion.ConversionBootID -or $Minutes -ne 8) { throw 'Callback readmission binding mismatch.' }
    $script:contractCallOrder.Add('readmission'); return $contractReboot
}
function Test-StackfortInstalledApiPersistence($Client, $BaseUri, $Evidence) {
    if (-not [object]::ReferenceEquals($Client, $contractClient) -or $BaseUri -ne $contractBaseUri -or $Evidence.fixturePrefix -cne 'synthetic-fixture') { throw 'Persistence session/evidence handoff mismatch.' }
    $script:contractCallOrder.Add('persistence'); return [pscustomobject]@{ checks = @('synthetic-persistence') }
}
try {
    . ([scriptblock]::Create($contractCallback.Extent.Text))
    $taskSmoke.Invoke($contractClient, $contractBaseUri, 'synthetic-csrf')
    if (($script:contractCallOrder -join ',') -cne 'smoke,rerun,same-boot,persistence,reboot,readmission,persistence' -or $script:taskOnboardPersistence.FinalBootID -cne $contractReboot.BootID) { throw 'Callback rerun/reboot/persistence ordering mismatch.' }
} finally { $contractClient.Dispose() }
Write-Host 'LOCAL CONTRACT PASS: parser/CSharp, process bounds, transient polling, first/consecutive boot receipts, digest rejection, same-session rerun/reboot callback. No VM or network actions performed.'
