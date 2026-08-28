# Instalador de arnes para Windows.
#   irm https://raw.githubusercontent.com/MauricioJC3/arnes_ng/main/install.ps1 | iex
#
# Variables opcionales:
#   $env:ARNES_INSTALL_DIR   dónde poner el binario (default: %LOCALAPPDATA%\arnes\bin)
#   $env:ARNES_VERSION       tag a instalar (default: el último release)
$ErrorActionPreference = "Stop"

$repo = "MauricioJC3/arnes_ng"

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
  "AMD64" { "amd64" }
  "ARM64" { "arm64" }
  default { throw "arquitectura no soportada: $($env:PROCESSOR_ARCHITECTURE)" }
}
$asset = "arnes-windows-$arch.exe"

if ($env:ARNES_VERSION) {
  $url = "https://github.com/$repo/releases/download/$($env:ARNES_VERSION)/$asset"
} else {
  $url = "https://github.com/$repo/releases/latest/download/$asset"
}

$dir = if ($env:ARNES_INSTALL_DIR) { $env:ARNES_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "arnes\bin" }
New-Item -ItemType Directory -Force -Path $dir | Out-Null
$dest = Join-Path $dir "arnes.exe"

Write-Host "bajando $asset…"
Invoke-WebRequest -Uri $url -OutFile $dest

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$dir*") {
  [Environment]::SetEnvironmentVariable("Path", "$userPath;$dir", "User")
  Write-Host "agregado $dir al PATH del usuario — reiniciá la terminal"
}

Write-Host "instalado en $dest"
& $dest --version
