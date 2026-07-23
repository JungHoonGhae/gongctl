#!/bin/sh
# gongctl 설치 스크립트 (macOS/Linux)
#
#   curl -fsSL https://raw.githubusercontent.com/JungHoonGhae/gongctl/main/install.sh | sh
#
# 환경변수:
#   INSTALL_DIR     설치 위치 (기본 /usr/local/bin)
#   GONGCTL_VERSION 특정 버전 고정 (예: v0.4.0, 기본 latest)
set -e

REPO="JungHoonGhae/gongctl"
BINARY="gongctl"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

main() {
    os=$(detect_os)
    arch=$(detect_arch)

    if [ "$os" = "windows" ]; then
        echo "Error: this script does not support Windows. Use PowerShell instead:"
        echo '  irm https://raw.githubusercontent.com/'"$REPO"'/main/install.ps1 | iex'
        exit 1
    fi

    version="${GONGCTL_VERSION:-$(latest_version)}"
    [ -n "$version" ] || { echo "Error: could not resolve latest version."; exit 1; }
    ver_no_v="${version#v}"

    asset="${BINARY}_${ver_no_v}_${os}_${arch}.tar.gz"
    base="https://github.com/${REPO}/releases/download/${version}"

    echo "Detected: ${os}/${arch}"
    echo "Installing ${BINARY} ${version}..."

    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' EXIT

    curl -fsSL -o "${tmpdir}/${asset}" "${base}/${asset}"
    curl -fsSL -o "${tmpdir}/checksums.txt" "${base}/checksums.txt"

    echo "Verifying checksum..."
    (
        cd "$tmpdir"
        grep " ${asset}\$" checksums.txt > asset.sha256
        if command -v shasum >/dev/null 2>&1; then
            shasum -a 256 -c asset.sha256 >/dev/null
        else
            sha256sum -c asset.sha256 >/dev/null
        fi
    )

    tar -xzf "${tmpdir}/${asset}" -C "$tmpdir"

    # 쓰기 가능하면 그대로, 아니면 sudo. 새 Apple Silicon 맥은 /usr/local/bin 이
    # 없을 수 있어(홈브루가 /opt/homebrew) mkdir -p 를 먼저 한다.
    if can_write "$INSTALL_DIR"; then
        SUDO=""
    else
        SUDO="sudo"
        echo "Elevated permissions required to write ${INSTALL_DIR}."
    fi
    $SUDO mkdir -p "$INSTALL_DIR"
    $SUDO mv "${tmpdir}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
    $SUDO chmod +x "${INSTALL_DIR}/${BINARY}"

    echo ""
    echo "Installed: $("${INSTALL_DIR}/${BINARY}" version 2>/dev/null || echo "$BINARY")"
    echo ""
    echo "Next steps:"
    echo "  gongctl login                                 # 브라우저 1회 로그인"
    echo "  gongctl search 대기오염 --type api -f table    # 데이터셋 검색"
    echo "  gongctl apply <pk> --purpose ... --category research"
}

latest_version() {
    # API 대신 releases/latest 리다이렉트에서 태그 추출 (rate limit 없음)
    curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest" \
        | sed 's#.*/tag/##'
}

detect_os() {
    case "$(uname -s)" in
        Darwin) echo "darwin" ;;
        Linux) echo "linux" ;;
        MINGW* | MSYS* | CYGWIN*) echo "windows" ;;
        *) echo "Error: unsupported OS $(uname -s)" >&2; exit 1 ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64 | amd64) echo "amd64" ;;
        arm64 | aarch64) echo "arm64" ;;
        *) echo "Error: unsupported architecture $(uname -m)" >&2; exit 1 ;;
    esac
}

# dir 이 아직 없으면 존재하는 최상위 조상에 쓰기 가능한지 본다 (mkdir -p 대비).
can_write() {
    d="$1"
    while [ ! -d "$d" ]; do
        d=$(dirname "$d")
    done
    [ -w "$d" ]
}

main "$@"
