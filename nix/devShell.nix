{ pkgs }:
with pkgs;
let
  proxyPort = 8118;
  fakeDnsIp = "10.3.3.53";

  shellPackages = [
    go
    gcc

    # test dependencies
    nodejs
    socat

    # Required for building go dependencies
    autoconf
    automake
    git
    gnumake
    libtool
    flex
    pkg-config
    procps # pkill
    iproute2 # ip
  ]
  ++ lib.optional stdenv.isLinux pcsclite
  ++ lib.optional stdenv.isDarwin pkgs.darwin.apple_sdk.frameworks.PCSC;

  # Closure of exactly the packages needed in the sandbox — avoids exposing
  # the entire host nix store to the sandboxed process.
  sandboxClosure = pkgs.closureInfo {
    rootPaths = shellPackages ++ [ bashInteractive ];
  };

  privoxyActions = writeText "whitelist.action" ''
    {+block{not in whitelist}}
    /

    {-block}
    .anthropic.com
    anthropic.com
    .claude.com
    claude.com
    .golang.org
  '';

  privoxyConf = writeText "privoxy.conf" ''
    listen-address  127.0.0.1:${toString proxyPort}
    enforce-blocks  1
    actionsfile     ${privoxyActions}
    logdir          /tmp
    logfile         privoxy.log
  '';

  sandboxInit = writeShellScript "sandbox-init" ''
    set -euo pipefail

    GW=$(ip route show default | awk '/default/ {print $3}')
    DEV=$(ip route show default | awk '/default/ {print $5}')

    # Block outgoing network traffic by dropping the default route
    # Note: the sandbox will still have an assigned IP, which will allows
    # to route traffic to/from the sandbox
    ip route del default 2>/dev/null || true

    # Setup DNS: allow traffic to the forwarded DNS IP
    ip route add ${fakeDnsIp} via $GW dev $DEV  2>/dev/null || true
    RESOLV=$(mktemp)
    echo "nameserver ${fakeDnsIp}" > "$RESOLV"

    bwrap_args=(
      # Env variables
      --clearenv
      --setenv IN_BWRAP 1
      --setenv PATH "$PATH"
      --setenv HOME "$PWD"
      --setenv TERM "$TERM"
      --setenv HTTP_PROXY  "http://127.0.0.1:${toString proxyPort}"
      --setenv HTTPS_PROXY "http://127.0.0.1:${toString proxyPort}"

      # Tell Claude not to phone home
      --setenv CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC 1
      --setenv DISABLE_TELEMETRY 1
      --setenv DISABLE_ERROR_REPORTING 1

      # Filesystem mounts - order matters!
      --bind        "$PWD"      "$PWD"      # the only writable path is the project directory
      --ro-bind-try "$PWD/.git" "$PWD/.git" # ...except for .git, which is read-only
      # hide sensitive files
      --ro-bind     /dev/null    "$PWD/secrets"

      # needed for dynamic linking to work
      --ro-bind-try /lib /lib
      --ro-bind-try /lib64 /lib64

      # DNS setup
      --ro-bind "$RESOLV" /etc/resolv.conf

      # for HTTPS to work
      --ro-bind-try /etc/ssl/certs /etc/ssl/certs

      # Set up fake dev, proc and tmpfs
      --dev /dev
      --proc /proc
      --tmpfs /tmp

      # isolate everything
      --unshare-all
      # except for networking, which is isolated using `pasta` + `ip`
      --share-net

      # set a standard uid/gid, because some tools abort if ran with uid=0 (root)
      --uid 1000
      --gid 1000

      --die-with-parent
      --chdir "$PWD"
    )

    # Mount only the closure of the dev shell's packages, not the full store
    while IFS= read -r storePath; do
      bwrap_args+=(--ro-bind "$storePath" "$storePath")
    done < ${sandboxClosure}/store-paths

    echo "Entering sandbox..."
    exec ${bubblewrap}/bin/bwrap "''${bwrap_args[@]}" ${bashInteractive}/bin/bash
  '';

  sandboxEntry = writeShellScript "sandbox-entry" ''
    set -euo pipefail

    export PATH="$PWD/node_modules/.bin:$PATH"

    echo "Starting privoxy..."
    pkill -f privoxy
    ${privoxy}/bin/privoxy --no-daemon ${privoxyConf} &
    PRIVOXY_PID=$!
    trap "kill $PRIVOXY_PID 2>/dev/null || true" EXIT INT TERM

    pasta_args=(
      # re-map DNS requests to this IP to the real DNS server
      --dns-forward ${fakeDnsIp}

      # set up networking (initially, later dropped in sandboxInit)
      --config-net

      # Port forwarding: parent->sandbox
      # Expose any bound TCP or UDP port also on the "parent" localhost
      # (iff not bound already), allows to easily access services from otuside
      -t auto
      -u auto

      # Port forwarding: sandbox->parent
      # forward proxyPort from sandbox to parent
      -T ${toString proxyPort}

      -- ${sandboxInit}
    )

    echo "Entering isolated network ..."
    exec ${passt}/bin/pasta "''${pasta_args[@]}"
  '';
in
mkShell {
  buildInputs = shellPackages;

  shellHook = ''
    if [ -z "$IN_BWRAP" ]; then
      exec ${sandboxEntry}
    fi
  '';
}
