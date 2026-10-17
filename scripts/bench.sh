#!/bin/bash
#
# Terminal throughput benchmark.
#
# Measures how fast the host terminal drains and renders PTY output. Run the
# SAME script in each terminal you want to compare, with the same font, the
# same window size, and on the same display. Results are only comparable
# under those conditions.
#
# Usage: ./scripts/bench.sh            benchmark, then compare against the
#                                      latest run of the other terminal
#        ./scripts/bench.sh --compare  reprint that comparison only
#
# BENCH_VS=<name> compares against that terminal's latest run (its
# TERM_PROGRAM, e.g. ghostty) instead of whichever non-Raven run is newest.
#

set -e

CORPUS_DIR="${TMPDIR:-/tmp}/raven-bench"
RESULTS="${BENCH_OUT:-$PWD/terminal_output.txt}"
LINES_PLAIN=100000
LINES_SGR=50000
LINES_CURSOR=30000

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

print_success() { echo -e "${GREEN}[OK]${NC} $1"; }
print_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
print_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# Raven against the most recent run of another terminal, both read back from
# $RESULTS. hyperfine's own Change/Factor columns compare each corpus to
# `plain` within one terminal, which reads like a cross-terminal result and
# isn't one. Plain POSIX awk: this also runs under macOS's BWK awk.
compare_latest() {
    if [ ! -f "$RESULTS" ]; then
        print_error "No results file at $RESULTS"
        return 1
    fi
    awk -v vs="${BENCH_VS:-}" -v results="$RESULTS" \
        -v yellow="$YELLOW" -v nc="$NC" '
    function pad(n,   s) { s = ""; while (n-- > 0) s = s " "; return s }
    function max(a, b) { return a > b ? a : b }
    # "62.0 ± 4.6" left-aligned in w columns. Width is measured from the
    # numbers, not the string: awks disagree on whether ± is 1 or 2 long.
    function cell(m, s, w,   a, b) {
        a = sprintf("%.1f", m); b = sprintf("%.1f", s)
        return a " ± " b pad(w - length(a) - length(b) - 3)
    }

    /^## / {
        n++
        i = index($0, " — ")
        name[n] = substr($0, 4, i - 4)
        when[n] = substr($0, i + length(" — "))
        next
    }
    /^- Geometry:/ { geom[n] = $3; next }
    /^- Corpus:/   { corpus[n] = substr($0, 11); next }
    # Older hyperfine writes "Mean [s]" for slow runs; normalise to ms.
    /^\| Command/ {
        split($0, f, "|")
        unit = f[3]; sub(/.*\[/, "", unit); sub(/\].*/, "", unit)
        scale = (unit == "s") ? 1000 : (unit == "µs") ? 0.001 : 1
        next
    }
    /^\| `/ {
        split($0, f, "|")
        cmd = f[2]; gsub(/[` ]/, "", cmd)
        split(f[3], v, " ")          # "63.2 ± 4.2"
        mean[n, cmd] = v[1] * scale
        sd[n, cmd] = v[3] * scale
        cmds[n] = cmds[n] " " cmd
    }

    END {
        for (k = n; k >= 1; k--) {
            if (cmds[k] == "") continue
            lname = tolower(name[k])
            if (lname ~ /^raven/) { if (!r) r = k }
            else if (!o && (vs == "" || lname == tolower(vs))) o = k
        }
        if (!r || !o) {
            missing = !r ? "RavenTerminal" : (vs != "" ? vs : "other-terminal")
            printf "No %s run in %s yet; run ./scripts/bench.sh there to get the comparison.\n", missing, results
            exit 0
        }

        nw = max(length(name[r]), length(name[o]))
        printf "\nRaven vs %s (latest run of each)\n", name[o]
        printf "  %s%s  %s  %s\n", name[r], pad(nw - length(name[r])), when[r], geom[r]
        printf "  %s%s  %s  %s\n\n", name[o], pad(nw - length(name[o])), when[o], geom[o]

        rl = name[r] " [ms]"; ol = name[o] " [ms]"
        rw = max(16, length(rl) + 2); ow = max(16, length(ol) + 2)
        printf "  %-8s%s%s%s%sresult\n", "corpus", rl, pad(rw - length(rl)), ol, pad(ow - length(ol))

        split(substr(cmds[r], 2), list, " ")
        for (j = 1; list[j] != ""; j++) {
            c = list[j]
            if (!((o, c) in mean)) continue
            mr = mean[r, c]; mo = mean[o, c]
            if (mr <= mo) verdict = sprintf("Raven %.2fx faster", mo / mr)
            else          verdict = sprintf("Raven %.2fx slower", mr / mo)
            # A gap smaller than the two spreads combined could flip on rerun.
            diff = mo - mr; if (diff < 0) diff = -diff
            if (diff <= sd[r, c] + sd[o, c]) verdict = verdict " (within noise)"
            printf "  %-8s%s%s%s\n", c, cell(mr, sd[r, c], rw), cell(mo, sd[o, c], ow), verdict
        }

        if (geom[r] != geom[o])
            printf "\n%s[WARNING]%s Geometry differs (%s vs %s): resize both windows to match and rerun.\n", \
                yellow, nc, geom[r], geom[o]
        if (corpus[r] != corpus[o])
            printf "%s[WARNING]%s Corpus sizes differ (%s vs %s): rerun both.\n", \
                yellow, nc, corpus[r], corpus[o]
    }' "$RESULTS"
}

