package main

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	styHeader   = lipgloss.NewStyle().Foreground(lipgloss.Color("44")).Bold(true)
	styBusy     = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	styIdle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	styWait     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	styDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styTitle    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	styModel    = lipgloss.NewStyle().Foreground(lipgloss.Color("103"))
	stySearch   = lipgloss.NewStyle().Foreground(lipgloss.Color("227")).Bold(true)
	styUpdate   = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	styPrevTtl  = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true)
	styPrevLbl  = lipgloss.NewStyle().Foreground(lipgloss.Color("44")).Bold(true)
	styPrevBody = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	stySection  = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	styTab      = lipgloss.NewStyle().Foreground(lipgloss.Color("44"))
	styCursor   = lipgloss.NewStyle().Background(lipgloss.Color("25")).Foreground(lipgloss.Color("231")).Bold(true)
	styHelp     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styStatus   = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	styTabOn    = lipgloss.NewStyle().Background(lipgloss.Color("44")).Foreground(lipgloss.Color("16")).Bold(true)
	styTabOff   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)
