# SPDX-License-Identifier: AGPL-3.0-or-later
# TEST ONLY: one explicitly disposable VM, exact tagged artifacts, real remote
# controlling TTY. No rebuild, restore, force-repair, raw transcript or token file.
#requires -Version 7.2
[CmdletBinding()]
param(
    [ValidateSet('Onboard')][string] $Stage = 'Onboard',
    [Parameter(Mandatory)][string] $ArchiveDirectory,
    [ValidateSet('0.1.0-beta.6')][string] $Version = '0.1.0-beta.6',
    [Parameter(Mandatory)][ValidatePattern('^[0-9a-f]{40}$')][string] $Commit,
    [Parameter(Mandatory)][ValidatePattern('^[0-9a-f]{64}$')][string] $ArchiveSHA256,
    [Parameter(Mandatory)][ValidatePattern('^[0-9a-f]{64}$')][string] $ChecksumsSHA256,
    [Parameter(Mandatory)][ValidatePattern('^[0-9a-f]{64}$')][string] $AttestationSHA256,
    [Parameter(Mandatory)][ValidatePattern('^[0-9a-f]{64}$')][string] $BootstrapSHA256,
    [switch] $AcceptDisposableReinstallationRisk,
    [switch] $ReturnRedactedResult
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if (-not $AcceptDisposableReinstallationRisk) {
    throw 'Onboard requires -AcceptDisposableReinstallationRisk for this exact fresh disposable VM; no restoration or repair is authorized.'
}

# This helper never writes subprocess output to any host stream. Only a small
# allowlisted result crosses back to PowerShell. Raw setup/password/cookies stay
# in process memory and are disposed after real HTTPS redemption/login checks.
if (-not ('StackfortNativeOnboardProbeV1' -as [type])) {
    Add-Type -TypeDefinition @'
using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Net;
using System.Net.Http;
using System.Net.Security;
using System.Runtime.InteropServices;
using System.Security;
using System.Security.Cryptography;
using System.Security.Cryptography.X509Certificates;
using System.Text;
using System.Text.Json;
using System.Text.RegularExpressions;
using System.Threading;
using System.Threading.Tasks;

public sealed class StackfortOnboardCommandResult {
    public int ExitCode; public string Output;
}
public sealed class StackfortOnboardCapture : IDisposable {
    public SecureString SetupCode;
    public string OperationID, ReviewSHA256, SetupSHA256;
    public void Dispose() { if (SetupCode != null) { SetupCode.Dispose(); SetupCode = null; } }
    public override string ToString() { return "[redacted native onboarding capture]"; }
}
public sealed class StackfortOnboardRedemption {
    public string AdministratorID, CertificateSHA256;
    public bool Redeemed, ReuseRejected, LoginVerified;
}
public static class StackfortNativeOnboardProbeV1 {
    static Process Start(string executable, string[] arguments) {
        var info = new ProcessStartInfo(executable) { UseShellExecute = false, CreateNoWindow = true,
            RedirectStandardInput = true, RedirectStandardOutput = true, RedirectStandardError = true };
        foreach (string argument in arguments) info.ArgumentList.Add(argument);
        var process = new Process { StartInfo = info };
        if (!process.Start()) throw new InvalidOperationException("Could not start the bounded qualification subprocess.");
        process.StandardInput.NewLine = "\n";
        return process;
    }
    static async Task<string> ReadBounded(StreamReader reader, int maximum) {
        var text = new StringBuilder(); var buffer = new char[4096];
        try {
            int count;
            while ((count = await reader.ReadAsync(buffer, 0, buffer.Length)) != 0) {
                if (text.Length > maximum - count) throw new InvalidOperationException("Subprocess output exceeded its bound.");
                text.Append(buffer, 0, count); Array.Clear(buffer, 0, count);
            }
            return text.ToString();
        } finally { text.Clear(); Array.Clear(buffer, 0, buffer.Length); }
    }
    static void Stop(Process process) {
        try { if (!process.HasExited) process.Kill(true); } catch { }
        try { process.WaitForExit(3000); } catch { }
    }
    public static StackfortOnboardCommandResult Run(string executable, string[] arguments, int seconds) {
        using (var process = Start(executable, arguments)) {
            var stdout = ReadBounded(process.StandardOutput, 262144);
            var stderr = ReadBounded(process.StandardError, 262144);
            process.StandardInput.Close(); var elapsed = Stopwatch.StartNew();
            try {
                while (!process.HasExited || !stdout.IsCompleted || !stderr.IsCompleted) {
                    if (elapsed.Elapsed.TotalSeconds > seconds || stdout.IsFaulted || stderr.IsFaulted)
                        throw new InvalidOperationException("Bounded SSH/transfer subprocess failed or timed out; preserve guest state.");
                    Thread.Sleep(25);
                }
                stderr.GetAwaiter().GetResult(); // Never expose stderr/command dumps.
                return new StackfortOnboardCommandResult { ExitCode = process.ExitCode, Output = stdout.GetAwaiter().GetResult() };
            } finally { Stop(process); }
        }
    }
    static string Field(string text, string label, string pattern) {
        var matches = Regex.Matches(text, "(?m)^" + Regex.Escape(label) + ": (" + pattern + ")$");
        if (matches.Count != 1) throw new InvalidOperationException("Interactive review omitted or duplicated an expected identity field.");
        return matches[0].Groups[1].Value;
    }
    static void Ack(Process process, string value) {
        process.StandardInput.WriteLine(value); process.StandardInput.Flush();
    }
    public static StackfortOnboardCapture Onboard(string executable, string[] arguments, string version, string commit,
        string machine, string root, string partition, string boot, string installerHash) {
        const string fresh = "FRESH-DISPOSABLE NO-DATA REINSTALLATION-RISK";
        var capture = new StackfortOnboardCapture(); bool success = false;
        var transcript = new StringBuilder(); var outBuffer = new char[4096]; var errBuffer = new char[4096];
        using (var process = Start(executable, arguments)) {
            Task<int> stdout = process.StandardOutput.ReadAsync(outBuffer, 0, outBuffer.Length);
            Task<int> stderr = process.StandardError.ReadAsync(errBuffer, 0, errBuffer.Length);
            var elapsed = Stopwatch.StartNew(); int stage = 0; bool armObserved = false;
            try {
                while (!process.HasExited || stdout != null || stderr != null) {
                    if (elapsed.Elapsed > TimeSpan.FromMinutes(15)) throw new InvalidOperationException("Interactive qualification timed out; preserve the guest and do not retry automatically.");
                    if (stdout != null && stdout.IsCompleted) {
                        int count = stdout.GetAwaiter().GetResult();
                        if (transcript.Length > 2097152 - count) throw new InvalidOperationException("Interactive output exceeded its bound.");
                        transcript.Append(outBuffer, 0, count); Array.Clear(outBuffer, 0, outBuffer.Length);
                        stdout = count == 0 ? null : process.StandardOutput.ReadAsync(outBuffer, 0, outBuffer.Length);
                    }
                    if (stderr != null && stderr.IsCompleted) {
                        int count = stderr.GetAwaiter().GetResult();
                        if (transcript.Length > 2097152 - count) throw new InvalidOperationException("Interactive output exceeded its bound.");
                        transcript.Append(errBuffer, 0, count); Array.Clear(errBuffer, 0, errBuffer.Length);
                        stderr = count == 0 ? null : process.StandardError.ReadAsync(errBuffer, 0, errBuffer.Length);
                    }
                    string text = transcript.ToString().Replace("\r", "");
                    if (stage == 0 && text.Contains("Type exactly: " + fresh + "\n> ")) {
                        if (Field(text, "Release", "[0-9A-Za-z.+-]+") != version || Field(text, "Tag commit", "[0-9a-f]{40}") != commit ||
                            Field(text, "Machine ID", "[0-9a-f-]{36}") != machine || Field(text, "Root filesystem UUID", "[0-9a-f-]{36}") != root ||
                            Field(text, "Root partition UUID", "[0-9a-f-]{36}") != partition || Field(text, "Boot ID", "[0-9a-f-]{36}") != boot ||
                            Field(text, "Installer SHA-256", "[0-9a-f]{64}") != installerHash)
                            throw new InvalidOperationException("Authenticated terminal review differs from the explicitly pinned release or live host.");
                        capture.OperationID = Field(text, "Operation", "[0-9a-f-]{36}");
                        if (Guid.Parse(capture.OperationID).ToString("D") != capture.OperationID) throw new InvalidOperationException("Noncanonical reviewed operation.");
                        capture.ReviewSHA256 = Field(text, "Review SHA-256", "[0-9a-f]{64}");
                        Ack(process, fresh); stage = 1;
                    }
                    if (stage == 1 && text.Contains("Type exactly: REBOOT\n> ")) { Ack(process, "REBOOT"); stage = 2; }
                    if (stage == 2 && text.Contains("Type exactly SAVED after saving the code:\n> ")) {
                        var codes = Regex.Matches(text, "(?m)^sfb_[A-Za-z0-9_-]{43}$");
                        if (codes.Count != 1) throw new InvalidOperationException("Expected exactly one canonical setup code on the controlling terminal.");
                        string code = codes[0].Value;
                        byte[] random = Convert.FromBase64String(code.Substring(4).Replace('-', '+').Replace('_', '/') + "=");
                        try {
                            if (random.Length != 32 || "sfb_" + Convert.ToBase64String(random).TrimEnd('=').Replace('+', '-').Replace('/', '_') != code)
                                throw new InvalidOperationException("Setup code encoding was not canonical.");
                        } finally { Array.Clear(random, 0, random.Length); }
                        capture.SetupCode = new SecureString(); foreach (char value in code) capture.SetupCode.AppendChar(value); capture.SetupCode.MakeReadOnly();
                        byte[] raw = Encoding.ASCII.GetBytes(code);
                        try { capture.SetupSHA256 = Convert.ToHexString(SHA256.HashData(raw)).ToLowerInvariant(); }
                        finally { Array.Clear(raw, 0, raw.Length); }
                        transcript.Replace(code, "[redacted setup code]"); code = null; text = null;
                        Ack(process, "SAVED"); stage = 3;
                    }
                    if (stage == 3 && transcript.ToString().Contains("Prepared runtime verified; arming the authorized one-shot boot and rebooting.")) armObserved = true;
                    Thread.Sleep(25);
                }
                if (stage != 3 || !armObserved || capture.SetupCode == null || (process.ExitCode != 0 && process.ExitCode != 255))
                    throw new InvalidOperationException("Interactive onboarding did not reach its acknowledged reboot boundary; preserve guest state.");
                success = true; return capture;
            } catch { throw new InvalidOperationException("Interactive onboarding qualification failed; raw terminal output was deliberately not logged. Preserve the guest for inspection."); }
            finally {
                Stop(process); transcript.Clear(); Array.Clear(outBuffer, 0, outBuffer.Length); Array.Clear(errBuffer, 0, errBuffer.Length);
                if (!success) capture.Dispose();
            }
        }
    }
    static JsonDocument Request(HttpClient client, HttpMethod method, string path, string body, int status) {
        using (var timeout = new CancellationTokenSource(TimeSpan.FromSeconds(45)))
        using (var request = new HttpRequestMessage(method, path)) {
            if (body != null) request.Content = new StringContent(body, Encoding.UTF8, "application/json");
            using (var response = client.Send(request, HttpCompletionOption.ResponseHeadersRead, timeout.Token)) {
                if ((int)response.StatusCode != status) throw new InvalidOperationException("Pinned HTTPS qualification returned an unexpected status.");
                using (var input = new StreamReader(response.Content.ReadAsStream())) {
                    return JsonDocument.Parse(ReadBounded(input, 65536).WaitAsync(timeout.Token).GetAwaiter().GetResult());
                }
            }
        }
    }
    public static StackfortOnboardRedemption Redeem(string address, string publicCertificate, SecureString setup, Action<HttpClient, Uri, string> smoke) {
        if (smoke == null) throw new InvalidOperationException("The in-memory installed API smoke callback is required.");
        if (publicCertificate.Contains("PRIVATE KEY")) throw new InvalidOperationException("Public certificate export unexpectedly contained private material.");
        using (var certificate = X509Certificate2.CreateFromPem(publicCertificate))
        using (var handler = new HttpClientHandler { AllowAutoRedirect = false, UseProxy = false, UseCookies = true, CookieContainer = new CookieContainer() }) {
            byte[] pin = SHA256.HashData(certificate.RawData);
            handler.ServerCertificateCustomValidationCallback = (request, peer, chain, errors) => peer != null &&
                (errors & (SslPolicyErrors.RemoteCertificateNotAvailable | SslPolicyErrors.RemoteCertificateNameMismatch)) == 0 &&
                DateTime.UtcNow >= peer.NotBefore.ToUniversalTime() && DateTime.UtcNow <= peer.NotAfter.ToUniversalTime() &&
                CryptographicOperations.FixedTimeEquals(pin, SHA256.HashData(peer.RawData));
            using (var client = new HttpClient(handler) { BaseAddress = new Uri("https://" + address + ":8443/"), Timeout = TimeSpan.FromSeconds(45) }) {
                using (var before = Request(client, HttpMethod.Get, "api/v1/bootstrap", null, 200)) {
                    if (!before.RootElement.GetProperty("required").GetBoolean() || !before.RootElement.GetProperty("capabilityActive").GetBoolean())
                        throw new InvalidOperationException("Original setup capability was not active before redemption.");
                }
                IntPtr pointer = Marshal.SecureStringToGlobalAllocUnicode(setup);
                string code = null, password = null, bootstrapBody = null, loginBody = null;
                byte[] passwordBytes = RandomNumberGenerator.GetBytes(32);
                string phase = "bootstrap-redemption";
                try {
                    code = Marshal.PtrToStringUni(pointer);
                    password = Convert.ToBase64String(passwordBytes) + "aA1!";
                    const string email = "native-candidate-qualification@example.invalid";
                    bootstrapBody = JsonSerializer.Serialize(new { token = code, email, displayName = "Disposable Native Qualification", password, locale = "en" });
                    string identity;
                    using (var created = Request(client, HttpMethod.Post, "api/v1/bootstrap", bootstrapBody, 201)) {
                        identity = created.RootElement.GetProperty("id").GetString();
                        if (!Guid.TryParseExact(identity, "D", out _) || created.RootElement.GetProperty("email").GetString() != email)
                            throw new InvalidOperationException("Bootstrap returned an unexpected administrator identity.");
                    }
                    phase = "bootstrap-consumption-and-replay";
                    using (var after = Request(client, HttpMethod.Get, "api/v1/bootstrap", null, 200)) {
                        if (after.RootElement.GetProperty("required").GetBoolean() || after.RootElement.GetProperty("capabilityActive").GetBoolean())
                            throw new InvalidOperationException("Bootstrap remained active after administrator creation.");
                    }
                    using (var replay = Request(client, HttpMethod.Post, "api/v1/bootstrap", bootstrapBody, 409)) {
                        if (replay.RootElement.GetProperty("code").GetString() != "bootstrap_disabled") throw new InvalidOperationException("Setup capability replay was not rejected.");
                    }
                    phase = "administrator-login-and-session";
                    loginBody = JsonSerializer.Serialize(new { email, password });
                    using (var login = Request(client, HttpMethod.Post, "api/v1/login", loginBody, 200)) {
                        if (login.RootElement.GetProperty("identity").GetProperty("id").GetString() != identity) throw new InvalidOperationException("New administrator login did not match.");
                    }
                    using (var session = Request(client, HttpMethod.Get, "api/v1/session", null, 200)) {
                        if (session.RootElement.GetProperty("identity").GetProperty("id").GetString() != identity) throw new InvalidOperationException("New administrator session did not match.");
                    }
                    var csrf = handler.CookieContainer.GetCookies(client.BaseAddress)["__Host-sf-csrf"];
                    if (csrf == null || String.IsNullOrEmpty(csrf.Value)) throw new InvalidOperationException("Authenticated CSRF cookie is missing.");
                    // The synchronous callback receives the live in-memory client
                    // only while this scope owns it; no session object is returned.
                    phase = "installed-product-callback";
                    smoke(client, client.BaseAddress, csrf.Value);
                    return new StackfortOnboardRedemption { AdministratorID = identity, CertificateSHA256 = Convert.ToHexString(pin).ToLowerInvariant(), Redeemed = true, ReuseRejected = true, LoginVerified = true };
                } catch { throw new InvalidOperationException("Pinned HTTPS qualification failed at phase '" + phase + "'; credentials and responses were deliberately not logged."); }
                finally { Marshal.ZeroFreeGlobalAllocUnicode(pointer); Array.Clear(passwordBytes, 0, passwordBytes.Length); code = password = bootstrapBody = loginBody = null; }
            }
        }
    }
}
'@
}

$taskVMName = 'stackfort-native-quota-debian-13'
$taskVM = Get-VM -Name $taskVMName
if ($taskVM.Id -ne '4361f439-15e9-4f9e-a690-9a8e44b6cbd3' -or $taskVM.State -ne 'Running') { throw 'Requires the exact already-running disposable Debian VM.' }
if ((Get-VM -Name 'stackfort-native-restore-rescue').State -ne 'Off') { throw 'The duplicate-identity restored clone must remain off.' }
$taskSSHPath = 'C:\Windows\System32\OpenSSH\ssh.exe'
$taskSCPPath = 'C:\Windows\System32\OpenSSH\scp.exe'
$taskSSH = @('-o', 'BatchMode=yes', '-o', 'IdentitiesOnly=yes', '-o', 'ConnectTimeout=5', '-o', 'StrictHostKeyChecking=yes',
    '-o', 'UserKnownHostsFile=C:\ProgramData\Stackfort\Hyper-V\known_hosts', '-o', "HostKeyAlias=$taskVMName", '-o', 'LogLevel=ERROR',
    '-i', 'C:\ProgramData\Stackfort\Hyper-V\keys\stackfort-host-test-ed25519')
$taskRepository = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '../..')).Path
$taskSmokePath = Join-Path $PSScriptRoot 'Invoke-StackfortInstalledApiSmoke.ps1'
if (-not (Test-Path -LiteralPath $taskSmokePath -PathType Leaf)) { throw 'The installed API smoke helper must be present before qualification.' }
. $taskSmokePath
foreach ($requiredFunction in @('Invoke-StackfortInstalledApiSmoke', 'Test-StackfortInstalledApiPersistence')) {
    if (-not (Get-Command -Name $requiredFunction -CommandType Function -ErrorAction SilentlyContinue)) { throw 'The installed API qualification helper contract is incomplete.' }
}
$taskBootstrap = Join-Path $taskRepository 'packaging/installer/install.sh'
$taskArchiveDirectory = (Resolve-Path -LiteralPath $ArchiveDirectory).Path
$taskBundle = "stackfort-$Version-linux-amd64"
$taskArchiveName = "$taskBundle.tar.gz"
$taskAssets = @(
    @{ Name = $taskArchiveName; Path = (Join-Path $taskArchiveDirectory $taskArchiveName); SHA = $ArchiveSHA256; Limit = 536870912 },
    @{ Name = 'SHA256SUMS'; Path = (Join-Path $taskArchiveDirectory 'SHA256SUMS'); SHA = $ChecksumsSHA256; Limit = 1048576 },
    @{ Name = 'build-attestation.jsonl'; Path = (Join-Path $taskArchiveDirectory 'build-attestation.jsonl'); SHA = $AttestationSHA256; Limit = 16777216 },
    @{ Name = 'install.sh'; Path = $taskBootstrap; SHA = $BootstrapSHA256; Limit = 1048576 }
)
foreach ($asset in $taskAssets) {
    $item = Get-Item -LiteralPath $asset.Path
    if ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or $item.Length -lt 1 -or $item.Length -gt $asset.Limit -or
        (Get-FileHash -LiteralPath $asset.Path -Algorithm SHA256).Hash.ToLowerInvariant() -ne $asset.SHA) { throw 'An exact candidate input is missing, unsafe or differs from its mandatory hash pin.' }
}
$taskChecksumRows = @(Get-Content -LiteralPath (Join-Path $taskArchiveDirectory 'SHA256SUMS') | Where-Object { $_ -match ('^[0-9a-f]{64}[ \t]+(\./)?' + [regex]::Escape($taskArchiveName) + '$') })
if ($taskChecksumRows.Count -ne 1 -or $taskChecksumRows[0].Substring(0, 64) -ne $ArchiveSHA256) { throw 'The candidate checksum manifest does not uniquely bind the selected archive.' }
$taskGit = (Get-Command git.exe -ErrorAction Stop).Source
$taskExpectedBlob = [StackfortNativeOnboardProbeV1]::Run($taskGit, @('-C', $taskRepository, 'rev-parse', "${Commit}:packaging/installer/install.sh"), 15)
$taskActualBlob = [StackfortNativeOnboardProbeV1]::Run($taskGit, @('-C', $taskRepository, 'hash-object', '--', $taskBootstrap), 15)
if ($taskExpectedBlob.ExitCode -ne 0 -or $taskActualBlob.ExitCode -ne 0 -or $taskExpectedBlob.Output.Trim() -notmatch '^[0-9a-f]{40}$' -or $taskExpectedBlob.Output.Trim() -ne $taskActualBlob.Output.Trim()) { throw 'Bootstrap bytes do not match the exact candidate commit.' }

