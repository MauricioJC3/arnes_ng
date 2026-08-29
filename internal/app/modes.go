package app

import (
	"fmt"
	"strings"

	"github.com/MauricioJC3/arnes_ng/internal/approval"
)

// permission modes
const (
	ModeNormal = "normal"
	ModeAuto   = "auto"
	ModePlan   = "plan"
)

// Mode implements command.Modes.
func (a *App) Mode() string { return a.mode }

// SetMode implements command.Modes: switch the permission mode, rebuild, and
// persist the choice so it sticks across restarts.
func (a *App) SetMode(name string) (string, error) {
	mode, err := ParseMode(name)
	if err != nil {
		return "", err
	}
	a.mode = mode
	a.rebuild(a.sess, a.sess.Messages)

	a.cfg.Mode = mode
	if saveErr := a.cfg.Save(a.cfgPath); saveErr != nil {
		return "modo: " + mode + " (no se pudo guardar en la config: " + saveErr.Error() + ")", nil
	}
	return "modo: " + mode, nil
}

// effectiveApprover is the gateway for the current mode.
func (a *App) effectiveApprover() approval.Approver {
	switch a.mode {
	case ModeAuto:
		return approval.AllowAll{}
	case ModePlan:
		return approval.ReadOnly{Allowed: map[string]bool{"read_file": true, "recall": true}}
	default:
		return a.baseApprover
	}
}

// EffectiveApprover exposes the current-mode gateway for the composition root
// (the delegate tool runs subagents under it).
func (a *App) EffectiveApprover() approval.Approver { return a.effectiveApprover() }

// ParseMode normalizes a permission-mode string (bypass/yolo are aliases for
// auto). An empty string is normal.
func ParseMode(name string) (string, error) {
	switch name = strings.ToLower(strings.TrimSpace(name)); name {
	case "", ModeNormal:
		return ModeNormal, nil
	case ModeAuto, ModePlan:
		return name, nil
	case "bypass", "yolo":
		return ModeAuto, nil
	default:
		return "", fmt.Errorf("modo desconocido: %q (normal|auto|plan)", name)
	}
}

func modeAddendum(mode string) string {
	switch mode {
	case ModePlan:
		return "\n\nMODO PLAN ACTIVO: no modifiques nada. Investigá solo con read_file y proponé un plan " +
			"detallado paso a paso. Las herramientas que escriben o ejecutan comandos van a ser denegadas."
	case ModeAuto:
		return "\n\nMODO AUTO: las herramientas se ejecutan sin pedir confirmación. Cuidado con comandos destructivos."
	default:
		return ""
	}
}
