package pages

import (
	"fmt"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

const codePreviewLimit = 48

type codeInput struct {
	in    textinput.Model
	limit int
}

func newCodeInput(placeholder string, charLimit int) codeInput {
	in := textinput.New()
	in.Placeholder = placeholder
	in.Prompt = "› "
	in.CharLimit = charLimit
	return codeInput{in: in, limit: codePreviewLimit}
}

func (c *codeInput) SetWidth(w int)             { c.in.SetWidth(w) }
func (c *codeInput) SetPlaceholder(text string) { c.in.Placeholder = text }
func (c *codeInput) Focus() tea.Cmd             { return c.in.Focus() }
func (c *codeInput) Blur()                      { c.in.Blur() }
func (c *codeInput) Value() string              { return c.in.Value() }
func (c *codeInput) SetValue(s string) {
	c.in.SetValue(s)
	c.syncMode()
}

func (c *codeInput) Reset() {
	c.in.Reset()
	c.syncMode()
}

func (c *codeInput) Update(msg tea.Msg) (codeInput, tea.Cmd) {
	c.syncMode()
	var cmd tea.Cmd
	c.in, cmd = c.in.Update(msg)
	c.syncMode()
	return *c, cmd
}

func (c *codeInput) View() string {
	value := c.in.Value()
	view := c.in.View()
	if len(value) <= c.limit {
		return view
	}
	preview := value
	if len(preview) > 12 {
		preview = preview[:8] + "…" + preview[len(preview)-4:]
	}
	return view + "\n" + StyleMuted.Render(fmt.Sprintf("pasted %d bytes · %s", len(value), preview))
}

func (c *codeInput) syncMode() {
	switch {
	case len(c.in.Value()) > c.limit:
		c.in.EchoMode = textinput.EchoNone
	default:
		c.in.EchoMode = textinput.EchoNormal
	}
}