function Get-OnboardAddress {
    $adapter = Get-VMNetworkAdapter -VMName $taskVMName | Select-Object -First 1
    $candidate = $adapter.IPAddresses |
        Where-Object { $_ -match '^\d{1,3}(\.\d{1,3}){3}$' -and $_ -notlike '169.254.*' } | Select-Object -First 1
    if ([string]::IsNullOrEmpty($candidate)) {
        $mac = $adapter.MacAddress -replace '(.{2})(?!$)', '$1-'
        $candidate = Get-NetNeighbor -InterfaceAlias "vEthernet ($($adapter.SwitchName))" -AddressFamily IPv4 -ErrorAction SilentlyContinue |
            Where-Object { $_.LinkLayerAddress -eq $mac -and $_.State -ne 'Unreachable' -and $_.IPAddress -notlike '169.254.*' } |
            Select-Object -ExpandProperty IPAddress -First 1
    }
    if ([string]::IsNullOrEmpty($candidate)) { return $null }
    return $candidate
}
function Invoke-OnboardSSH([string] $Address, [string] $Command, [int] $Seconds = 30) {
    return [StackfortNativeOnboardProbeV1]::Run($taskSSHPath, @($taskSSH) + @("stackfort-test@$Address", $Command), $Seconds)
}
function Wait-OnboardCompleted([string] $PreviousBoot, [string] $ConversionBoot = '', [int] $Minutes = 25, [switch] $RequireActiveSetup) {
    $deadline = [DateTime]::UtcNow.AddMinutes($Minutes)
    do {
        $address = Get-OnboardAddress
        $poll = $null
        if ($address) {
            # A restarting sshd/network stack is expected. Only successful reads
            # are parsed; timeouts/connection failures consume the bounded wait.
            try { $poll = Invoke-OnboardSSH $address 'cat /proc/sys/kernel/random/boot_id && sudo -n systemctl is-active --quiet stackfort-native-install.service && sudo -n test -f /var/lib/stackfort-installer/native-setup-registered.json' 15 } catch { $poll = $null }
        }
        if ($null -ne $poll -and $poll.ExitCode -eq 0) {
            $boot = $poll.Output.Trim()
            if ($boot -ne $PreviousBoot -and $boot -match '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$') {
                $rawRecords = @{}; $readable = $true
                foreach ($name in @('storage-state.json', 'install-state.json', 'native-setup.json', 'native-setup-registered.json')) {
                    try { $record = Invoke-OnboardSSH $address "sudo -n cat /var/lib/stackfort-installer/$name" 15 } catch { $readable = $false; break }
                    if ($record.ExitCode -ne 0) { $readable = $false; break }
                    $rawRecords[$name] = $record.Output
                }
                if ($readable) {
                    try {
                        $storage = $rawRecords['storage-state.json'] | ConvertFrom-Json
                        $installed = $rawRecords['install-state.json'] | ConvertFrom-Json
                        $setup = $rawRecords['native-setup.json'] | ConvertFrom-Json
                        $registered = $rawRecords['native-setup-registered.json'] | ConvertFrom-Json
                        $setupDigest = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($rawRecords['native-setup.json']))).ToLowerInvariant()
                        $conversion = if ($ConversionBoot -eq '') { $boot } else { $ConversionBoot }
                        # PowerShell 7.2 retains JSON date strings; newer versions
                        # can deserialize them as DateTime. Cast without a lossy
                        # culture-dependent DateTime -> string -> date roundtrip.
                        $created = [DateTimeOffset] $registered.capability.createdAt
                        $expires = [DateTimeOffset] $registered.capability.expiresAt
                        if ($storage.phase -ne 'ready' -or $storage.armAttempts -ne 1 -or $storage.plan.operationId -ne $taskCapture.OperationID -or $storage.plan.version -ne $Version -or $storage.resumeBootId -ne $conversion -or
                            $storage.plan.previousBootId -ne $taskInitialBoot -or $storage.plan.machineId -ne $taskIdentity[1] -or $storage.plan.rootUuid -ne $taskIdentity[2] -or $storage.plan.partitionUuid -ne $taskIdentity[3] -or
                            $installed.status -ne 'complete' -or $installed.version -ne $Version -or $installed.sourceDigest -ne $storage.plan.sourceDigest -or
                            $setup.operationId -ne $taskCapture.OperationID -or $setup.tokenSHA256 -ne $taskCapture.SetupSHA256 -or $registered.operationId -ne $taskCapture.OperationID -or
                            $registered.setupSHA256 -ne $setupDigest -or ($expires - $created) -ne [TimeSpan]::FromHours(1) -or ($RequireActiveSetup -and $expires -le [DateTimeOffset]::UtcNow)) {
                            throw 'Completion receipt binding mismatch.'
                        }
                        return [pscustomobject]@{ Address = $address; BootID = $boot; ConversionBootID = $conversion; CapabilityID = $registered.capability.id; CapabilityExpiresAt = $expires.ToString('o') }
                    } catch { throw 'Completed native/setup receipts are malformed or differ from the acknowledged operation; preserve the guest.' }
                }
            }
        }
        Start-Sleep -Seconds 3
    } while ([DateTime]::UtcNow -lt $deadline)
    throw 'Native installation/readmission did not complete within the bounded window; preserve the VM and all journals.'
}
$taskAddress = Get-OnboardAddress
if (-not $taskAddress) { throw 'The verified VM has no usable IPv4 address.' }
$taskHost = Invoke-OnboardSSH $taskAddress 'sudo -n test ! -e /var/lib/stackfort-installer && sudo -n test ! -L /var/lib/stackfort-installer && cat /proc/sys/kernel/random/boot_id && sudo -n cat /sys/class/dmi/id/product_uuid && sudo -n blkid -s UUID -o value -- "$(findmnt -nro SOURCE /)" && sudo -n blkid -s PARTUUID -o value -- "$(findmnt -nro SOURCE /)"'
$taskIdentity = @($taskHost.Output.Trim().ToLowerInvariant() -split '\r?\n')
if ($taskHost.ExitCode -ne 0 -or $taskIdentity.Count -ne 4 -or @($taskIdentity | Where-Object { $_ -notmatch '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' }).Count -ne 0) { throw 'Fresh-state or live host identity verification failed; no onboarding was started.' }
$taskInitialBoot = $taskIdentity[0]
$taskRunID = [Guid]::NewGuid().ToString('N')
$taskUpload = "/tmp/stackfort-onboard-upload-$taskRunID"
$taskFixture = "/var/tmp/stackfort-onboard-qualification-$taskRunID"
$taskCreate = Invoke-OnboardSSH $taskAddress "umask 077 && mkdir -- '$taskUpload' && sudo -n mkdir -m 0700 -- '$taskFixture'"
if ($taskCreate.ExitCode -ne 0) { throw 'Could not exclusively create qualification staging directories.' }
foreach ($asset in $taskAssets) {
    $transfer = [StackfortNativeOnboardProbeV1]::Run($taskSCPPath, @('-S', $taskSSHPath) + @($taskSSH) + @($asset.Path, "stackfort-test@${taskAddress}:$taskUpload/$($asset.Name)"), 300)
    if ($transfer.ExitCode -ne 0) { throw 'Exact candidate transfer failed; preserve the staging directory.' }
    $copied = Invoke-OnboardSSH $taskAddress "sudo -n install -m 0600 -- '$taskUpload/$($asset.Name)' '$taskFixture/$($asset.Name)' && sudo -n sha256sum -- '$taskFixture/$($asset.Name)'" 60
    if ($copied.ExitCode -ne 0 -or $copied.Output.Trim() -ne "$($asset.SHA)  $taskFixture/$($asset.Name)") { throw 'Root-owned guest candidate bytes differ from the supplied hash pin.' }
}
$taskInstallerDigest = Invoke-OnboardSSH $taskAddress "sudo -n /bin/bash -o pipefail -c 'tar -xOzf $taskFixture/$taskArchiveName $taskBundle/bin/stackfort-installer | sha256sum'" 60
$taskInstallerSHA = $taskInstallerDigest.Output.Trim().Split(' ')[0]
if ($taskInstallerDigest.ExitCode -ne 0 -or $taskInstallerSHA -notmatch '^[0-9a-f]{64}$') { throw 'Could not bind the reviewed installer executable to the exact candidate archive.' }

