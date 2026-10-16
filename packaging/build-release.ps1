param(
    [string]$OutputDirectory = "",
    [string]$ScratchRoot = $env:NLI_BUILD_SCRATCH
)
$ErrorActionPreference = 'Stop'
$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path

function Read-EnvFile([string]$Path) {
    $result = @{}
    Get-Content $Path | ForEach-Object {
        $line = $_.Trim()
        if ($line -and -not $line.StartsWith('#')) {
            $parts = $line.Split('=', 2)
            $result[$parts[0]] = $parts[1]
        }
    }
    return $result
}
function Invoke-Docker([string[]]$Arguments) {
    & docker @Arguments
    if ($LASTEXITCODE -ne 0) { throw "Docker command failed with exit code $LASTEXITCODE" }
}
function Get-Sha256([string]$Path) {
    $sha256 = [System.Security.Cryptography.SHA256]::Create()
    try {
        $stream = [System.IO.File]::OpenRead($Path)
        try {
            return ([System.BitConverter]::ToString($sha256.ComputeHash($stream))).Replace('-', '').ToLowerInvariant()
        } finally {
            $stream.Dispose()
        }
    } finally {
        $sha256.Dispose()
    }
}

$release = Read-EnvFile (Join-Path $Root 'packaging\release.env')
$images = Read-EnvFile (Join-Path $Root 'packaging\docker\images.env')
if (-not $ScratchRoot) { throw 'NLI_BUILD_SCRATCH or -ScratchRoot is required' }
$gitSafe = "safe.directory=$($Root -replace '\\','/')"
if ((git -c $gitSafe -C $Root status --porcelain)) { throw 'Release source must be a frozen, clean revision' }
$revision = (git -c $gitSafe -C $Root rev-parse HEAD).Trim()
$epoch = (git -c $gitSafe -C $Root show -s --format=%ct HEAD).Trim()
if (-not $OutputDirectory) { $OutputDirectory = Join-Path $ScratchRoot "nli-release-$revision" }
New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null
if (Get-ChildItem -Force $OutputDirectory) { throw "Output directory is not empty: $OutputDirectory" }

$scratch = Join-Path $ScratchRoot ("nli-release-build-" + [guid]::NewGuid().ToString('N'))
$paths = @('go-cache','go-mod','pass1','pass1-rpm','pass1-work','pass2','pass2-rpm','pass2-work')
foreach ($name in $paths) { New-Item -ItemType Directory -Path (Join-Path $scratch $name) -Force | Out-Null }

Invoke-Docker @('run','--rm','--mount',"type=bind,src=$Root,dst=/src,readonly",'--workdir','/src',$images.GO_IMAGE,'bash','-lc','export PATH=/usr/local/go/bin:/usr/bin:/bin; git config --global --add safe.directory /src; bash scripts/release/preflight.sh')

function Build-Binaries([string]$Destination) {
    Invoke-Docker @(
        'run','--rm',
        '--mount',"type=bind,src=$Root,dst=/src,readonly",
        '--mount',"type=bind,src=$Destination,dst=/out",
        '--mount',"type=bind,src=$(Join-Path $scratch 'go-cache'),dst=/cache/go-build",
        '--mount',"type=bind,src=$(Join-Path $scratch 'go-mod'),dst=/cache/go-mod",
        '--workdir','/src',
        '-e','ROOT=/src','-e','OUTPUT_DIR=/out','-e',"SOURCE_REVISION=$revision",'-e',"SOURCE_DATE_EPOCH=$epoch",
        '-e','GOCACHE=/cache/go-build','-e','GOMODCACHE=/cache/go-mod',
        $images.GO_IMAGE,'bash','-lc','export PATH=/usr/local/go/bin:/usr/bin:/bin; bash packaging/build-binaries.sh'
    )
}
$packageInstall = 'bash /src/packaging/docker/install-rpm-tools.sh >/dev/null; bash packaging/build-rpms.sh'
function Build-Rpms([string]$Binaries, [string]$Destination, [string]$Work) {
    Invoke-Docker @(
        'run','--rm',
        '--mount',"type=bind,src=$Root,dst=/src,readonly",
        '--mount',"type=bind,src=$Binaries,dst=/binaries,readonly",
        '--mount',"type=bind,src=$Destination,dst=/out",
        '--mount',"type=bind,src=$Work,dst=/work",
        '--workdir','/src','-e','ROOT=/src','-e','BIN_ROOT=/binaries','-e','OUTPUT_DIR=/out','-e','WORK_DIR=/work',
        '-e',"SOURCE_REVISION=$revision",'-e',"SOURCE_DATE_EPOCH=$epoch",
        $images.RPM_BUILD_IMAGE,'bash','-lc',$packageInstall
    )
}

