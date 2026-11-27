#!/usr/bin/env bash
# Preflight for `make up` / `make dev` / `make e2e-up`: fail early, with the
# .env variable to change, when a host port this Compose project publishes is
# already taken. Docker's own error ("Bind for 0.0.0.0:9001 failed: port is
# already allocated") names neither the variable nor the culprit.
#
# Usage: scripts/check-ports.sh [docker compose global flags, e.g. -p NAME -f FILE]
#
# Ports already held by this project's own containers are fine (they are about
# to be recreated). Never edits .env. Needs bash (uses /dev/tcp), docker and awk.
set -euo pipefail

compose=(docker compose "$@")

# Resolved config: also surfaces missing required .env values (e.g. credentials)
# with Compose's own message before we do anything else.
if ! resolved=$("${compose[@]}" config 2>&1); then
	printf '%s\n' "$resolved" >&2
	echo "Fix the configuration above (copy .env.example to .env and fill it in)." >&2
	exit 1
fi
raw=$("${compose[@]}" config --no-interpolate)
project=$(printf '%s\n' "$resolved" | sed -n 's/^name: //p' | head -n1)

# One line per published port, in config order (services sorted, ports in
# declaration order), from the raw config: "service<TAB>VARIABLE" where
# VARIABLE is the ${VAR...} in the host part, or "-" if hardcoded.
port_vars=$(printf '%s\n' "$raw" | awk '
	/^  [^ #][^:]*:$/ && in_services { svc = $1; sub(":", "", svc) }
	/^services:$/ { in_services = 1; next }
	/^[^ ]/ { in_services = 0 }
	/^    ports:$/ { in_ports = 1; next }
	in_ports && /^      - / {
		var = "-"
		if (match($0, /\$\{[A-Za-z_][A-Za-z0-9_]*/)) var = substr($0, RSTART + 2, RLENGTH - 2)
		print svc "\t" var
		next
	}
	in_ports && !/^        / { in_ports = 0 }
')
# Same order from the resolved (long-syntax) config: the published host port.
published=$(printf '%s\n' "$resolved" | awk '
	/^    ports:$/ { in_ports = 1; next }
	in_ports && /^        published: / { p = $2; gsub(/"/, "", p); print p; next }
	in_ports && !/^      / { in_ports = 0 }
')

if [ "$(printf '%s\n' "$port_vars" | grep -c .)" != "$(printf '%s\n' "$published" | grep -c .)" ]; then
	echo "check-ports: could not pair ports with .env variables; skipping preflight." >&2
	exit 0
fi

# Expand `docker ps` port strings ("0.0.0.0:9000-9001->9000-9001/tcp, ...")
# into "host_port<TAB>container_name" lines.
docker_ports() {
	docker ps "$@" --format '{{.Names}}	{{.Ports}}' | awk -F'\t' '{
		n = split($2, parts, ", ")
		for (i = 1; i <= n; i++) {
			if (index(parts[i], "->") == 0) continue
			host = parts[i]; sub(/->.*/, "", host); sub(/.*:/, "", host)
			lo = host; hi = host
			if (index(host, "-")) { lo = host; sub(/-.*/, "", lo); hi = host; sub(/.*-/, "", hi) }
			for (p = lo + 0; p <= hi + 0; p++) print p "\t" $1
		}
	}' | sort -u
}
own=$(docker_ports --filter "label=com.docker.compose.project=$project" | cut -f1)
others=$(docker_ports)

in_use() { (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }

failed=0
seen=""
while IFS=$'\t' read -r svc var port; do
	label="$port (${var/#-/hardcoded in compose}, service $svc)"
	if printf '%s\n' "$seen" | grep -qx "$port"; then
		echo "Port $label is published twice in this stack. Give it a unique value in .env." >&2
		failed=1
		continue
	fi
	seen="$seen"$'\n'"$port"
	if printf '%s\n' "$own" | grep -qx "$port"; then
		continue
	fi
	if in_use "$port"; then
		holder=$(printf '%s\n' "$others" | awk -F'\t' -v p="$port" '$1 == p { print $2; exit }')
		echo "Port $label is already in use${holder:+ by Docker container '$holder'}." >&2
		[ "$var" != "-" ] && echo "  Set $var to a free port in .env (see .env.example)." >&2
		failed=1
	fi
done < <(paste <(printf '%s\n' "$port_vars") <(printf '%s\n' "$published"))

if [ "$failed" -ne 0 ]; then
	echo "Host port preflight failed for Compose project '$project'; nothing was started." >&2
	exit 1
fi
