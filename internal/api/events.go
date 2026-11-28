package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/charterhq/charter/internal/run"
)

// heartbeat keeps idle streams open through proxies.
const heartbeat = 15 * time.Second

// hub wakes the event streams of this core: one LISTEN connection outside the pool, a wake-up per
// object. Events themselves are read from the database, so a missed wake-up loses nothing.
type hub struct {
	pool *pgxpool.Pool
	once sync.Once
	mu   sync.Mutex
	subs map[string]map[chan struct{}]struct{}
	done chan struct{}
	stop context.CancelFunc
}

func newHub(pool *pgxpool.Pool) *hub {
	return &hub{pool: pool, subs: map[string]map[chan struct{}]struct{}{}, done: make(chan struct{})}
}

// subscribe returns a channel that gets a signal when the object may have new events.
func (h *hub) subscribe(objectID string) (chan struct{}, func()) {
	h.once.Do(h.start)
	c := make(chan struct{}, 1)
	h.mu.Lock()
	if h.subs[objectID] == nil {
		h.subs[objectID] = map[chan struct{}]struct{}{}
	}
	h.subs[objectID][c] = struct{}{}
	h.mu.Unlock()
	return c, func() {
		h.mu.Lock()
		delete(h.subs[objectID], c)
		if len(h.subs[objectID]) == 0 {
			delete(h.subs, objectID)
		}
		h.mu.Unlock()
	}
}

func (h *hub) wake(objectID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, cs := range h.subs {
		if objectID != "" && id != objectID {
			continue
		}
		for c := range cs {
			select {
			case c <- struct{}{}:
			default:
			}
		}
	}
}

func (h *hub) start() {
	ctx, cancel := context.WithCancel(context.Background())
	h.stop = cancel
	go func() {
		for ctx.Err() == nil {
			err := h.listen(ctx)
			if ctx.Err() != nil {
				return
			}
			// the connection broke: streams read what they may have missed, then we listen again
			h.wake("")
			_ = err
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
		}
	}()
}

func (h *hub) listen(ctx context.Context) error {
	conn, err := h.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	pc := conn.Hijack() // a LISTEN connection must not go back to the pool
	defer func() { _ = pc.Close(context.Background()) }()
	if _, err := pc.Exec(ctx, "LISTEN "+run.NotifyChannel); err != nil {
		return err
	}
	h.wake("") // events committed before LISTEN started
	for {
		n, err := pc.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		id, _, _ := strings.Cut(n.Payload, ":")
		h.wake(id)
	}
}

// close ends all streams of this core: on shutdown they must not hold the server.
func (h *hub) close() {
	select {
	case <-h.done:
		return
	default:
	}
	close(h.done)
	if h.stop != nil {
		h.stop()
	}
}

// StopStreams ends the event streams; call it when the HTTP server shuts down.
func (s *Server) StopStreams() { s.hub().close() }

func (s *Server) hub() *hub {
	s.hubOnce.Do(func() { s.events = newHub(s.Pool) })
	return s.events
}

// objectEvents streams the events of an object as server-sent events.
//
//	GET /v1/objects/{id}/events?after={seq}
//
// The stream continues after the seq from the Last-Event-ID header (a reconnecting browser sends it),
// else from ?after (the last_seq of the snapshot). When events the client needs are no longer kept,
// the stream sends "reset" and ends: the client reads the snapshot again.
func (s *Server) objectEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := s.visible(w, r)
	if !ok {
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			after = n
		}
	}
	wake, unsubscribe := s.hub().subscribe(id) // before the first read: nothing slips between
	defer unsubscribe()

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{}) // a stream outlives any write timeout of the server
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, ": stream\n\n")
	_ = rc.Flush()

	tick := time.NewTicker(heartbeat)
	defer tick.Stop()
	ctx := r.Context()
	for {
		evs, oldest, err := s.Engine.Events(ctx, id, after, 500)
		if err != nil {
			if ctx.Err() == nil {
				s.Log.Error("object events", "object", id, "err", err)
			}
			return
		}
		if after+1 < oldest {
			_, _ = fmt.Fprintf(w, "event: reset\ndata: {\"oldest\":%d}\n\n", oldest)
			_ = rc.Flush()
			return
		}
		for _, ev := range evs {
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.Seq, ev.Type, ev.Data); err != nil {
				return
			}
			after = ev.Seq
		}
		if len(evs) > 0 {
			if err := rc.Flush(); err != nil {
				return
			}
			if len(evs) == 500 {
				continue // more are waiting
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-s.hub().done:
			return
		case <-wake:
		case <-tick.C:
			// the session or the role may have ended since the stream opened
			if !s.stillVisible(r, id) {
				_, _ = fmt.Fprint(w, "event: end\ndata: {}\n\n")
				_ = rc.Flush()
				return
			}
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		}
	}
}

// stillVisible checks again that a stream's caller may see the object: a person's session is alive
// (and its groups fresh), and the person still has a role on the object or is the platform.
func (s *Server) stillVisible(r *http.Request, id string) bool {
	ctx := r.Context()
	c := who(ctx)
	if c.Kind == KindPerson {
		ck, err := r.Cookie(sessionCookie)
		if err != nil {
			return false
		}
		if c, err = sessionLookup(ctx, s.Pool, s.OIDC, s.Log, ck.Value, func(time.Duration) {}); err != nil {
			return false
		}
		ctx = context.WithValue(ctx, ctxKey{}, c)
	}
	if s.platform(ctx) {
		return true
	}
	ok, err := s.Engine.HasRole(ctx, id, c.Name)
	return err == nil && ok
}