$pass1 = Join-Path $scratch 'pass1'; $pass2 = Join-Path $scratch 'pass2'
$pass1Rpm = Join-Path $scratch 'pass1-rpm'; $pass2Rpm = Join-Path $scratch 'pass2-rpm'
Build-Binaries $pass1
Build-Rpms $pass1 $pass1Rpm (Join-Path $scratch 'pass1-work')
Build-Binaries $pass2
Build-Rpms $pass2 $pass2Rpm (Join-Path $scratch 'pass2-work')
foreach ($name in @('nli_client_ver13.rpm','nli_clinet_ver13.rpm','nli_controller_agent_ver13.rpm','webview_ver13.zip','webapp_ver13.zip','install-controller-ver13.sh','install-client-ver13.sh','install-controller-aws-ver13.sh','install-client-aws-ver13.sh','aws-rhel-preflight-ver13.sh','aws-security-group-plan-ver13.sh','uninstall-controller-ver13.sh','uninstall-client-ver13.sh','INSTALLATION_AND_CONFIGURATION.md','V13_AWS_RED_HAT_INSTALLATION.md','WEBAPP_OPERATOR_AND_PARSER_GUIDE.md','PARSER_CATALOG.md','SOURCE_BINARY_PROVENANCE.txt','SOURCE_REVISION','WEBVIEW_SHA256SUMS','RELEASE_ID')) {
    $a = Get-Sha256 (Join-Path $pass1Rpm $name)
    $b = Get-Sha256 (Join-Path $pass2Rpm $name)
    if ($a -ne $b) { throw "Non-deterministic output: $name" }
}
Copy-Item -Path (Join-Path $pass1Rpm '*') -Destination $OutputDirectory -Recurse

$benchmarkCommand = 'export PATH=/usr/local/go/bin:/usr/bin:/bin GOCACHE=/tmp/gocache GOTMPDIR=/tmp/gotmp; mkdir -p "$GOCACHE" "$GOTMPDIR"; { go test ./internal/benchmarks -run="^$" -bench=. -benchmem -benchtime=250ms -count=5; go test ./parser-policy ./internal/mixedkv -run="^$" -bench=. -benchmem -benchtime=500ms -count=5; } > /artifacts/BENCHMARKS.txt'
Invoke-Docker @('run','--rm','--mount',"type=bind,src=$Root,dst=/src,readonly",'--mount',"type=bind,src=$OutputDirectory,dst=/artifacts",'--workdir','/src',$images.GO_IMAGE,'bash','-lc',$benchmarkCommand)

$fieldAccountingCommand = 'export PATH=/usr/local/go/bin:/usr/bin:/bin GOCACHE=/tmp/gocache GOTMPDIR=/tmp/gotmp NLI_FIELD_ACCOUNTING_REPORT=/artifacts/SAMPLE_FIELD_ACCOUNTING.json; mkdir -p "$GOCACHE" "$GOTMPDIR"; go test ./internal/migration -count=1'
Invoke-Docker @('run','--rm','--mount',"type=bind,src=$Root,dst=/src,readonly",'--mount',"type=bind,src=$OutputDirectory,dst=/artifacts",'--workdir','/src',$images.GO_IMAGE,'bash','-lc',$fieldAccountingCommand)

Invoke-Docker @('run','--rm','--mount',"type=bind,src=$Root,dst=/src,readonly",'--mount',"type=bind,src=$OutputDirectory,dst=/artifacts",$images.EVIDENCE_IMAGE,'python','/src/scripts/release/generate-evidence.py','--artifacts','/artifacts','--version',$release.NLI_VERSION,'--release',$release.NLI_RELEASE,'--revision',$revision,'--source-date-epoch',$epoch,'--images','/src/packaging/docker/images.env')
$verify = 'bash /src/packaging/docker/install-rpm-tools.sh >/dev/null; ROOT=/src bash /src/scripts/release/create-bundle.sh /artifacts; ROOT=/src bash /src/scripts/release/verify-artifacts.sh /artifacts'
Invoke-Docker @('run','--rm','--mount',"type=bind,src=$Root,dst=/src,readonly",'--mount',"type=bind,src=$OutputDirectory,dst=/artifacts",$images.RPM_BUILD_IMAGE,'bash','-lc',$verify)

Write-Host "Reproducible unsigned release: PASS"
Write-Host "Artifacts: $OutputDirectory"
Write-Host "Scratch: $scratch"
