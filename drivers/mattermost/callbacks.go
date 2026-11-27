package mattermost

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/charterhq/charter/internal/driver"
	"github.com/charterhq/charter/internal/signing"
)

// Name is the driver name: the core serves its callbacks under /v1/drivers/mattermost/.
const Name = "mattermost"

// Name implements driver.Callbacks.
func (c *Channel) Name() string { return Name }

// Handler takes approval button presses: POST actions/<wait-id> with a Mattermost integration request.
// The signed context binds the press to the wait, the draft fingerprint and the decision.
func (c *Channel) Handler(a driver.Approvals) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		waitID, ok := strings.CutPrefix(r.URL.Path, "/actions/")
		if r.Method != http.MethodPost || !ok || waitID == "" || strings.Contains(waitID, "/") {
			http.NotFound(w, r)
			return
		}
		var req struct {
			UserID   string            `json:"user_id"`
			UserName string            `json:"user_name"`
			Context  map[string]string `json:"context"`
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil || json.Unmarshal(body, &req) != nil {
			http.Error(w, "body is not a Mattermost integration request", http.StatusBadRequest)
			return
		}
		ctxv := req.Context
		if ctxv["wait_id"] != waitID || !signing.Verify(c.cfg.Secret, waitID, ctxv["fingerprint"], ctxv["decision"], ctxv["sig"]) {
			http.Error(w, "bad signature", http.StatusForbidden)
			return
		}
		decision := ctxv["decision"]
		if decision != "approve" && decision != "reject" {
			http.Error(w, "decision must be approve or reject", http.StatusBadRequest)
			return
		}
		by := req.UserName
		if by == "" {
			by = req.UserID
		}
		err = a.Resolve(r.Context(), waitID, by, ctxv["fingerprint"], decision == "approve")
		var text string
		switch {
		case err == nil && decision == "approve":
			text = c.say(c.cfg.ApprovedText, by)
		case err == nil:
			text = c.say(c.cfg.RejectedText, by)
		case errors.Is(err, driver.ErrWaitClosed):
			text = c.cfg.ClosedText
		case errors.Is(err, driver.ErrFingerprint):
			text = c.cfg.ChangedText
		case errors.Is(err, driver.ErrSameApprover):
			text = c.cfg.SameApproverText
		default:
			c.log().Error("mattermost: resolve wait", "wait", waitID, "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		// Mattermost shows ephemeral_text to the clicking user only.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ephemeral_text": text})
	})
}

func (c *Channel) say(tmpl, by string) string {
	if strings.Contains(tmpl, "%s") {
		return fmt.Sprintf(tmpl, by)
	}
	return tmpl
}
