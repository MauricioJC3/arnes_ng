package main

import (
	"fmt"

	"github.com/MauricioJC3/arnes_ng/internal/setup"
)

const initUsage = "uso: arnes init [uiskills | impeccable [--from <ruta|url>]]"

// runInit handles `arnes init <target>`: it writes the ~/.arnes config that
// wires a fresh install for UI Skills and/or impeccable, then exits (no TUI).
func runInit(args []string) error {
	var target, from string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--from":
			if i+1 >= len(args) {
				return fmt.Errorf("--from necesita una ruta o url\n%s", initUsage)
			}
			from = args[i+1]
			i++
		case "-h", "--help":
			fmt.Println(initUsage)
			return nil
		default:
			if target == "" {
				target = args[i]
			}
		}
	}
	if target == "" {
		target = "uiskills"
	}

	paths, err := setup.DefaultPaths()
	if err != nil {
		return err
	}

	switch target {
	case "uiskills", "ui-skills":
		msg, err := setup.InitUISkills(paths)
		if err != nil {
			return err
		}
		fmt.Println(msg)
		return nil
	case "impeccable":
		rep, err := setup.InitImpeccable(paths, from)
		if err != nil {
			return err
		}
		fmt.Print(rep.String())
		return nil
	default:
		return fmt.Errorf("target desconocido: %q\n%s", target, initUsage)
	}
}
