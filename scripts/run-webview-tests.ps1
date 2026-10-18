param([int]$Port = 12382, [switch]$SkipGoGate, [string]$ScratchRoot = $env:NLI_BUILD_SCRATCH)
$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$Scratch = if ($ScratchRoot) { Join-Path $ScratchRoot ("nli-webview-tests-$PID-" + [guid]::NewGuid().ToString('N')) } else { throw 'NLI_BUILD_SCRATCH or -ScratchRoot is required' }
$Name = "nli-webview-$PID"
$Network = "$Name-net"
New-Item -ItemType Directory -Force -Path $Scratch,(Join-Path $Scratch 'tmp'),(Join-Path $Scratch 'gocache'),(Join-Path $Scratch 'gomodcache'),(Join-Path $Scratch 'state'),(Join-Path $Scratch 'web') | Out-Null
foreach ($asset in @('index.html','styles.css','app.js','nli-mark.svg','README.txt')) { Copy-Item -LiteralPath (Join-Path $Root "web\$asset") -Destination (Join-Path $Scratch 'web') }
$AdminPassword = 'Browser-' + [guid]::NewGuid().ToString('N') + '!'
$Config = @{
  listen_address='0.0.0.0:12382'; state_dir='/scratch/state'; web_dir='/scratch/web';
  storage_provider='local'; storage_dir='/scratch/state/metadata'; event_path='/scratch/state/event-metadata.jsonl';
  event_retention=1000; assurance_retention_enabled=$true; assurance_retention_max=50; assurance_retention_hours=24;
  artifact_public_keys=@(); enable_legacy_event_ingest=$false; allow_insecure_remote=$true
} | ConvertTo-Json
[IO.File]::WriteAllText((Join-Path $Scratch 'controller.json'), $Config, [Text.UTF8Encoding]::new($false))
$Key = [Convert]::ToBase64String((1..32 | ForEach-Object { [byte]$_ }))
try {
  if(-not $SkipGoGate) {
    docker run --rm --mount "type=bind,src=$Root,dst=/src,readonly" --mount "type=bind,src=$Scratch,dst=/scratch" --mount "type=volume,src=nli-go-build-cache,dst=/root/.cache/go-build" --mount "type=volume,src=nli-go-mod-cache,dst=/go/pkg/mod" -e TMPDIR=/scratch/tmp golang:1.24.9 bash -c "cd /src && /usr/local/go/bin/go test -count=1 ./internal/auth ./internal/auditlog ./internal/middleware ./internal/store ./internal/eventstore ./internal/controlplane ./internal/webtest ./internal/webview"
    if($LASTEXITCODE -ne 0){throw "WebView Go gate failed with exit code $LASTEXITCODE"}
  }
  docker network create $Network | Out-Null
  if($LASTEXITCODE -ne 0){throw "Docker network creation failed with exit code $LASTEXITCODE"}
  docker run -d --name $Name --network $Network -p "127.0.0.1:${Port}:12382" --mount "type=bind,src=$Root,dst=/src,readonly" --mount "type=bind,src=$Scratch,dst=/scratch" --mount "type=volume,src=nli-go-build-cache,dst=/root/.cache/go-build" --mount "type=volume,src=nli-go-mod-cache,dst=/go/pkg/mod" -e TMPDIR=/scratch/tmp -e NLI_ENROLLMENT_TOKEN='Browser-enrollment-token-123456' -e NLI_BOOTSTRAP_ADMIN_PASSWORD=$AdminPassword -e NLI_AUDIT_HMAC_KEY=$Key -e NLI_CSRF_HMAC_KEY=$Key -e NLI_ASSURANCE_ENCRYPTION_KEY=$Key golang:1.24.9 bash -c "cd /src && exec /usr/local/go/bin/go run ./cmd/nli-control --config /scratch/controller.json" | Out-Null
  if($LASTEXITCODE -ne 0){throw "Controller container start failed with exit code $LASTEXITCODE"}
  $ready=$false; for($i=0;$i -lt 90;$i++){try{if((Invoke-WebRequest -UseBasicParsing -TimeoutSec 2 "http://127.0.0.1:$Port/api/v1/health").StatusCode -eq 200){$ready=$true;break}}catch{};Start-Sleep 1};if(-not $ready){throw 'Controller did not become ready'}
  docker run --rm --network $Network --mount "type=bind,src=$Root,dst=/src,readonly" --mount "type=bind,src=$Scratch,dst=/scratch" --mount "type=volume,src=nli-npm-cache,dst=/root/.npm" -e NLI_BASE_URL="http://${Name}:12382" -e NLI_ADMIN_PASSWORD=$AdminPassword -e NLI_E2E_OUTPUT=/scratch/e2e-results mcr.microsoft.com/playwright:v1.64.1-noble bash -c "rm -rf /scratch/playwright-work && mkdir -p /scratch/playwright-work && cp /src/web/package.json /src/web/playwright.config.js /src/web/e2e.spec.js /scratch/playwright-work/ && cd /scratch/playwright-work && npm install --ignore-scripts --no-audit --no-fund && npx playwright test"
  if($LASTEXITCODE -ne 0){throw "Playwright gate failed with exit code $LASTEXITCODE; evidence: $Scratch\e2e-results"}
} finally {
  docker rm -f $Name 2>$null | Out-Null
  docker network rm $Network 2>$null | Out-Null
}
