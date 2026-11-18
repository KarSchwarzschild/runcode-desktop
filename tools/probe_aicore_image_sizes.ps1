param(
    [string]$BaseUrl = "http://123.249.28.107:15001",
    [string]$TenantId = "wjtest",
    [string]$InternalServiceKey = $env:AICORE_INTERNAL_SERVICE_KEY,
    [string]$OutputPath = "C:\Users\yyzxc\Desktop\生图模型列表\尺寸测试-20261116.jsonl",
    [switch]$ExtraOnly
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($InternalServiceKey)) {
    throw "Set AICORE_INTERNAL_SERVICE_KEY before running this probe."
}

Add-Type -AssemblyName System.Drawing
$auth = 'Bearer {"TenantId":"' + $TenantId + '","DependentType":"User","DependentId":"codex-size-probe","DependentName":"codex-size-probe"}'
$work = Join-Path ([IO.Path]::GetTempPath()) ("aicore-image-probe-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $work | Out-Null

$cases = @(
    @{ model = "doubao-seedream-4-0-250828"; provider = "volcengine"; size = "1280x720"; source = "Core description lower bound" },
    @{ model = "doubao-seedream-4-0-250828"; provider = "volcengine"; size = "2048x2048"; source = "existing verified combination" },
    @{ model = "doubao-seedream-4-0-250828"; provider = "volcengine"; size = "4096x4096"; source = "Core description upper bound" },
    @{ model = "doubao-seedream-4-5-251128"; provider = "volcengine"; size = "2560x1440"; source = "Core description lower bound" },
    @{ model = "doubao-seedream-4-5-251128"; provider = "volcengine"; size = "4096x4096"; source = "Core description upper bound" },
    @{ model = "doubao-seedream-5-0-260128"; provider = "volcengine"; size = "2560x1440"; source = "Core description lower bound" },
    @{ model = "doubao-seedream-5-0-260128"; provider = "volcengine"; size = "3072x3072"; source = "Core description upper bound" },
    @{ model = "qwen-image-3.0-pro"; provider = "bailian"; size = "1024*1024"; source = "DashScope canonical candidate" },
    @{ model = "qwen-image-max"; provider = "bailian"; size = "1024*1024"; source = "DashScope canonical candidate" },
    @{ model = "qwen-image-plus"; provider = "bailian"; size = "1024*1024"; source = "DashScope canonical candidate" },
    @{ model = "wan2.6-t2i"; provider = "bailian"; size = "1024*1024"; source = "DashScope canonical candidate" },
    @{ model = "wan2.7-image"; provider = "bailian"; size = "768*768"; source = "Core description lower bound" },
    @{ model = "wan2.7-image"; provider = "bailian"; size = "2048*2048"; source = "Core description upper bound" },
    @{ model = "wanx-v1"; provider = "dashscope"; size = "1024*1024"; source = "local SDK enum and historical test" },
    @{ model = "wanx-v1"; provider = "dashscope"; size = "1280*720"; source = "local SDK enum and historical test" },
    @{ model = "wanx-v1"; provider = "dashscope"; size = "720*1280"; source = "local SDK enum and historical test" },
    @{ model = "wanx-v1"; provider = "dashscope"; size = "768*1152"; source = "historical provider report" },
    @{ model = "hunyuan-image-latest"; provider = "hunyuan"; size = "1024x1024"; source = "canonical Tencent candidate" },
    @{ model = "hunyuan-image-lite"; provider = "hunyuan"; size = "1024x1024"; source = "historical test" }
)

if ($ExtraOnly) {
    $cases = @(
        @{ model = "qwen-image-3.0-pro"; provider = "bailian"; size = "1536*1024"; source = "additional common DashScope candidate" },
        @{ model = "qwen-image-3.0-pro"; provider = "bailian"; size = "1024*1536"; source = "additional common DashScope candidate" },
        @{ model = "wan2.6-t2i"; provider = "bailian"; size = "1280*720"; source = "additional common DashScope candidate" },
        @{ model = "wan2.6-t2i"; provider = "bailian"; size = "720*1280"; source = "additional common DashScope candidate" },
        @{ model = "wan2.7-image"; provider = "bailian"; size = "768*2048"; source = "Core ratio boundary candidate" },
        @{ model = "wan2.7-image"; provider = "bailian"; size = "2048*768"; source = "Core ratio boundary candidate" }
    )
}

function Invoke-ImageCase($case) {
    $started = [DateTime]::UtcNow
    $body = @{ model = $case.model; prompt = "一张简单的几何色块测试图"; n = 1; size = $case.size; response_format = "url" } | ConvertTo-Json -Compress
    $bodyPath = Join-Path $work (([guid]::NewGuid().ToString("N")) + ".json")
    $responsePath = Join-Path $work (([guid]::NewGuid().ToString("N")) + ".response")
    Set-Content -LiteralPath $bodyPath -Value $body -Encoding UTF8 -NoNewline
    $status = & curl.exe --max-time 120 --silent --show-error --output $responsePath --write-out "%{http_code}" `
        -H ("X-Internal-Service-Key: " + $InternalServiceKey) `
        -H ("Authorization: " + $auth) `
        -H "Content-Type: application/json" --data-binary ("@" + $bodyPath) `
        ($BaseUrl.TrimEnd("/") + "/v1/Images/Generations") 2>$null
    $statusCode = [int]$status
    $raw = if (Test-Path $responsePath) { Get-Content -Raw -LiteralPath $responsePath } else { "" }
    $record = [ordered]@{
        tested_at = [DateTime]::UtcNow.ToString("o")
        model = $case.model
        provider = $case.provider
        requested_size = $case.size
        candidate_source = $case.source
        http_status = $statusCode
        outcome = "unknown"
        returned_size = $null
        error_category = $null
        error_message = $null
        elapsed_ms = [int](([DateTime]::UtcNow - $started).TotalMilliseconds)
    }
    try {
        $json = $raw | ConvertFrom-Json
        if ($statusCode -eq 200 -and $json.data -and $json.data[0].url) {
            $imgPath = Join-Path $work (([guid]::NewGuid().ToString("N")) + ".img")
            try {
                & curl.exe --max-time 120 --silent --show-error --output $imgPath $json.data[0].url 2>$null
                $img = [Drawing.Image]::FromFile($imgPath)
                $record.outcome = "success"
                $record.returned_size = ("{0}x{1}" -f $img.Width, $img.Height)
                $img.Dispose()
                Remove-Item -LiteralPath $imgPath -Force -ErrorAction SilentlyContinue
            } catch {
                $record.outcome = "generated_download_failed"
                $record.error_category = "result_download"
                $record.error_message = $_.Exception.Message.Substring(0, [Math]::Min(240, $_.Exception.Message.Length))
            }
        } else {
            $record.outcome = "rejected"
            $record.error_category = if ($statusCode -eq 401 -or $statusCode -eq 403) { "auth_or_tenant" } elseif ($statusCode -ge 500) { "core_or_upstream" } else { "provider_or_request" }
            $message = if ($json.message) { [string]$json.message } elseif ($json.error -and $json.error.message) { [string]$json.error.message } else { $raw }
            $record.error_message = $message.Substring(0, [Math]::Min(400, $message.Length))
        }
    } catch {
        $record.outcome = if ($raw -match "Model not exist") { "rejected" } else { "transport_or_invalid_response" }
        $record.error_category = if ($raw -match "Model not exist") { "provider_or_request" } else { "transport" }
        $record.error_message = if ($raw) { $raw.Substring(0, [Math]::Min(400, $raw.Length)) } else { "no response body" }
    }
    ($record | ConvertTo-Json -Compress) | Add-Content -LiteralPath $OutputPath -Encoding UTF8
    $record | ConvertTo-Json -Compress
}

New-Item -ItemType File -Path $OutputPath -Force | Out-Null
foreach ($case in $cases) {
    Invoke-ImageCase $case
}
Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
