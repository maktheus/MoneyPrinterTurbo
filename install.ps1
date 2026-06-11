# nichebot installer — Windows (PowerShell)
# Usage:  irm https://raw.githubusercontent.com/maktheus/MoneyPrinterTurbo/main/install.ps1 | iex
#Requires -Version 5.1
Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$REPO        = "maktheus/MoneyPrinterTurbo"
$NichebotDir = Join-Path $env:USERPROFILE ".nichebot"
$BinDir      = Join-Path $NichebotDir "bin"
$ComposeUrl  = "https://raw.githubusercontent.com/$REPO/main/docker-compose.nichebot.yml"
$MptImage    = "ghcr.io/maktheus/nichebot-mpt:latest"
$CliImage    = "ghcr.io/maktheus/nichebot:latest"

function Write-Step  { Write-Host "[nichebot] $args" -ForegroundColor Cyan }
function Write-Ok    { Write-Host "✔  $args" -ForegroundColor Green }
function Write-Warn  { Write-Host "⚠  $args" -ForegroundColor Yellow }
function Write-Fail  { Write-Host "✖  $args" -ForegroundColor Red; exit 1 }

# ── Docker ───────────────────────────────────────────────────────────────────
function Ensure-Docker {
    try {
        $null = docker compose version 2>&1
        Write-Ok "Docker encontrado: $(docker --version)"
        return
    } catch {}

    Write-Step "Docker não encontrado. Verificando winget..."
    if (Get-Command winget -ErrorAction SilentlyContinue) {
        Write-Step "Instalando Docker Desktop via winget..."
        winget install -e --id Docker.DockerDesktop --accept-source-agreements --accept-package-agreements
        Write-Warn "Docker Desktop instalado. Inicie-o, aguarde finalizar e execute o instalador novamente."
        Write-Host "  https://docs.docker.com/desktop/windows/" -ForegroundColor Blue
        exit 0
    } else {
        Write-Warn "winget não disponível. Baixe o Docker Desktop manualmente:"
        Write-Host "  https://docs.docker.com/desktop/windows/" -ForegroundColor Blue
        Write-Fail "Instale o Docker Desktop e execute o instalador novamente."
    }
}

# ── Pull images ───────────────────────────────────────────────────────────────
function Pull-Images {
    Write-Step "Baixando imagens Docker (pode demorar na primeira vez)..."
    docker pull $MptImage
    docker pull $CliImage
    Write-Ok "Imagens atualizadas."
}

# ── Install compose file ──────────────────────────────────────────────────────
function Install-Compose {
    New-Item -ItemType Directory -Force -Path $NichebotDir | Out-Null
    Write-Step "Instalando em $NichebotDir ..."
    Invoke-WebRequest -Uri $ComposeUrl -OutFile (Join-Path $NichebotDir "docker-compose.yml") -UseBasicParsing
    Write-Ok "docker-compose.yml instalado."
}

# ── Write wrapper script ──────────────────────────────────────────────────────
function Install-Wrapper {
    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null

    $wrapperContent = @"
# nichebot wrapper — gerado pelo instalador
`$NichebotDir = Join-Path `$env:USERPROFILE ".nichebot"
`$ComposeFile = Join-Path `$NichebotDir "docker-compose.yml"
`$CliImage    = "ghcr.io/maktheus/nichebot:latest"

function Start-Mpt {
    docker compose -f `$ComposeFile up -d mpt
    `$timeout = 120; `$elapsed = 0
    Write-Host "⏳ Aguardando MoneyPrinterTurbo iniciar..." -NoNewline
    while (`$elapsed -lt `$timeout) {
        `$health = (docker compose -f `$ComposeFile ps mpt --format '{{.Health}}' 2>`$null)
        if (`$health -eq "healthy") { Write-Host " pronto!"; return }
        Start-Sleep 5; `$elapsed += 5; Write-Host "." -NoNewline
    }
    Write-Host ""
    Write-Warning "MPT demorou para ficar pronto. Verifique: nichebot logs"
}

switch (`$args[0]) {
    "update" {
        Write-Host "🔄 Atualizando nichebot..."
        docker compose -f `$ComposeFile pull
        docker compose -f `$ComposeFile up -d --remove-orphans mpt
        docker pull `$CliImage
        Write-Host "✅ Atualizado!"
    }
    "stop"   { docker compose -f `$ComposeFile down; Write-Host "⏹  Parado." }
    "logs"   { docker compose -f `$ComposeFile logs -f mpt }
    "status" { docker compose -f `$ComposeFile ps }
    "uninstall" {
        `$confirm = Read-Host "Desinstalar nichebot e apagar todos os dados? [s/N]"
        if (`$confirm -ne "s") { exit 0 }
        docker compose -f `$ComposeFile down -v
        Remove-Item -Recurse -Force `$NichebotDir
        Write-Host "🗑  nichebot desinstalado."
    }
    default {
        Start-Mpt
        docker run -it --rm ``
            --network nichebot ``
            -v nichebot_data:/data ``
            -e NICHEBOT_MPT_URL=http://mpt:8080 ``
            `$CliImage
    }
}
"@

    $wrapperPath = Join-Path $BinDir "nichebot.ps1"
    Set-Content -Path $wrapperPath -Value $wrapperContent -Encoding UTF8

    # Create a .cmd shim so "nichebot" works without the .ps1 extension
    $shimContent = "@echo off`r`npowershell -NoProfile -ExecutionPolicy Bypass -File `"%~dp0nichebot.ps1`" %*"
    Set-Content -Path (Join-Path $BinDir "nichebot.cmd") -Value $shimContent -Encoding ASCII

    Write-Ok "Wrapper instalado em $BinDir."
}

# ── PATH ──────────────────────────────────────────────────────────────────────
function Ensure-Path {
    $userPath = [Environment]::GetEnvironmentVariable("PATH", "User")
    if ($userPath -notlike "*$BinDir*") {
        [Environment]::SetEnvironmentVariable("PATH", "$userPath;$BinDir", "User")
        $env:PATH += ";$BinDir"
        Write-Ok "Adicionado ao PATH do usuário."
    }
}

# ── Main ──────────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "  nichebot — instalador" -ForegroundColor Cyan -BackgroundColor DarkBlue
Write-Host "  ─────────────────────────────────"
Write-Host ""

Ensure-Docker
Install-Compose
Pull-Images
Install-Wrapper
Ensure-Path

Write-Host ""
Write-Host "✅ Instalação concluída!" -ForegroundColor Green
Write-Host ""
Write-Host "  Execute:  nichebot"
Write-Host "  Ajuda:    nichebot help"
Write-Host ""
