#!/usr/bin/env bash
set -euo pipefail

BASE_PORT=${BASE_PORT:-9090}
DATA_ROOT=${DATA_ROOT:-"/tmp/quorum-data"}
BINARY=${BINARY:-"quorum"}
LOGDIR=${LOGDIR:-"/tmp"}

cmd=${1:-}
shift || true

launch_terminal() {
  local title="$1" cmd="$2"
  local term
  if command -v gnome-terminal &>/dev/null; then
    term="gnome-terminal"
  elif command -v xterm &>/dev/null; then
    term="xterm"
  elif command -v kitty &>/dev/null; then
    term="kitty"
  else
    echo "no terminal emulator found (tried gnome-terminal, xterm, kitty)"
    exit 1
  fi

  case "$term" in
    gnome-terminal)
      gnome-terminal --window --title="$title" -- bash -c "$cmd; echo; echo 'exited — press enter to close'; read" &
      ;;
    xterm)
      xterm -T "$title" -e bash -c "$cmd; echo; echo 'exited — press enter to close'; read" &
      ;;
    kitty)
      kitty --title "$title" bash -c "$cmd; echo; echo 'exited — press enter to close'; read" &
      ;;
  esac
}

case "$cmd" in
  start)
    N=3
    TABS=false
    while [[ $# -gt 0 ]]; do
      case "$1" in
        --tabs|-t) TABS=true; shift ;;
        *) N=$1; shift ;;
      esac
    done

    killall -9 quorum 2>/dev/null || true
    sleep 1

    CLUSTER=""
    for i in $(seq 0 $((N - 1))); do
      CLUSTER="${CLUSTER}node${i}=127.0.0.1:$((BASE_PORT + i)),"
    done
    CLUSTER="${CLUSTER%,}"

    echo "starting ${N}-node cluster: ${CLUSTER}"

    for i in $(seq 0 $((N - 1))); do
      mkdir -p "${DATA_ROOT}-${i}"
      QCMD="${BINARY} --id node${i} --addr :$((BASE_PORT + i)) --initial-cluster ${CLUSTER} --data-dir ${DATA_ROOT}-${i}"

      if $TABS; then
        launch_terminal "node${i}" "${QCMD}"
      else
        ${BINARY} \
          --id "node${i}" \
          --addr ":$((BASE_PORT + i))" \
          --initial-cluster "${CLUSTER}" \
          --data-dir "${DATA_ROOT}-${i}" \
          > "${LOGDIR}/quorum-node${i}.log" 2>&1 &
      fi
    done

    if $TABS; then
      echo "opened ${N} terminal windows"
      exit 0
    fi

    echo "logs: ${LOGDIR}/quorum-node*.log"
    echo "waiting for leader election..."

    for _ in $(seq 1 10); do
      sleep 1
      for pid in $(pgrep quorum 2>/dev/null); do
        node=$(tr '\0' ' ' < "/proc/${pid}/cmdline" 2>/dev/null | grep -o -- '--id [^ ]*' | cut -d' ' -f2 || true)
        [ -z "$node" ] && continue
        if grep -q "became leader" "${LOGDIR}/quorum-${node}.log" 2>/dev/null; then
          echo "leader: ${node}"
          exit 0
        fi
      done
    done
    echo "(no leader yet — check logs)"
    ;;

  stop)
    echo "stopping quorum nodes..."
    killall -9 quorum 2>/dev/null || echo "(none running)"
    ;;

  status)
    PIDS=$(pgrep quorum 2>/dev/null | paste -sd, - || true)
    if [ -z "$PIDS" ]; then
      echo "no nodes running"
      exit 1
    fi
    echo "running nodes:"
    ps -p "$PIDS" -o pid,args | tail -n +2 | sed 's/^/  /'
    echo ""

    LEADER=""
    for pid in $(pgrep quorum); do
      node=$(tr '\0' ' ' < "/proc/${pid}/cmdline" 2>/dev/null | grep -o -- '--id [^ ]*' | cut -d' ' -f2 || true)
      [ -z "$node" ] && continue
      line=$(grep -h "became leader" "${LOGDIR}/quorum-${node}.log" 2>/dev/null | tail -1 || true)
      [ -n "$line" ] && LEADER="$line"
    done

    if [ -n "$LEADER" ]; then
      echo "leader: $(echo "$LEADER" | grep -o 'node[0-9]')"
    else
      echo "(no leader elected)"
    fi
    ;;

  clean)
    rm -rf "${DATA_ROOT}"-*
    echo "data dirs cleaned: ${DATA_ROOT}-*"
    ;;

  *)
    echo "usage: $0 {start [N] [--tabs|-t]|stop|status|clean}"
    exit 1
    ;;
esac
