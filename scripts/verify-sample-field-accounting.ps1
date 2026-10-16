[CmdletBinding()]
param(
    [string]$ReportPath = ""
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($ReportPath)) {
    $ReportPath = Join-Path $repoRoot "build/sample-field-accounting.json"
} elseif (-not [System.IO.Path]::IsPathRooted($ReportPath)) {
    $ReportPath = Join-Path $repoRoot $ReportPath
}

$env:NLI_FIELD_ACCOUNTING_REPORT = [System.IO.Path]::GetFullPath($ReportPath)
$testExitCode = 1
try {
    Push-Location $repoRoot
    try {
        $images = @{}
        Get-Content (Join-Path $repoRoot 'packaging/docker/images.env') | ForEach-Object {
            if ($_ -match '^([^#=]+)=(.+)$') { $images[$Matches[1]] = $Matches[2] }
        }
        if (-not $images.GO_IMAGE) { throw 'GO_IMAGE is missing from packaging/docker/images.env' }
        $reportDirectory = Split-Path -Parent $env:NLI_FIELD_ACCOUNTING_REPORT
        New-Item -ItemType Directory -Force -Path $reportDirectory | Out-Null
        $dockerReport = "/nli-report/$([System.IO.Path]::GetFileName($env:NLI_FIELD_ACCOUNTING_REPORT))"
        docker run --rm `
            --mount "type=bind,source=$repoRoot,target=/workspace,readonly" `
            --mount "type=bind,source=$reportDirectory,target=/nli-report" `
            --mount 'type=volume,source=nli-go-build-cache,target=/root/.cache/go-build' `
            --mount 'type=volume,source=nli-go-mod-cache,target=/go/pkg/mod' `
            --workdir /workspace `
            --env "NLI_FIELD_ACCOUNTING_REPORT=$dockerReport" `
            $images.GO_IMAGE `
            bash -lc "export PATH=/usr/local/go/bin:/usr/bin:/bin; go test ./internal/migration -run '^TestSample(LogCorpusFieldAccounting|FieldAccountingDuplicateSemantics)$' -count=1 -v"
        $testExitCode = $LASTEXITCODE
    } finally {
        Pop-Location
    }
} finally {
    Remove-Item Env:NLI_FIELD_ACCOUNTING_REPORT -ErrorAction SilentlyContinue
}

Write-Output "Field-accounting report: $([System.IO.Path]::GetFullPath($ReportPath))"
exit $testExitCode
