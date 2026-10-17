# Precog smoke test: builds and starts cmd/testserver, then fires realistic
# traffic at it and asserts every behavior. Exits non-zero on any failure.
#
# Usage:  powershell -ExecutionPolicy Bypass -File scripts\smoke.ps1
param(
    [int]$Port = 8099
)

$ErrorActionPreference = "Stop"
$base = "http://localhost:$Port"
$exe = Join-Path $env:TEMP "precog-testserver.exe"
$jar = Join-Path $env:TEMP "precog-cookies.txt"
$log = Join-Path $env:TEMP "precog-testserver.log"

$script:failures = 0

function Check($name, $ok, $detail) {
    if ($ok) { Write-Host ("  PASS  {0}" -f $name) -ForegroundColor Green }
    else {
        Write-Host ("  FAIL  {0}  [{1}]" -f $name, $detail) -ForegroundColor Red
        $script:failures++
    }
}

# GET $url with the session cookie jar; returns @{ Status, Time, Headers, Body }
function Req($url, $method = "GET", $data = $null) {
    $hdrFile = [System.IO.Path]::GetTempFileName()
    $bodyFile = [System.IO.Path]::GetTempFileName()
    $curlArgs = @("-s", "-S", "-D", $hdrFile, "-o", $bodyFile,
        "-b", $jar, "-c", $jar, "-w", "%{http_code} %{time_total}", $url)
    if ($method -eq "POST") { $curlArgs = @("-s", "-S", "-D", $hdrFile, "-o", $bodyFile,
        "-b", $jar, "-c", $jar, "-X", "POST", "-d", $data, "-w", "%{http_code} %{time_total}", $url) }

    $meta = & curl.exe @curlArgs
    $parts = $meta.Trim().Split(" ")
    $headers = @{}
    foreach ($line in (Get-Content $hdrFile)) {
        if ($line -match "^([^:]+):\s*(.*)$") { $headers[$matches[1].ToLower()] = $matches[2].Trim() }
    }
    $body = Get-Content $bodyFile -Raw
    Remove-Item $hdrFile, $bodyFile -ErrorAction SilentlyContinue
    return @{ Status = [int]$parts[0]; Time = [double]$parts[1]; Headers = $headers; Body = $body }
}

function Get-Stats {
    Invoke-RestMethod -Uri "$base/stats" -TimeoutSec 5
}

# ---- Build and start the server -------------------------------------------
Write-Host "Building test server..."
& go build -o $exe ./cmd/testserver
if ($LASTEXITCODE -ne 0) { throw "go build failed" }

