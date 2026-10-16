param()
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$corpus = Join-Path $root 'testdata\migration'
$expected = [ordered]@{
  'orchestration\main-combined.alloy' = '65758781d889dd91c6ae2b330241411e55e2f2f0fe966c43f16871ca75d91f44'
  'smtp-in\main.alloy' = 'ee40347d23038514551263b00e7763446d720d92ad550e4a32f5d729c8a6ccd8'
  'imap\imap_parse.toml' = 'ebee96a564e0dd214fccc10c84826519c508791bd05a89d286e8901062fa4ceb'
  'webtop-webmail\webtop_processor.alloy' = '75f921c558463bf110fdb249322f2d4e59c8c398849b95280ff928bce441b7d8'
  'webtop-webmail\main.alloy' = 'cab586ec877a2f4c5037e86a998025349b87341f0f81573a3370632e3bfad4fb'
  'imap\imapserv_processor.alloy' = '1ac70be4cdb72b2a8c1c8f8d965cfa280945facc3805eae9f05b57ac6d12f99d'
  'smtp-in\smtp-in_processor.alloy' = 'c824e6d8bdcff359ec2714ebf3d7d3875aa9270442e20dbf896e1b3ca99cd8c6'
  'pop\pop_parse.toml' = '27ddbbb14601bca17a561c760155045929033fb2331f9cf0674296cf7c08f533'
  'mta\mta_processor.alloy' = 'f2940d4c893c6bf28c5ec058d82f0118546b258b94dd3a361121dc9fe1a3d8a0'
  'pop\popserv_processor.alloy' = '830aeb1e269ab6720e55af2d35c045085090188423f59208966e12a06f5870c8'
  'smtp-in\smtp-in_universal.alloy' = '9e8f5e07b75125b02df983bbb4c88973b38297aff71ba4a7690db0d8d90e3e27'
  'pabd-pss\pabd_processor.alloy' = '08f9c1ca5728a2a79f2d1aba6240684244e8b39e3062b1cd73c6d55e4c1b3bd6'
  'capd-mss\capd_processor.alloy' = '9f09660d64a9469c1aa102087d5d0e3133d23997e14ce665828f3a6fa8d7ac87'
  'orchestration\main-duplicate.alloy' = '65758781d889dd91c6ae2b330241411e55e2f2f0fe966c43f16871ca75d91f44'
  'smtp-out\smtp-out_processor.alloy' = '5daaf08766af0a6dc4327db79bdf689dd7df5a7fd9bdf35fb8233fa869b43332'
  'smtp-out\main.alloy' = 'e002a47d5c437a61c02c7bdfc06687bb07f27d59fee5af999524725a4beeed21'
}
if ($expected.Count -ne 16) { throw "Expected 16 corpus entries" }
foreach ($entry in $expected.GetEnumerator()) {
  $path = Join-Path $corpus $entry.Key
  if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "Missing $($entry.Key)" }
  $actual = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($actual -ne $entry.Value) { throw "SHA-256 mismatch for $($entry.Key): $actual" }
}
Write-Host 'Corpus accounting: 16/16 hashes verified.'
docker run --rm `
  -e GOCACHE=/tmp/gocache `
  -e GOTMPDIR=/tmp/gotmp `
  -v "${root}:/src:ro" `
  -w /src `
  golang:1.24.9-bookworm `
  sh -c 'mkdir -p /tmp/gocache /tmp/gotmp && /usr/local/go/bin/go test -count=1 ./internal/migration'
if ($LASTEXITCODE -ne 0) { throw "Pinned Docker corpus tests failed" }
