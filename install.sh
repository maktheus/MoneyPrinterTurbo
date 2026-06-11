#!/usr/bin/env bash
# nichebot installer — Linux & macOS
# Usage:  curl -fsSL https://raw.githubusercontent.com/maktheus/MoneyPrinterTurbo/main/install.sh | bash
set -euo pipefail

REPO="maktheus/MoneyPrinterTurbo"
NICHEBOT_DIR="${NICHEBOT_HOME:-$HOME/.nichebot}"
BIN_DIR="$HOME/.local/bin"
COMPOSE_URL="https://raw.githubusercontent.com/$REPO/main/docker-compose.nichebot.yml"
MPT_IMAGE="ghcr.io/maktheus/nichebot-mpt:latest"
CLI_IMAGE="ghcr.io/maktheus/nichebot:latest"

# ── colours ──────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BOLD='\033[1m'; NC='\033[0m'
info()    { echo -e "${BOLD}[nichebot]${NC} $*"; }
success() { echo -e "${GREEN}✔${NC}  $*"; }
warn()    { echo -e "${YELLOW}⚠${NC}  $*"; }
die()     { echo -e "${RED}✖${NC}  $*" >&2; exit 1; }

# ── Docker ───────────────────────────────────────────────────────────────────
ensure_docker() {
  if command -v docker &>/dev/null && docker compose version &>/dev/null; then
    success "Docker encontrado: $(docker --version)"
    return
  fi

  info "Docker não encontrado. Instalando..."
  OS="$(uname -s)"
  case "$OS" in
    Linux)
      curl -fsSL https://get.docker.com | sh
      # Add current user to docker group so we don't need sudo
      if getent group docker &>/dev/null; then
        sudo usermod -aG docker "$USER" 2>/dev/null || true
        warn "Adicionado ao grupo 'docker'. Pode ser necessário fazer logout/login para ativar sem sudo."
      fi
      ;;
    Darwin)
      die "No macOS instale o Docker Desktop manualmente: https://docs.docker.com/desktop/mac/ e depois execute o instalador novamente."
      ;;
    *)
      die "SO não suportado: $OS. Instale o Docker manualmente e execute o instalador novamente."
      ;;
  esac
  success "Docker instalado."
}

# ── Pull images ───────────────────────────────────────────────────────────────
pull_images() {
  info "Baixando imagens Docker (pode demorar na primeira vez)..."
  docker pull "$MPT_IMAGE"
  docker pull "$CLI_IMAGE"
  success "Imagens atualizadas."
}

# ── Install compose file ──────────────────────────────────────────────────────
install_compose() {
  mkdir -p "$NICHEBOT_DIR"
  info "Instalando em $NICHEBOT_DIR ..."
  curl -fsSL "$COMPOSE_URL" -o "$NICHEBOT_DIR/docker-compose.yml"
  success "docker-compose.yml instalado."
}

# ── Write wrapper script ──────────────────────────────────────────────────────
install_wrapper() {
  mkdir -p "$BIN_DIR"
  cat > "$BIN_DIR/nichebot" <<'WRAPPER'
#!/usr/bin/env bash
set -euo pipefail

NICHEBOT_DIR="${NICHEBOT_HOME:-$HOME/.nichebot}"
COMPOSE="docker compose -f $NICHEBOT_DIR/docker-compose.yml"
CLI_IMAGE="ghcr.io/maktheus/nichebot:latest"

_wait_healthy() {
  local timeout=120 elapsed=0
  echo "⏳ Aguardando MoneyPrinterTurbo iniciar..."
  while [ $elapsed -lt $timeout ]; do
    status=$($COMPOSE ps mpt --format '{{.Health}}' 2>/dev/null || echo "")
    [ "$status" = "healthy" ] && return 0
    sleep 5; elapsed=$((elapsed+5))
    printf "."
  done
  echo ""
  echo "⚠  MPT demorou para ficar pronto. Verifique com: nichebot logs"
}

case "${1:-}" in
  update)
    echo "🔄 Atualizando nichebot..."
    $COMPOSE pull
    $COMPOSE up -d --remove-orphans mpt
    docker pull "$CLI_IMAGE"
    echo "✅ Atualizado!"
    ;;
  stop)
    $COMPOSE down
    echo "⏹  Serviços parados."
    ;;
  logs)
    $COMPOSE logs -f mpt
    ;;
  status)
    $COMPOSE ps
    ;;
  uninstall)
    read -rp "Desinstalar nichebot e apagar todos os dados? [s/N] " confirm
    [ "${confirm,,}" = "s" ] || exit 0
    $COMPOSE down -v
    rm -rf "$NICHEBOT_DIR"
    rm -f "$HOME/.local/bin/nichebot"
    echo "🗑  nichebot desinstalado."
    ;;
  help|--help|-h)
    echo "Uso: nichebot [comando]"
    echo "  (sem args)   Inicia a TUI"
    echo "  update       Atualiza para a versão mais recente"
    echo "  stop         Para os serviços em background"
    echo "  logs         Mostra logs do MoneyPrinterTurbo"
    echo "  status       Status dos containers"
    echo "  uninstall    Remove tudo"
    ;;
  *)
    $COMPOSE up -d mpt
    _wait_healthy
    echo ""
    docker run -it --rm \
      --network nichebot \
      -v nichebot_data:/data \
      -e NICHEBOT_MPT_URL=http://mpt:8080 \
      "$CLI_IMAGE"
    ;;
esac
WRAPPER

  chmod +x "$BIN_DIR/nichebot"
  success "Wrapper instalado em $BIN_DIR/nichebot."
}

# ── PATH check ────────────────────────────────────────────────────────────────
ensure_path() {
  if echo "$PATH" | grep -q "$BIN_DIR"; then
    return
  fi
  warn "$BIN_DIR não está no PATH."
  SHELL_RC="$HOME/.bashrc"
  [ -f "$HOME/.zshrc" ] && SHELL_RC="$HOME/.zshrc"
  echo "export PATH=\"\$HOME/.local/bin:\$PATH\"" >> "$SHELL_RC"
  warn "Adicionado ao $SHELL_RC. Execute: source $SHELL_RC  ou abra um novo terminal."
}

# ── Main ──────────────────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}  nichebot — instalador${NC}"
echo "  ─────────────────────────────────"
echo ""

ensure_docker
install_compose
pull_images
install_wrapper
ensure_path

echo ""
echo -e "${GREEN}${BOLD}✅ Instalação concluída!${NC}"
echo ""
echo "  Execute:  nichebot"
echo "  Ajuda:    nichebot help"
echo ""
