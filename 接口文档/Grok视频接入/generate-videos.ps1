# ============================================================================
# Grok2API Batch Video Generation PowerShell Script (Polling Mode, OpenAI Flow)
# ----------------------------------------------------------------------------
# Usage:
#   1. Edit the variables below: $sites, $videosPerSite, $refImage1Path, etc.
#   2. Run in PowerShell: .\generate-videos.ps1
#
# Workflow (OpenAI standard 3 steps):
#   POST /v1/videos            -> returns immediately with {id, status: "queued"}
#   GET  /v1/videos/{id}       -> poll until status=completed or failed
#   GET  /v1/files/video?id=   -> download final MP4
#
# All HTTP requests complete within 60s, avoiding CF/newapi long-connection timeouts.
# ============================================================================

[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
[Console]::InputEncoding  = [System.Text.Encoding]::UTF8
$OutputEncoding           = [System.Text.Encoding]::UTF8
chcp 65001 | Out-Null

# ========== Configuration ==========

$runId   = Get-Date -Format 'yyyyMMdd_HHmmss'
$saveDir = "C:\Users\Administrator\Desktop\videos\run_$runId"
$logFile = Join-Path $saveDir "run_log.txt"
New-Item -ItemType Directory -Force -Path $saveDir | Out-Null

# Site config (duplicate lines for multiple sites)
$sites = @(
    @{ url = "https://ai.xitongsp.top"; apiKey = "sk-BuUC64Q1PULXMDgfdSrThDBkm4AY8vn8sDpKSO85RzoDgNZI"; password = "AnWT1NRMiys2mxFd" }
)

$videosPerSite = 2                  # videos per site
$maxConcurrent = 600                # max concurrent jobs
$pollInterval  = 5                  # poll interval (seconds)
$maxWaitSec    = 600                # max wait per job (seconds)

# Reference images (2+ images billed at 4/3 rate by the server)
# NOTE: update these paths to match your actual filenames
$refImage1Path = "D:\grok\person1.jpeg"
$refImage2Path = "D:\grok\judge.jpg"

# Prompt list (assigned to each video in a round-robin fashion)
$prompts = @(
    "Zhang San stands at the defendant's dock, judge reading the verdict aloud, solemn courtroom atmosphere, cinematic quality",
    "Zhang San and his lawyer in urgent discussion in the courthouse corridor, sunlight streaming through windows, tense atmosphere",
    "Judge strikes the gavel, Zhang San looks tense, audience murmuring in the gallery, wide courtroom shot",
    "Zhang San testifying in the witness box, judge listening attentively, lawyer taking notes, side-angle close-up",
    "Court in recess, Zhang San sits alone at the dock with head bowed in thought, empty courtroom, melancholy mood",
    "Judge reviewing case files with a grave expression, courtroom lights shining from above, close-up shot",
    "Zhang San's lawyer delivering a passionate defense, judge frowning slightly, full courtroom view, dramatic tension",
    "Verdict announcement moment, Zhang San slowly rises to his feet, all eyes on him, slow motion, cinematic feel",
    "Zhang San exits the courthouse, reporters swarm around him, camera flashes everywhere, chaotic scene",
    "Judge alone in office carefully reviewing case materials, warm desk lamp, bookshelf background, composed atmosphere"
)

# ========== No need to edit below ==========

# Load reference images as data URIs
$refImage1Bytes   = [System.IO.File]::ReadAllBytes($refImage1Path)
$refImage1Base64  = [Convert]::ToBase64String($refImage1Bytes)
$refImage1DataUrl = "data:image/jpeg;base64,$refImage1Base64"

$refImage2Bytes   = [System.IO.File]::ReadAllBytes($refImage2Path)
$refImage2Base64  = [Convert]::ToBase64String($refImage2Bytes)
$refImage2DataUrl = "data:image/jpeg;base64,$refImage2Base64"

# Logging function
$logMutex = New-Object System.Threading.Mutex($false, "LogMutex_$(Get-Random)")
function Log {
    param([string]$msg)
    $line = "[$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')] $msg"
    Write-Host $line
    $logMutex.WaitOne() | Out-Null
    try { Add-Content -Path $logFile -Value $line -Encoding UTF8 }
    finally { $logMutex.ReleaseMutex() }
}

$globalTimer = [System.Diagnostics.Stopwatch]::StartNew()
Log "==================== Batch Job Started (Polling Mode) ===================="
Log "Sites: $($sites.Count)  Videos/site: $videosPerSite  Total: $($sites.Count * $videosPerSite)"
Log "Ref image 1: $refImage1Path"
Log "Ref image 2: $refImage2Path"
Log "Prompts: $($prompts.Count) (round-robin)"
Log "Poll interval: ${pollInterval}s  Max wait per job: ${maxWaitSec}s"
Log "Log file: $logFile"
Log ""

for ($s = 0; $s -lt $sites.Count; $s++) {
    $site = $sites[$s]
    Log ("Site {0:D2} | URL: {1}" -f ($s + 1), $site.url)
}
Log ""

# Create output directory for each site
for ($s = 0; $s -lt $sites.Count; $s++) {
    $site = $sites[$s]
    $siteIndex = $s + 1
    $siteDir = Join-Path $saveDir ("site_{0:D2}_{1}" -f $siteIndex, ($site.url -replace 'http://', '' -replace '[:\/]', '_'))
    New-Item -ItemType Directory -Force -Path $siteDir | Out-Null
}

# RunspacePool for concurrency
$runspacePool = [RunspaceFactory]::CreateRunspacePool(1, $maxConcurrent)
$runspacePool.Open()

# ============================================================================
# Per-job scriptBlock: create -> poll -> download
# ============================================================================
$scriptBlock = {
    param($apiKey, $baseUrl, $prompt, $siteIndex, $videoIndex, $siteDir, $siteUrl, $password,
          $imageDataUrl1, $imageDataUrl2, $pollInterval, $maxWaitSec)

    [Console]::OutputEncoding = [System.Text.Encoding]::UTF8
    [Console]::InputEncoding  = [System.Text.Encoding]::UTF8
    $OutputEncoding           = [System.Text.Encoding]::UTF8

    $headers = @{
        "Content-Type"  = "application/json; charset=utf-8"
        "Authorization" = "Bearer $apiKey"
    }
    $body = @{
        model   = "grok-imagine-1.0-video"
        prompt  = $prompt
        size    = "1792x1024"
        seconds = "10"
        quality = "standard"
        image_references = @(
            @{ image_url = $imageDataUrl1 },
            @{ image_url = $imageDataUrl2 }
        )
    } | ConvertTo-Json -Depth 5
    # PowerShell 5.1 ConvertTo-Json escapes non-ASCII as \uXXXX; restore actual UTF-8 chars
    $body = [System.Text.RegularExpressions.Regex]::Replace(
        $body, '\\u([0-9a-fA-F]{4})',
        [System.Text.RegularExpressions.MatchEvaluator]{
            [char][convert]::ToInt32($args[0].Groups[1].Value, 16)
        }
    )

    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    $result = [ordered]@{
        siteIndex  = $siteIndex
        videoIndex = $videoIndex
        siteUrl    = $siteUrl
        apiKey     = $apiKey
        password   = $password
        prompt     = $prompt
        success    = $false
        jobId      = $null
        videoUrl   = $null
        file       = $null
        error      = $null
        elapsed    = $null
        startTime  = (Get-Date -Format 'yyyy-MM-dd HH:mm:ss')
        endTime    = $null
    }

    # ---------- Step 1: Create job (returns queued within a few seconds) ----------
    $job = $null
    $createRetry = 3
    for ($a = 1; $a -le $createRetry; $a++) {
        try {
            $job = Invoke-RestMethod -Uri "$baseUrl/v1/videos" `
                    -Method Post -Headers $headers `
                    -Body ([System.Text.Encoding]::UTF8.GetBytes($body)) `
                    -TimeoutSec 60 -ErrorAction Stop
            break
        } catch {
            $errMsg = $_.Exception.Message
            try {
                $reader = New-Object System.IO.StreamReader($_.Exception.Response.GetResponseStream())
                $reader.BaseStream.Position = 0
                $errBody = $reader.ReadToEnd()
                if ($errBody) { $errMsg += " | response body: $errBody" }
            } catch {}
            if ($a -lt $createRetry) {
                Start-Sleep -Seconds ($a * 5)
            } else {
                $result.error = "create failed after ${createRetry} retries: $errMsg"
                $sw.Stop()
                $result.elapsed = "{0:F1}s" -f $sw.Elapsed.TotalSeconds
                $result.endTime = (Get-Date -Format 'yyyy-MM-dd HH:mm:ss')
                return [PSCustomObject]$result
            }
        }
    }

    $jobId = $job.id
    if (-not $jobId) {
        $result.error = "no job id in create response (body: $($job | ConvertTo-Json -Compress))"
        $sw.Stop()
        $result.elapsed = "{0:F1}s" -f $sw.Elapsed.TotalSeconds
        $result.endTime = (Get-Date -Format 'yyyy-MM-dd HH:mm:ss')
        return [PSCustomObject]$result
    }
    $result.jobId = $jobId

    # ---------- Step 2: Poll until completed / failed ----------
    $waited = 0
    $status = "queued"
    $final  = $null
    while ($waited -lt $maxWaitSec) {
        Start-Sleep -Seconds $pollInterval
        $waited += $pollInterval
        try {
            $final = Invoke-RestMethod -Uri "$baseUrl/v1/videos/$jobId" `
                      -Method Get -Headers $headers `
                      -TimeoutSec 30 -ErrorAction Stop
            $status = $final.status
            if ($status -eq "completed" -or $status -eq "failed") { break }
        } catch {
            # transient network error - retry next poll
        }
    }

    # ---------- Step 3: Handle final status ----------
    if ($status -eq "completed") {
        $videoUrl = $final.url
        if (-not $videoUrl -and $final.data) { $videoUrl = $final.data.url }
        $result.videoUrl = $videoUrl
        if ($videoUrl) {
            $filePath = Join-Path $siteDir ("video_{0:D3}.mp4" -f $videoIndex)
            try {
                Invoke-WebRequest -Uri $videoUrl -OutFile $filePath -TimeoutSec 300 -ErrorAction Stop
                $result.success = $true
                $result.file    = $filePath
            } catch {
                $result.error = "download failed: " + $_.Exception.Message
            }
        } else {
            $result.error = "no url in retrieve response"
        }
    } elseif ($status -eq "failed") {
        $errInfo = if ($final.error) { $final.error.message } else { "unknown" }
        $result.error = "job failed: $errInfo"
    } else {
        $result.error = "poll timeout after ${maxWaitSec}s, last status: $status"
    }

    $sw.Stop()
    $result.elapsed = "{0:F1}s" -f $sw.Elapsed.TotalSeconds
    $result.endTime = (Get-Date -Format 'yyyy-MM-dd HH:mm:ss')
    [PSCustomObject]$result
}

# ============================================================================
# Submit all jobs
# ============================================================================
$pipelines = @()
for ($s = 0; $s -lt $sites.Count; $s++) {
    $site = $sites[$s]
    $siteIndex = $s + 1
    $siteDir = Join-Path $saveDir ("site_{0:D2}_{1}" -f $siteIndex, ($site.url -replace 'http://', '' -replace '[:\/]', '_'))

    for ($v = 0; $v -lt $videosPerSite; $v++) {
        $prompt     = $prompts[$v % $prompts.Count]
        $videoIndex = $v + 1

        $ps = [PowerShell]::Create().AddScript($scriptBlock)
        $ps.AddArgument($site.apiKey)       | Out-Null
        $ps.AddArgument($site.url)          | Out-Null
        $ps.AddArgument($prompt)            | Out-Null
        $ps.AddArgument($siteIndex)         | Out-Null
        $ps.AddArgument($videoIndex)        | Out-Null
        $ps.AddArgument($siteDir)           | Out-Null
        $ps.AddArgument($site.url)          | Out-Null
        $ps.AddArgument($site.password)     | Out-Null
        $ps.AddArgument($refImage1DataUrl)  | Out-Null
        $ps.AddArgument($refImage2DataUrl)  | Out-Null
        $ps.AddArgument($pollInterval)      | Out-Null
        $ps.AddArgument($maxWaitSec)        | Out-Null
        $ps.RunspacePool = $runspacePool

        $pipelines += @{
            Pipeline   = $ps
            Handle     = $ps.BeginInvoke()
            SiteIndex  = $siteIndex
            VideoIndex = $videoIndex
        }
    }
}

Log "$($pipelines.Count) requests submitted, waiting for poll results..."
Log ""

# ============================================================================
# Collect results in real time
# ============================================================================
$completed = @()
while ($pipelines.Count -gt 0) {
    $done = $pipelines | Where-Object { $_.Handle.IsCompleted }
    foreach ($item in $done) {
        try {
            $r = $item.Pipeline.EndInvoke($item.Handle)
            if ($r) {
                foreach ($row in $r) {
                    $status = if ($row.success) { "SUCCESS" } else { "FAILED" }
                    $msg = ("[{0}] Site {1:D2} Video {2:D3} | elapsed: {3} | {4}" -f `
                            $status, $row.siteIndex, $row.videoIndex, $row.elapsed,
                            $row.prompt.Substring(0, [Math]::Min(50, $row.prompt.Length)))
                    if ($row.success) {
                        $msg += " | file: $($row.file)"
                    } else {
                        $msg += " | error: $($row.error)"
                    }
                    Log $msg
                    $completed += $row
                }
            }
        } catch {
            Log ("[ERROR] Site {0:D2} Video {1:D3} | {2}" -f $item.SiteIndex, $item.VideoIndex, $_.Exception.Message)
        }
        $item.Pipeline.Dispose()
    }
    $pipelines = @($pipelines | Where-Object { -not $_.Handle.IsCompleted })
    if ($pipelines.Count -gt 0) { Start-Sleep -Seconds 2 }
}

