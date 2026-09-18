param([string]$Version = 'v0.1.0')
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path $PSScriptRoot -Parent
Push-Location $repoRoot
$oldGOOS = $env:GOOS
$oldGOARCH = $env:GOARCH
$oldCGO = $env:CGO_ENABLED
try {
    $dist = Join-Path $repoRoot 'dist'
    New-Item -ItemType Directory -Force $dist | Out-Null
    $moduleInfo = (& go list -m -f '{{.Dir}}' all)
    if ($LASTEXITCODE -ne 0) { throw 'Cannot enumerate dependency licenses' }
    $notices = @('Third-party components bundled in the One-KVM CLI', '')
    $goroot = (& go env GOROOT)
    foreach ($moduleDir in @($goroot) + @($moduleInfo | Select-Object -Skip 1)) {
        if (!$moduleDir) { continue }
        $licenses = Get-ChildItem -LiteralPath $moduleDir -File | Where-Object { $_.Name -match '^(LICENSE|COPYING)(\..*)?$' }
        foreach ($license in $licenses) {
            $notices += "=== $(Split-Path $moduleDir -Leaf) / $($license.Name) ==="
            $notices += Get-Content -LiteralPath $license.FullName -Raw
        }
    }
    $archives = @()
    foreach ($platform in @(@('windows','amd64'), @('linux','amd64'), @('linux','arm64'))) {
        $env:GOOS = $platform[0]; $env:GOARCH = $platform[1]; $env:CGO_ENABLED = '0'
        $label = "$($platform[0])-$($platform[1])"
        $stage = Join-Path $dist "$Version-$label"
        $skill = Join-Path $stage 'onekvm'
        New-Item -ItemType Directory -Force (Join-Path $skill 'scripts') | Out-Null
        Copy-Item 'skill/onekvm/*' $skill -Recurse -Force
        $executable = if ($env:GOOS -eq 'windows') { 'onekvm.exe' } else { 'onekvm' }
        & go build -trimpath -ldflags "-s -w -X github.com/LxFee/onekvm-cli/internal/cli.Version=$Version" -o (Join-Path $skill "scripts/$executable") ./cmd/onekvm
        if ($LASTEXITCODE -ne 0) { throw "Build failed: $label" }
        $notices -join "`n" | Set-Content (Join-Path $skill 'THIRD_PARTY_NOTICES.txt') -Encoding utf8
        if ($env:GOOS -eq 'windows') {
            $archive = Join-Path $dist "onekvm-skill-$Version-$label.zip"
            Compress-Archive -Path $skill -DestinationPath $archive -Force
        } else {
            $archive = Join-Path $dist "onekvm-skill-$Version-$label.tar.gz"

            $env:GOOS = $oldGOOS; $env:GOARCH = $oldGOARCH
            & go run ./scripts/package $skill $archive
            if ($LASTEXITCODE -ne 0) { throw "Archive failed: $label" }
        }
        $archives += $archive
    }
    $archives | ForEach-Object { $hash = Get-FileHash $_ -Algorithm SHA256; "$($hash.Hash.ToLower())  $(Split-Path $_ -Leaf)" } | Set-Content (Join-Path $dist 'SHA256SUMS') -Encoding ascii
    Write-Output "Built $Version skill packages in $dist"
} finally {
    $env:GOOS = $oldGOOS; $env:GOARCH = $oldGOARCH; $env:CGO_ENABLED = $oldCGO
    Pop-Location
}
