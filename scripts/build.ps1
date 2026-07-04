# Build script for RouterHub
# Builds frontend, copies to embed directory, then builds Go binary

$ErrorActionPreference = "Stop"
$rootDir = Split-Path -Parent $PSScriptRoot

Write-Host "=== Step 1: Building frontend ===" -ForegroundColor Cyan
Push-Location "$rootDir\web"
try {
    npm run build
    if ($LASTEXITCODE -ne 0) { throw "Frontend build failed" }
} finally {
    Pop-Location
}

Write-Host "=== Step 2: Copying dist to embed directory ===" -ForegroundColor Cyan
$embedDist = "$rootDir\internal\webui\dist"
# Recreate the directory to ensure a clean state
Remove-Item -Recurse -Force $embedDist -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $embedDist | Out-Null
Copy-Item -Recurse -Force "$rootDir\web\dist\*" -Destination $embedDist
# Ensure .gitkeep exists for version control
New-Item -ItemType File -Force -Path "$embedDist\.gitkeep" | Out-Null

Write-Host "=== Step 3: Building Go binary ===" -ForegroundColor Cyan
Push-Location $rootDir
try {
    go build -o routerhub.exe ./cmd/routerhub
    if ($LASTEXITCODE -ne 0) { throw "Go build failed" }
} finally {
    Pop-Location
}

Write-Host "=== Build complete: routerhub.exe ===" -ForegroundColor Green
