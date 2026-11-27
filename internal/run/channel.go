package run

import (
	"context"
	"fmt"
	"sort"

	"github.com/charterhq/charter/internal/driver"
)

// channelFor picks the channel driver for a message: the system of its target, then the systems of
// the agent identities, then the only channel of the installation. The target gets the chosen system.
func (e *Engine) channelFor(m *driver.Message) (driver.Channel, error) {
	systems := []string{m.Target.System}
	ids := make([]string, 0, len(m.Identities))
	for sys := range m.Identities {
		ids = append(ids, sys)
	}
	sort.Strings(ids)
	c, sys := e.drv.Channel(append(systems, ids...)...)
	if c == nil {
		return nil, fmt.Errorf("no channel driver for systems %v", append(systems, ids...))
	}
	m.Target.System = sys
	return c, nil
}

func (e *Engine) post(ctx context.Context, m driver.Message) (driver.MessageRef, error) {
	c, err := e.channelFor(&m)
	if err != nil {
		return driver.MessageRef{}, err
	}
	return c.Post(ctx, m)
}

func (e *Engine) askApproval(ctx context.Context, r driver.ApprovalRequest) (driver.MessageRef, error) {
	c, err := e.channelFor(&r.Message)
	if err != nil {
		return driver.MessageRef{}, err
	}
	return c.AskApproval(ctx, r)
}
