package main

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/fang/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"golang.org/x/term"
)

// palette holds the dashboard's design tokens from web/src/styles/global.css,
// converted from oklch. Color is for the accent and status; the rest is ink.
type palette struct {
	accent, onAccent, grass, tomato, ink, ink2, ink3, ink4, line color.Color
}

func newPalette(c lipgloss.LightDarkFunc) palette {
	hex := lipgloss.Color
	return palette{
		accent:   hex("#3ecf72"),
		onAccent: c(hex("#fbfdfb"), hex("#08230f")),
		grass:    c(hex("#1f8a3f"), hex("#3ecf72")),
		tomato:   c(hex("#c9352b"), hex("#f0655a")),
		ink:      c(hex("#22222a"), hex("#f2f1ee")),
		ink2:     c(hex("#55555e"), hex("#b9bac0")),
		ink3:     c(hex("#7f7f87"), hex("#8b8d93")),
		ink4:     c(hex("#a4a4aa"), hex("#64666b")),
		line:     c(hex("#cfccc6"), hex("#45464b")),
	}
}

// styles are the lipgloss styles every screen shares.
type styles struct {
	palette
	dark                                                   bool
	tile, title, body, muted, faint, good, bad, code, card lipgloss.Style
}

func newStyles() styles {
	dark := lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	p := newPalette(lipgloss.LightDark(dark))
	s := lipgloss.NewStyle
	return styles{
		palette: p,
		dark:    dark,
		tile:    s().Bold(true).Foreground(p.onAccent).Background(p.accent).Padding(0, 1),
		title:   s().Bold(true).Foreground(p.ink),
		body:    s().Foreground(p.ink),
		muted:   s().Foreground(p.ink2),
		faint:   s().Foreground(p.ink3),
		good:    s().Foreground(p.grass),
		bad:     s().Foreground(p.tomato),
		code:    s().Foreground(p.grass),
		card:    s().Border(lipgloss.RoundedBorder()).BorderForeground(p.line).Padding(0, 2),
	}
}

var ui = newStyles()

func interactive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

func heading(text string) string {
	return ui.tile.Render("shed lab") + " " + ui.muted.Render(text)
}

// row is one key and value in a card.
type row struct{ key, value string }

// card renders a title and aligned rows in a bordered sheet.
func card(title string, rows ...row) string {
	width := 0
	for _, r := range rows {
		width = max(width, lipgloss.Width(r.key))
	}
	lines := []string{title, ""}
	for _, r := range rows {
		key := ui.faint.Width(width + 2).Render(r.key)
		lines = append(lines, key+r.value)
	}
	return ui.card.Render(strings.Join(lines, "\n"))
}

// hints renders suggested commands, aligned with what they do.
func hints(pairs ...[2]string) string {
	width := 0
	for _, p := range pairs {
		width = max(width, lipgloss.Width(p[0]))
	}
	var lines []string
	for _, p := range pairs {
		lines = append(lines, "  "+ui.code.Width(width+2).Render(p[0])+ui.faint.Render(p[1]))
	}
	return strings.Join(lines, "\n")
}

// step is one unit of a long operation.
type step struct {
	label string
	run   func(ctx context.Context, p *progress) error
}

// progress lets a running step describe what it is doing right now.
type progress struct{ set func(string) }

type stepDoneMsg struct{ err error }
type stepDetailMsg string

type stepsModel struct {
	ctx      context.Context
	cancel   context.CancelFunc
	steps    []step
	current  int
	spin     spinner.Model
	detail   string
	began    time.Time
	took     []time.Duration
	err      error
	finished bool
	send     func(tea.Msg)
}

func (m stepsModel) Init() tea.Cmd { return tea.Batch(m.spin.Tick, m.runCurrent()) }

func (m stepsModel) runCurrent() tea.Cmd {
	s := m.steps[m.current]
	return func() tea.Msg {
		p := &progress{set: func(d string) { m.send(stepDetailMsg(d)) }}
		return stepDoneMsg{s.run(m.ctx, p)}
	}
}

func (m stepsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.cancel()
			m.err = context.Canceled
			m.finished = true
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case stepDetailMsg:
		m.detail = string(msg)
	case stepDoneMsg:
		if msg.err != nil {
			m.err = msg.err
			m.finished = true
			return m, tea.Quit
		}
		m.took = append(m.took, time.Since(m.began).Round(100*time.Millisecond))
		m.current++
		m.detail = ""
		m.began = time.Now()
		if m.current == len(m.steps) {
			m.finished = true
			return m, tea.Quit
		}
		return m, m.runCurrent()
	}
	return m, nil
}

