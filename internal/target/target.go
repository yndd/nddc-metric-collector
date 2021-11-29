package target

import (
	"github.com/karimra/gnmic/types"
)

// A TargetAction represents an action on a target
type Action string

// Action Kinds.
const (
	Add    Action = "add target"
	Delete Action = "delete target "
)

// Info identifies the target information
type Info struct {
	Name   string
	Action Action
	Config *types.TargetConfig
}
