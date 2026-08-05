package rest

// Log panel — tails NATS's own log_file (nats/nats.conf), REST-polled like
// every other Admin UI NATS panel rather than a push/follow transport (see
// the design discussion this implements: level/q are the only filters,
// tail has a hard server-side ceiling, no log rotation is handled — this is
// a lab convenience, not a production log pipeline).
import (
	"bufio"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// natsLogMaxTail is the hard ceiling on the number of lines returned,
// regardless of what the caller asks for via ?tail= — an unbounded tail
// would turn a monitoring endpoint into a way to read the entire log file
// in one request.
const natsLogMaxTail = 1000

// natsLogDefaultTail is used when the caller omits ?tail= entirely.
const natsLogDefaultTail = 200

// natsLogMaxReadBytes bounds how much of the file is ever read from disk,
// seeking to the last N bytes first — without this, a log file that grew
// large between the occasional restarts this feature assumes (see
// nats.conf's log_file comment: no rotation) would make every poll read
// the whole file just to keep the last few hundred lines.
const natsLogMaxReadBytes = 8 * 1024 * 1024 // 8MB

var natsLogLevels = map[string]string{
	"error": "[ERR]",
	"warn":  "[WRN]",
	"info":  "[INF]",
	"debug": "[DBG]",
	"trace": "[TRC]",
}

type natsLogResponse struct {
	Lines []string `json:"lines"`
	// Truncated is true when more matching lines existed than natsLogMaxTail
	// (or the caller's smaller ?tail=) could return — lets the UI say "showing
	// the most recent N" instead of implying this is the complete history.
	Truncated bool `json:"truncated"`
}

// tailNatsLog godoc
//
// @Summary      Tail the NATS server log
// @Description  The last N lines of NATS's own log_file, optionally filtered by level ([ERR]/[WRN]/[INF]/[DBG]/[TRC]) and/or a free-text substring — REST-polled, not a push/follow transport. tail is capped server-side at 1000 regardless of what's requested. Returns 503 if NatsLogPath was never configured (e.g. running outside Docker).
// @Tags         nats
// @Produce      json
// @Param        level  query  string  false  "error|warn|info|debug|trace — omit for all levels"
// @Param        q      query  string  false  "case-insensitive substring filter, applied in addition to level"
// @Param        tail   query  int     false  "number of matching lines to return (default 200, hard-capped at 1000)"
// @Success      200  {object}  natsLogResponse
// @Failure      503  {object}  errorResponse  "log tailing not configured"
// @Router       /api/nats/log [get]
func (h *Handlers) tailNatsLog(w http.ResponseWriter, r *http.Request) {
	deps := h.deps()
	if deps.NatsLogPath == "" {
		writeError(w, http.StatusServiceUnavailable, "log tailing not configured (NATS_LOG_PATH unset)")
		return
	}

	tail := natsLogDefaultTail
	if raw := r.URL.Query().Get("tail"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			tail = n
		}
	}
	if tail > natsLogMaxTail {
		tail = natsLogMaxTail
	}

	levelTag := ""
	if lvl := strings.ToLower(r.URL.Query().Get("level")); lvl != "" {
		levelTag = natsLogLevels[lvl] // unrecognized value silently matches nothing, same as a level with zero log lines
	}
	q := strings.ToLower(r.URL.Query().Get("q"))

	lines, err := readLastLines(deps.NatsLogPath)
	if err != nil {
		deps.Log.Error("tail nats log", "path", deps.NatsLogPath, "err", err)
		writeError(w, http.StatusBadGateway, "nats log file unreadable")
		return
	}

	matched := make([]string, 0, tail)
	for i := len(lines) - 1; i >= 0 && len(matched) < tail; i-- {
		line := lines[i]
		if levelTag != "" && !strings.Contains(line, levelTag) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(line), q) {
			continue
		}
		matched = append(matched, line)
	}
	// matched was built newest-first (walking the file backwards); reverse
	// to the natural oldest-first reading order before returning.
	for i, j := 0, len(matched)-1; i < j; i, j = i+1, j-1 {
		matched[i], matched[j] = matched[j], matched[i]
	}

	writeJSON(w, http.StatusOK, natsLogResponse{
		Lines:     matched,
		Truncated: len(matched) == tail,
	})
}

// readLastLines reads at most natsLogMaxReadBytes from the end of path and
// splits it into lines. The first line of that window may be a partial
// line (cut mid-way by the seek) — dropped rather than returned truncated,
// since it's discarded before any filtering happens anyway.
func readLastLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > natsLogMaxReadBytes {
		if _, err := f.Seek(-natsLogMaxReadBytes, io.SeekEnd); err != nil {
			return nil, err
		}
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var lines []string
	first := info.Size() > natsLogMaxReadBytes
	for scanner.Scan() {
		if first {
			first = false // the seek landed mid-line; that first read is a fragment, not a real line
			continue
		}
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}