$taskCapture = $null
$script:taskOnboardPhase = 'native-onboarding'
try {
    Write-Host 'Starting exact-tag native onboarding on the verified disposable VM; terminal output and setup secrets will not be logged.'
    $taskCommand = "sudo -n env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin HOME=/root LANG=C LC_ALL=C STACKFORT_VERSION=$Version STACKFORT_BOOTSTRAP_TESTING=1 STACKFORT_BOOTSTRAP_TEST_FIXTURE=$taskFixture /bin/bash $taskFixture/install.sh"
    $taskCapture = [StackfortNativeOnboardProbeV1]::Onboard($taskSSHPath, @($taskSSH) + @('-tt', "stackfort-test@$taskAddress", $taskCommand),
        $Version, $Commit, $taskIdentity[1], $taskIdentity[2], $taskIdentity[3], $taskInitialBoot, $taskInstallerSHA)
    Write-Host 'Exact interactive review acknowledged and original setup code retained only in process memory; waiting for the authorized reboot/install.'
    $taskCompletion = Wait-OnboardCompleted -PreviousBoot $taskInitialBoot -RequireActiveSetup
    $taskAddress = $taskCompletion.Address
    $taskFinalBoot = $taskCompletion.BootID
    $taskCertificate = Invoke-OnboardSSH $taskAddress 'sudo -n openssl x509 -in /etc/stackfort/panel-tls/bootstrap.pem -outform PEM' 15
    if ($taskCertificate.ExitCode -ne 0 -or -not $taskCertificate.Output.StartsWith('-----BEGIN CERTIFICATE-----') -or $taskCertificate.Output.Contains('PRIVATE KEY')) { throw 'SSH-authenticated public certificate export failed.' }
    $script:taskOnboardApiSmoke = $null
    $script:taskOnboardPersistence = $null
    $script:taskOnboardPhase = 'bootstrap-redemption'
    Write-Host 'STACKFORT_QUALIFICATION_PHASE bootstrap-redemption-login-session'
    $taskSmoke = [Action[System.Net.Http.HttpClient, Uri, string]] {
        param($Client, $BaseUri, $CsrfToken)
        $script:taskOnboardPhase = 'installed-api-smoke'
        Write-Host 'STACKFORT_QUALIFICATION_PHASE installed-api-smoke'
        $script:taskOnboardApiSmoke = Invoke-StackfortInstalledApiSmoke -Client $Client -BaseUri $BaseUri -CsrfToken $CsrfToken
        $script:taskOnboardPhase = 'same-release-rerun'
        Write-Host 'STACKFORT_QUALIFICATION_PHASE same-release-rerun'
        $rerun = Invoke-OnboardSSH $taskAddress $taskCommand 480
        if ($rerun.ExitCode -ne 0 -or -not $rerun.Output.Contains('Stackfort is already installed. Live native admission checks passed;') -or $rerun.Output.Contains('sfb_')) { throw 'Same-release native rerun failed or attempted setup reissue.' }
        $sameBoot = Invoke-OnboardSSH $taskAddress 'cat /proc/sys/kernel/random/boot_id' 15
        if ($sameBoot.ExitCode -ne 0 -or $sameBoot.Output.Trim() -cne $taskFinalBoot) { throw 'Same-release native rerun changed the current boot.' }
        $afterRerun = Test-StackfortInstalledApiPersistence -Client $Client -BaseUri $BaseUri -Evidence $script:taskOnboardApiSmoke
        $script:taskOnboardPhase = 'normal-reboot'
        Write-Host 'STACKFORT_QUALIFICATION_PHASE normal-reboot'
        $reboot = Invoke-OnboardSSH $taskAddress 'sudo -n systemctl reboot' 30
        if ($reboot.ExitCode -notin @(0, 255)) { throw 'Normal reboot request failed; preserve guest state.' }
        $readmitted = Wait-OnboardCompleted -PreviousBoot $taskFinalBoot -ConversionBoot $taskCompletion.ConversionBootID -Minutes 8
        if ($readmitted.Address -ne $BaseUri.Host -or $readmitted.CapabilityID -ne $taskCompletion.CapabilityID -or $readmitted.CapabilityExpiresAt -ne $taskCompletion.CapabilityExpiresAt) { throw 'Normal reboot changed lab address or renewed setup registration.' }
        $script:taskOnboardPhase = 'post-reboot-persistence'
        Write-Host 'STACKFORT_QUALIFICATION_PHASE post-reboot-persistence'
        $afterReboot = Test-StackfortInstalledApiPersistence -Client $Client -BaseUri $BaseUri -Evidence $script:taskOnboardApiSmoke
        $script:taskOnboardPersistence = [pscustomobject]@{ SameReleaseRerun = $afterRerun; NormalReboot = $afterReboot; FinalBootID = $readmitted.BootID }
    }
    $taskRedemption = [StackfortNativeOnboardProbeV1]::Redeem($taskAddress, $taskCertificate.Output, $taskCapture.SetupCode, $taskSmoke)
    $taskResult = [pscustomobject]@{
        Stage = $Stage; VM = $taskVMName; Version = $Version; Commit = $Commit; ArchiveSHA256 = $ArchiveSHA256; AttestationSHA256 = $AttestationSHA256;
        BootstrapSHA256 = $BootstrapSHA256; InstallerSHA256 = $taskInstallerSHA; OperationID = $taskCapture.OperationID; ReviewSHA256 = $taskCapture.ReviewSHA256;
        InitialBootID = $taskInitialBoot; ConversionBootID = $taskFinalBoot; FinalBootID = $script:taskOnboardPersistence.FinalBootID; CertificateSHA256 = $taskRedemption.CertificateSHA256;
        OriginalSetupRedeemed = $taskRedemption.Redeemed; SetupReplayRejected = $taskRedemption.ReuseRejected; AdministratorLoginVerified = $taskRedemption.LoginVerified;
        AdministratorID = $taskRedemption.AdministratorID; InstalledApiSmoke = $script:taskOnboardApiSmoke; Persistence = $script:taskOnboardPersistence; CredentialsRetained = $false; RawTranscriptRetained = $false
    }
    if ($ReturnRedactedResult) { Write-Output $taskResult } else { Write-Host 'Exact-tag native onboarding, original setup redemption, installed API checks, same-release rerun and normal reboot persistence passed. No raw credentials or terminal transcript were retained.' }
} catch {
    throw "Exact-tag qualification failed at phase '$script:taskOnboardPhase'; preserve the VM. Raw responses, terminal transcript and credentials were not logged."
} finally {
    if ($null -ne $taskCapture) { $taskCapture.Dispose(); $taskCapture = $null }
}