Remove-Item $jar, "$log.out", "$log.err" -ErrorAction SilentlyContinue
$p = Start-Process -FilePath $exe -ArgumentList "-port=$Port" -PassThru -WindowStyle Hidden -RedirectStandardOutput "$log.out" -RedirectStandardError "$log.err"
try {
    $ready = $false
    for ($i = 0; $i -lt 50; $i++) {
        try { Get-Stats | Out-Null; $ready = $true; break } catch { Start-Sleep -Milliseconds 100 }
    }
    if (-not $ready) { throw "test server did not become ready on $base" }
    Write-Host "Server ready on $base`n"

    # ---- 1. MISS then HIT ----------------------------------------------------
    $r1 = Req "$base/api/products/42"
    Check "first request is a MISS" ($r1.Headers["x-cache"] -eq "MISS") ("x-cache=" + $r1.Headers["x-cache"])
    Check "first request honors handler latency" ($r1.Time -gt 0.1) ("time=" + $r1.Time)

    $r2 = Req "$base/api/products/42"
    Check "second request is a HIT" ($r2.Headers["x-cache"] -eq "HIT") ("x-cache=" + $r2.Headers["x-cache"])
    Check "HIT is dramatically faster" ($r2.Time -lt 0.05) ("time=" + $r2.Time)
    Check "HIT preserves Content-Type" ($r2.Headers["content-type"] -like "application/json*") ($r2.Headers["content-type"])

    # ---- 2. Speculation Link hint on first (MISS) response --------------------
    Check "Link prefetch hint present on MISS" ($r1.Headers["link"] -like "*/api/cart*rel=prefetch*") ("link=" + $r1.Headers["link"])

    # ---- 3. Speculative pre-warming of /api/cart -------------------------------
    Start-Sleep -Seconds 1
    $r3 = Req "$base/api/cart"
    Check "predicted /api/cart pre-warmed (HIT)" ($r3.Headers["x-cache"] -eq "HIT") ("x-cache=" + $r3.Headers["x-cache"])
    Check "pre-warmed HIT is instant" ($r3.Time -lt 0.05) ("time=" + $r3.Time)

    # ---- 4. POST pass-through (never cached) ----------------------------------
    $p1 = Req "$base/api/checkout" "POST" '{"order":1}'
    $p2 = Req "$base/api/checkout" "POST" '{"order":2}'
    Check "POST returns 200" ($p1.Status -eq 200) ("status=" + $p1.Status)
    Check "POST not cached (both MISS)" ($p1.Headers["x-cache"] -eq "MISS" -and $p2.Headers["x-cache"] -eq "MISS") ("p1=" + $p1.Headers["x-cache"] + " p2=" + $p2.Headers["x-cache"])
    Check "POST body passes through" ($p2.Body -match '"bytes":9') ($p2.Body)
    $stats = Get-Stats
    Check "POST executed once per request" ($stats.handler_calls."/api/checkout" -eq 2) ("calls=" + $stats.handler_calls."/api/checkout")

    # ---- 5. Set-Cookie responses are never shared-cached -----------------------
    $v1 = Req "$base/api/private"
    $v2 = Req "$base/api/private"
    Check "Set-Cookie response not cached (2nd is MISS)" ($v2.Headers["x-cache"] -eq "MISS") ("x-cache=" + $v2.Headers["x-cache"])
    Check "Set-Cookie handler ran fresh both times" ($v2.Body -match "token-2") ($v2.Body)
    Check "fresh Set-Cookie issued (token-2, not token-1 replay)" ($v2.Headers["set-cookie"] -match "token-2") ($v2.Headers["set-cookie"])

    # ---- 6. In-flight coalescing: 5 concurrent requests, 1 handler call --------
    $before = (Get-Stats).handler_calls."/api/slow"
    $procs = 1..5 | ForEach-Object {
        Start-Process -FilePath "curl.exe" -ArgumentList @("-s", "-o", "NUL", "$base/api/slow") -PassThru -WindowStyle Hidden
    }
    $procs | Wait-Process -Timeout 15 -ErrorAction SilentlyContinue
    Start-Sleep -Milliseconds 100
    $after = (Get-Stats).handler_calls."/api/slow"
    Check "5 concurrent requests coalesced into 1 handler call" (($after - $before) -eq 1) ("delta=" + ($after - $before))

    # ---- 7. Telemetry ----------------------------------------------------------
    $stats = Get-Stats
    Check "telemetry: cache hits recorded" ($stats.engine.cache_hits -ge 2) ("hits=" + $stats.engine.cache_hits)
    Check "telemetry: prefetches executed" ($stats.engine.prefetches_executed -ge 1) ("prefetches=" + $stats.engine.prefetches_executed)
    Check "telemetry: hits + misses == total requests" (($stats.engine.cache_hits + $stats.engine.cache_misses) -eq $stats.engine.total_requests) ("hits={0} misses={1} total={2}" -f $stats.engine.cache_hits, $stats.engine.cache_misses, $stats.engine.total_requests)

    Write-Host ""
    Write-Host ("Engine stats: hits={0} misses={1} prefetches={2} hitRate={3:P1}" -f `
        $stats.engine.cache_hits, $stats.engine.cache_misses, $stats.engine.prefetches_executed, $stats.engine.hit_rate)
}
finally {
    if ($p -and -not $p.HasExited) { Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue }
}

Write-Host ""
if ($script:failures -gt 0) {
    Write-Host ("SMOKE TEST FAILED: {0} check(s) failed" -f $script:failures) -ForegroundColor Red
    exit 1
}
Write-Host "SMOKE TEST PASSED: all checks green" -ForegroundColor Green

