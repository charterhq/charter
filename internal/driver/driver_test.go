package driver_test

import (
	"context"
	"testing"

	"github.com/charterhq/charter/internal/driver"
)

type ch struct{ name string }

func (ch) Post(context.Context, driver.Message) (driver.MessageRef, error) {
	return driver.MessageRef{}, nil
}
func (ch) AskApproval(context.Context, driver.ApprovalRequest) (driver.MessageRef, error) {
	return driver.MessageRef{}, nil
}

func TestChannelBySystem(t *testing.T) {
	chat, tracker := ch{"chat"}, ch{"tracker"}
	two := driver.Set{Channels: map[string]driver.Channel{"chat": chat, "tracker": tracker}}
	if c, sys := two.Channel("", "tracker", "chat"); c != tracker || sys != "tracker" {
		t.Fatalf("first known system wins: %v %s", c, sys)
	}
	if c, _ := two.Channel("slack"); c != nil {
		t.Fatal("unknown system with two channels must not guess")
	}
	one := driver.Set{Channels: map[string]driver.Channel{"chat": chat}}
	if c, sys := one.Channel("slack"); c != chat || sys != "chat" {
		t.Fatalf("the only channel serves a run without a known system: %v %s", c, sys)
	}
}