# --compare reprints the table from saved results without benchmarking, so it
# skips the terminal check below.
if [ "$1" = "--compare" ]; then
    compare_latest
    exit $?
fi

# The whole point is to time the terminal's render loop. Piped into a file or
# another process there is no renderer involved and the numbers are garbage,
# so refuse rather than print a meaningless result.
if [ ! -t 1 ]; then
    print_error "stdout is not a terminal."
    echo "This benchmark must run in a real terminal window, not piped or captured."
    exit 1
fi

if ! command -v hyperfine >/dev/null 2>&1; then
    print_error "hyperfine not found. Install with: brew install hyperfine"
    exit 1
fi

mkdir -p "$CORPUS_DIR"

# Line counts are in the filenames so changing them above busts stale corpora
# instead of silently reusing a differently-sized file from an earlier run.
PLAIN="$CORPUS_DIR/plain-$LINES_PLAIN.txt"
SGR="$CORPUS_DIR/sgr-$LINES_SGR.txt"
CURSOR="$CORPUS_DIR/cursor-$LINES_CURSOR.txt"

# Plain text: parser + glyph rasterization path.
if [ ! -f "$PLAIN" ]; then
    echo "Generating plain corpus (${LINES_PLAIN} lines)..."
    yes 'The quick brown fox jumps over the lazy dog 0123456789' \
        | head -"$LINES_PLAIN" > "$PLAIN"
fi

# Colored text: SGR escape-sequence handling, where terminals actually diverge.
if [ ! -f "$SGR" ]; then
    echo "Generating SGR corpus (${LINES_SGR} lines)..."
    awk 'BEGIN {
        for (i = 0; i < '"$LINES_SGR"'; i++)
            printf "\033[31mred\033[32mgreen\033[34mblue\033[0m %d\n", i
    }' > "$SGR"
fi

# Scroll region + cursor movement, the other common divergence.
if [ ! -f "$CURSOR" ]; then
    echo "Generating cursor corpus (${LINES_CURSOR} lines)..."
    awk 'BEGIN {
        for (i = 0; i < '"$LINES_CURSOR"'; i++)
            printf "\033[s\033[1;1Hline %d\033[u\033[K%d\n", i, i
    }' > "$CURSOR"
fi

# Cell count drives how much the terminal has to draw, so an unmatched window
# size invalidates the comparison outright. Ask for a fixed geometry via the
# XTWINOPS resize sequence rather than trusting the eye.
WANT_COLS="${BENCH_COLS:-100}"
WANT_ROWS="${BENCH_ROWS:-30}"

printf '\033[8;%d;%dt' "$WANT_ROWS" "$WANT_COLS"
sleep 0.4   # the resize is async; give the window manager time to land it

COLS=$(tput cols)
ROWS=$(tput lines)

echo
print_success "Terminal:  ${TERM_PROGRAM:-unknown} (TERM=$TERM)"

# XTWINOPS is optional and some terminals ignore or clamp it, so verify instead
# of assuming. A refused resize is fine as long as both terminals end up equal.
if [ "$COLS" = "$WANT_COLS" ] && [ "$ROWS" = "$WANT_ROWS" ]; then
    print_success "Geometry:  ${COLS}x${ROWS} (pinned)"
else
    print_warning "Geometry:  ${COLS}x${ROWS} — asked for ${WANT_COLS}x${WANT_ROWS}, terminal refused."
    print_warning "Resize manually to ${WANT_COLS}x${WANT_ROWS}, or the comparison is void."
fi
print_warning "Screen will flood; that is the benchmark running."
echo

# --show-output is load-bearing: without it hyperfine sends the child's stdout
# to /dev/null, so cat never reaches the terminal and this measures page-cache
# read speed instead of rendering. Its own docs require it for output speed.
SUMMARY=$(mktemp)
trap 'rm -f "$SUMMARY"' EXIT

hyperfine --warmup 1 --runs 5 --show-output \
    --export-markdown "$SUMMARY" \
    --command-name 'plain'  "cat $PLAIN" \
    --command-name 'sgr'    "cat $SGR" \
    --command-name 'cursor' "cat $CURSOR"

# Append rather than overwrite: comparing two terminals means two runs, and a
# clobbering write would leave only whichever ran last.
{
    echo
    echo "## ${TERM_PROGRAM:-unknown} — $(date '+%Y-%m-%d %H:%M:%S')"
    echo
    echo "- TERM: \`$TERM\`"
    echo "- Geometry: ${COLS}x${ROWS}"
    echo "- Corpus: plain=${LINES_PLAIN} sgr=${LINES_SGR} cursor=${LINES_CURSOR} lines"
    echo
    cat "$SUMMARY"
} >> "$RESULTS"

compare_latest

echo
# ponytail: cat returns when the PTY drains, not when pixels land. A terminal
# that reads greedily and drops frames scores well here while looking worse.
# Frame-accurate timing needs a high-speed camera; add that only if these
# numbers land within ~10% and you need to break the tie.
print_warning "Timing measures PTY drain, not frames drawn. Watch whether the scroll actually looked smooth."
print_success "Results appended to $RESULTS"
