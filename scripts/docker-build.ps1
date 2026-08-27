# Build linux/amd64 and/or linux/arm64 images (Docker Desktop + buildx).
#
#   .\scripts\docker-build.ps1
#   .\scripts\docker-build.ps1 amd64
#   .\scripts\docker-build.ps1 arm64
#   $env:IMAGE="ghcr.io/you/ipv6-proxy:latest"; $env:PUSH="1"; .\scripts\docker-build.ps1 both
param(
    [ValidateSet("local", "amd64", "x86", "arm64", "arm", "both", "multi")]
    [string]$Arch = "local"
)

$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")

$Image = if ($env:IMAGE) { $env:IMAGE } else { "ipv6-proxy:local" }
$Builder = if ($env:BUILDER) { $env:BUILDER } else { "ipv6-proxy-builder" }
$engineArch = (docker version --format "{{.Server.Arch}}" 2>$null)
if (-not $engineArch) { $engineArch = "amd64" }

$inspect = docker buildx inspect $Builder 2>$null
if ($LASTEXITCODE -ne 0) {
    docker buildx create --name $Builder --driver docker-container --use | Out-Null
    docker buildx inspect --bootstrap | Out-Null
} else {
    docker buildx use $Builder | Out-Null
}

switch ($Arch) {
    "local" {
        if ($engineArch -eq "arm64") {
            docker buildx bake --set "image.tags=$Image" local-arm64
        } else {
            docker buildx bake --set "image.tags=$Image" local-amd64
        }
    }
    { $_ -in "amd64", "x86" } {
        docker buildx bake --set "image.tags=$Image" local-amd64
    }
    { $_ -in "arm64", "arm" } {
        docker buildx bake --set "image.tags=$Image" local-arm64
    }
    { $_ -in "both", "multi" } {
        if ($env:PUSH -ne "1") {
            throw "multi-arch images cannot --load into docker; set IMAGE=registry/name:tag and PUSH=1"
        }
        docker buildx bake --set "image.tags=$Image" --push image
    }
}

Write-Host "built $Image"