# ============================================================================
# Summary report
# ============================================================================
Log ""
Log "==================== Job Summary ===================="
$successCount = ($completed | Where-Object { $_.success }).Count
$failCount    = ($completed | Where-Object { -not $_.success }).Count
$globalTimer.Stop()
$totalMin = [Math]::Round($globalTimer.Elapsed.TotalMinutes, 1)
$avgSec   = if ($completed.Count -gt 0) {
                [Math]::Round($globalTimer.Elapsed.TotalSeconds / $completed.Count, 1)
            } else { 0 }
Log "Total: $($completed.Count) | Success: $successCount | Failed: $failCount"
Log "Total time: ${totalMin} min | Average: ${avgSec} s/job"
Log ""

for ($s = 0; $s -lt $sites.Count; $s++) {
    $siteIndex   = $s + 1
    $siteResults = $completed | Where-Object { $_.siteIndex -eq $siteIndex }
    $siteSuccess = ($siteResults | Where-Object { $_.success }).Count
    $siteFail    = ($siteResults | Where-Object { -not $_.success }).Count
    Log ("Site {0:D2} | {1} | Success: {2} Failed: {3}" -f $siteIndex, $sites[$s].url, $siteSuccess, $siteFail)
}

$completed | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $saveDir "results.json") -Encoding UTF8
Log ""
Log "JSON results saved: $(Join-Path $saveDir 'results.json')"
Log "Log saved: $logFile"

$runspacePool.Close()
$runspacePool.Dispose()