func (m stepsModel) View() tea.View {
	width := 0
	for _, s := range m.steps {
		width = max(width, lipgloss.Width(s.label))
	}
	var b strings.Builder
	for i, s := range m.steps {
		status, label, extra := "", ui.faint.Render(s.label), ""
		switch {
		case i < m.current:
			status, label = ui.good.Render("done"), ui.body.Render(s.label)
			extra = ui.faint.Render(m.took[i].String())
		case i == m.current && m.err != nil:
			status, label = ui.bad.Render("failed"), ui.body.Render(s.label)
		case i == m.current && !m.finished:
			status, label = m.spin.View(), ui.body.Render(s.label)
			extra = ui.faint.Render(time.Since(m.began).Round(time.Second).String())
			if m.detail != "" {
				extra += "  " + ui.muted.Render(m.detail)
			}
		}
		pad := strings.Repeat(" ", width-lipgloss.Width(s.label)+2)
		fmt.Fprintf(&b, "  %s%s%s%s\n", lipgloss.NewStyle().Width(8).Render(status), label, pad, extra)
	}
	return tea.NewView(b.String())
}

// runSteps runs steps in order, with a live checklist on a terminal and plain
// lines otherwise, and stops at the first failure.
func runSteps(ctx context.Context, steps []step) error {
	if !interactive() {
		return runStepsPlain(ctx, steps)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	model := stepsModel{
		ctx: ctx, cancel: cancel, steps: steps, began: time.Now(),
		spin: spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(lipgloss.NewStyle().Foreground(ui.accent))),
	}
	var program *tea.Program
	model.send = func(msg tea.Msg) { program.Send(msg) }
	program = tea.NewProgram(model)
	final, err := program.Run()
	if err != nil {
		return err
	}
	return final.(stepsModel).err
}

func runStepsPlain(ctx context.Context, steps []step) error {
	for _, s := range steps {
		began := time.Now()
		fmt.Println(s.label + "...")
		if err := s.run(ctx, &progress{set: func(d string) {
			if !strings.Contains(d, "%") {
				fmt.Println("  " + d)
			}
		}}); err != nil {
			fmt.Printf("%s: failed after %s\n", s.label, time.Since(began).Round(time.Second))
			return err
		}
		fmt.Printf("%s: done in %s\n", s.label, time.Since(began).Round(100*time.Millisecond))
	}
	return nil
}

// fangColors themes help and error output.
func fangColors(c lipgloss.LightDarkFunc) fang.ColorScheme {
	p := newPalette(c)
	return fang.ColorScheme{
		Base: p.ink, Title: p.grass, Description: p.ink2, Codeblock: c(lipgloss.Color("#efece6"), lipgloss.Color("#1b1c20")),
		Program: p.grass, DimmedArgument: p.ink4, Comment: p.ink3, Flag: p.grass, FlagDefault: p.ink3,
		Command: p.ink, QuotedString: p.ink2, Argument: p.ink2, Help: p.ink3, Dash: p.ink3,
		ErrorHeader: [2]color.Color{lipgloss.Color("#fff7f5"), p.tomato}, ErrorDetails: p.ink2,
	}
}

// formTheme themes prompts to match. It keeps the background detected at
// startup, because huh's own detection can disagree inside a multiplexer.
func formTheme() huh.Theme {
	return huh.ThemeFunc(func(bool) *huh.Styles {
		p := ui.palette
		t := huh.ThemeBase(ui.dark)
		f := &t.Focused
		f.Base = f.Base.BorderForeground(p.accent)
		f.Title = f.Title.Foreground(p.ink).Bold(true)
		f.Description = f.Description.Foreground(p.ink3)
		f.SelectSelector = f.SelectSelector.Foreground(p.accent)
		f.SelectedOption = f.SelectedOption.Foreground(p.grass)
		f.UnselectedOption = f.UnselectedOption.Foreground(p.ink2)
		f.FocusedButton = f.FocusedButton.Foreground(p.onAccent).Background(p.accent)
		f.BlurredButton = f.BlurredButton.Foreground(p.ink2)
		f.TextInput.Cursor = f.TextInput.Cursor.Foreground(p.accent)
		f.TextInput.Prompt = f.TextInput.Prompt.Foreground(p.accent)
		f.ErrorMessage = f.ErrorMessage.Foreground(p.tomato)
		f.ErrorIndicator = f.ErrorIndicator.Foreground(p.tomato)
		t.Blurred = *f
		t.Blurred.Base = t.Blurred.Base.BorderForeground(p.line)
		return t
	})
}

func runForm(fields ...huh.Field) error {
	if !interactive() {
		return errors.New("this needs a terminal; pass the value as an argument or flag")
	}
	return huh.NewForm(huh.NewGroup(fields...)).WithTheme(formTheme()).WithShowHelp(false).Run()
}
