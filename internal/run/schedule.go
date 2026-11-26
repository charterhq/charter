package run

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/riverqueue/river"
	"github.com/robfig/cron/v3"
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// ParseSchedule parses a five-field cron or a descriptor (@hourly, @every 10m). A "CRON_TZ=Area/City "
// prefix sets the time zone of the schedule, otherwise loc is used.
func ParseSchedule(spec string, loc *time.Location) (cron.Schedule, error) {
	s, err := cronParser.Parse(spec)
	if err != nil {
		return nil, err
	}
	if ss, ok := s.(*cron.SpecSchedule); ok && ss.Location == time.Local && loc != nil {
		ss.Location = loc
	}
	return s, nil
}

type scheduleArgs struct{}

func (scheduleArgs) Kind() string { return "charter_schedule_tick" }

// InsertOpts: a failed tick is not retried, the next one covers its window.
func (scheduleArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: 1} }

type scheduleWorker struct {
	river.WorkerDefaults[scheduleArgs]
	e *Engine
}

func (w *scheduleWorker) Work(ctx context.Context, _ *river.Job[scheduleArgs]) error {
	_, err := w.e.Tick(ctx)
	return err
}

// Due is a schedule occurrence the tick launched or found already launched.
type Due struct {
	Agent, Connection, Scenario, Source string
	At                                  time.Time
	RunID                               string
	Duplicate                           bool
}

// Tick launches the schedule triggers due since the lookback window began. Each occurrence has a
// fixed delivery ID, so a repeated tick, a second core or a restart do not launch it twice.
func (e *Engine) Tick(ctx context.Context) ([]Due, error) { return e.TickAt(ctx, e.now()) }

// TickAt is Tick at a given time.
func (e *Engine) TickAt(ctx context.Context, now time.Time) ([]Due, error) {
	rows, err := e.pool.Query(ctx, `
		SELECT DISTINCT ON (agent) agent, package -> 'scenarios', package -> 'connections'
		FROM agent_versions WHERE applied_at IS NOT NULL
		ORDER BY agent, applied_at DESC`)
	if err != nil {
		return nil, err
	}
	type current struct {
		agent string
		pkg   Package
	}
	var agents []current
	for rows.Next() {
		var c current
		var scn, conns []byte
		if err := rows.Scan(&c.agent, &scn, &conns); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal(scn, &c.pkg.Scenarios); err != nil {
			e.log.Warn("schedule: bad package", "agent", c.agent, "err", err)
			continue
		}
		if len(conns) > 0 && string(conns) != "null" {
			if err := json.Unmarshal(conns, &c.pkg.Connections); err != nil {
				e.log.Warn("schedule: bad connections", "agent", c.agent, "err", err)
				continue
			}
		}
		agents = append(agents, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var out []Due
	for _, a := range agents {
		for _, d := range dueOccurrences(a.agent, a.pkg, now.Add(-e.lookback), now, e.loc, e.log.Warn) {
			ct := a.pkg.Connections[d.Connection].Triggers[d.Source]
			payload := json.RawMessage(`{}`)
			if len(ct.Input) > 0 {
				payload = ct.Input
			}
			env := Envelope{
				DeliveryID: fmt.Sprintf("%s/%s/%s/%s@%s", d.Agent, d.Connection, d.Scenario, d.Source, d.At.UTC().Format(time.RFC3339)),
				Trigger:    Trigger{Type: "schedule", Source: d.Source},
				Connection: d.Connection,
				Initiator:  Initiator{Kind: "system", ID: "scheduler"},
				Payload:    payload,
			}
			id, dup, err := e.Accept(ctx, d.Agent, d.Scenario, env)
			if err != nil {
				e.log.Warn("schedule: launch failed", "agent", d.Agent, "connection", d.Connection, "scenario", d.Scenario, "err", err)
				continue
			}
			d.RunID, d.Duplicate = id, dup
			if !dup {
				e.log.Info("schedule: launched", "agent", d.Agent, "connection", d.Connection, "scenario", d.Scenario,
					"source", d.Source, "at", d.At, "run_id", id)
			}
			out = append(out, d)
		}
	}
	return out, nil
}

// dueOccurrences lists schedule occurrences in (from, to] for the connections of a package.
func dueOccurrences(agent string, p Package, from, to time.Time, loc *time.Location, warn func(string, ...any)) []Due {
	var out []Due
	for cname, c := range p.Connections {
		if c.Status == ConnPaused {
			continue
		}
		for _, sname := range c.Scenarios {
			for _, t := range p.Scenarios[sname].Triggers {
				ct, bound := c.Triggers[t.Source]
				if t.Type != "schedule" || !bound {
					continue
				}
				spec := t.Schedule
				if ct.Schedule != "" {
					spec = ct.Schedule
				}
				s, err := ParseSchedule(spec, loc)
				if err != nil {
					warn("schedule: bad cron", "agent", agent, "connection", cname, "scenario", sname, "cron", spec, "err", err)
					continue
				}
				for at := s.Next(from); !at.IsZero() && !at.After(to); at = s.Next(at) {
					out = append(out, Due{Agent: agent, Connection: cname, Scenario: sname, Source: t.Source, At: at})
				}
			}
		}
	}
	return out
}
