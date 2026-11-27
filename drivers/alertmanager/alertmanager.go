// Package alertmanager reads Prometheus Alertmanager webhooks as Charter events.
package alertmanager

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/charterhq/charter/internal/driver"
)

// Format splits an Alertmanager webhook into one event per firing alert. Resolved alerts are dropped;
// the alert fingerprint is the object, fingerprint and start time make the delivery.
type Format struct{}

// Name is the receiver name connections use: triggers.<source>.receiver: alertmanager.
func (Format) Name() string { return "alertmanager" }

func (Format) Split(_ *http.Request, body []byte) ([]driver.Event, error) {
	var wh struct {
		GroupKey string            `json:"groupKey"`
		Alerts   []json.RawMessage `json:"alerts"`
	}
	if err := json.Unmarshal(body, &wh); err != nil {
		return nil, fmt.Errorf("not an Alertmanager webhook: %w", err)
	}
	var out []driver.Event
	for _, a := range wh.Alerts {
		var al struct {
			Status      string `json:"status"`
			Fingerprint string `json:"fingerprint"`
			StartsAt    string `json:"startsAt"`
		}
		if err := json.Unmarshal(a, &al); err != nil {
			return nil, err
		}
		if al.Status != "firing" {
			continue
		}
		out = append(out, driver.Event{
			DeliveryID: al.Fingerprint + "@" + al.StartsAt,
			Source:     "alertmanager",
			ObjectKey:  al.Fingerprint,
			Initiator:  "alertmanager", InitiatorRef: wh.GroupKey,
			Payload: a,
		})
	}
	return out, nil
}
