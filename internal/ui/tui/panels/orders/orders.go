package orders

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/cli/render"
	"tofu/internal/ui/tui/input/pointer"
	"tofu/internal/ui/tui/look/look"
)

type Kind string

const (
	Spawned  Kind = "spawned"
	Reviewed Kind = "reviewed"
	Reopened Kind = "reopened"
	Accepted Kind = "accepted"
	Messaged Kind = "messaged"
	Answered Kind = "answered"
	Stopped  Kind = "stopped"
	Killed   Kind = "killed"
	Released Kind = "released"
	Granted  Kind = "granted"
	Revoked  Kind = "revoked"
	Replaced Kind = "replaced"
	BackedUp Kind = "backed up"
)

const (
	title         = "Orders"
	oneOrder      = "1 order"
	emptyTitle    = "no order has gone to a sub-agent in this session"
	emptyBody     = "what the orchestrator tells its sub-agents shows here, one line each, newest last: a spawn with its ticket, a hand-back reaching review, a reopen with its reason, an acceptance, a stop or a release, and a message sent to a child. What a sub-agent does with it is on the sub-agents screen."
	unreplayed    = " came back from disk without their history: a review, reopen or acceptance before this resume is not shown."
	clock         = "15:04:05"
	columnGap     = "  "
	padding       = 2
	trackGap      = 1
	headRows      = 3
	clockMinWidth = 40
	pageRows      = 10
)

type Order struct {
	At    time.Time
	Agent string
	Kind  Kind
	Round int
	Text  string
}

type Model struct {
	orders        []Order
	unreplayed    []string
	width, height int
	behindNewest  int
	drawn         drawn
}

type drawn struct {
	orders, unreplayed, width int
	lines                     []string
}

type message struct {
	To     string `json:"to"`
	Text   string `json:"text"`
	Do     string `json:"do"`
	Answer string `json:"answer"`
	Stop   bool   `json:"stop"`
}

func Message(args json.RawMessage) (Order, bool) {
	var sent message
	if json.Unmarshal(args, &sent) != nil || sent.To == "" {
		return Order{}, false
	}
	if sent.Stop {
		sent.Do = "stop"
	}
	order := Order{Agent: sent.To, Text: sent.Text}
	switch sent.Do {
	case "stop":
		order.Kind = Stopped
	case "kill":
		order.Kind = Killed
	case "release":
		order.Kind = Released
	case "grant":
		order.Kind = Granted
	case "revoke":
		order.Kind = Revoked
	case "replace":
		order.Kind = Replaced
	case "backup":
		order.Kind = BackedUp
	case "":
		order.Kind = Messaged
		if sent.Answer != "" {
			order.Kind, order.Text = Answered, strings.TrimSuffix(sent.Answer+": "+sent.Text, ": ")
		}
	default:
		return Order{}, false
	}
	return order, true
}

func (m *Model) SetSize(width, height int) { m.width, m.height = width, height }

func (m *Model) Add(order Order) { m.orders = append(m.orders, order) }

func (m *Model) Unreplayed(agent string) {
	if !slices.Contains(m.unreplayed, agent) {
		m.unreplayed = append(m.unreplayed, agent)
	}
}

func (m *Model) Wheel(delta int) { m.SetScroll(m.behindNewest - delta) }

func (m *Model) SetScroll(behindNewest int) {
	m.behindNewest = max(0, min(behindNewest, len(m.lines())-m.visible()))
}

func (m *Model) Key(key string) bool {
	switch key {
	case "up":
		m.Wheel(-1)
	case "down":
		m.Wheel(1)
	case "pgup":
		m.Wheel(-pageRows)
	case "pgdown":
		m.Wheel(pageRows)
	case "home":
		m.SetScroll(len(m.lines()))
	case "end":
		m.SetScroll(0)
	default:
		return false
	}
	return true
}

func (m *Model) Track() pointer.Track {
	total, visible := len(m.lines()), m.visible()
	return pointer.Track{Total: total, Visible: visible, FromTop: max(0, total-visible-m.behindNewest)}
}

func (m *Model) View() string {
	if len(m.orders) == 0 && len(m.unreplayed) == 0 {
		return look.Surface(m.width, m.height, "", padding, "\n"+look.Title(emptyTitle)+"\n\n"+look.Muted(emptyBody))
	}
	lines := m.lines()
	end := max(0, len(lines)-m.behindNewest)
	shown := lines[max(0, end-m.visible()):end]
	summary := strconv.Itoa(len(m.orders)) + " orders"
	if len(m.orders) == 1 {
		summary = oneOrder
	}
	return look.Surface(m.width, m.height, "", padding, "\n"+look.PaneTitle(title, true)+"  "+look.Faint(summary)+"\n\n"+strings.Join(shown, "\n"))
}

func (m *Model) visible() int { return max(1, m.height-headRows) }

func (m *Model) lines() []string {
	width := m.width - 2*padding - trackGap
	if m.drawn.width == width && m.drawn.orders == len(m.orders) && m.drawn.unreplayed == len(m.unreplayed) {
		return m.drawn.lines
	}
	var lines []string
	if len(m.unreplayed) > 0 {
		for _, line := range render.Wrap(strings.Join(m.unreplayed, ", ")+unreplayed, width) {
			lines = append(lines, look.Faint(line))
		}
		lines = append(lines, "")
	}
	stamped, indent := width >= clockMinWidth, columnGap
	if stamped {
		indent = strings.Repeat(" ", len(clock)) + columnGap
	}
	kindWidth := 0
	for _, order := range m.orders {
		kindWidth = max(kindWidth, render.Cells(string(order.Kind)))
	}
	for _, order := range m.orders {
		head := look.Style(colour(order.Kind)).Render(render.Pad(string(order.Kind), kindWidth)) + columnGap + look.AgentRef(order.Agent)
		if order.Round > 1 {
			head += look.Faint(" · round " + strconv.Itoa(order.Round))
		}
		if stamped {
			head = look.Faint(order.At.Format(clock)) + columnGap + head
		}
		text := strings.TrimSpace(order.Text)
		switch {
		case text == "":
			lines = append(lines, render.Fit(head, width))
		case !strings.Contains(text, "\n") && render.Cells(head+columnGap+text) <= width:
			lines = append(lines, head+columnGap+look.Muted(text))
		default:
			lines = append(lines, render.Fit(head, width))
			for _, line := range render.Wrap(text, width-len(indent)) {
				lines = append(lines, indent+look.Muted(line))
			}
		}
	}
	m.drawn = drawn{orders: len(m.orders), unreplayed: len(m.unreplayed), width: width, lines: lines}
	return lines
}

func colour(kind Kind) look.Color {
	switch kind {
	case Spawned, Messaged, Answered, Granted, Revoked, Replaced, BackedUp:
		return look.Text
	case Reviewed:
		return look.Blue
	case Reopened, Stopped:
		return look.Amber
	case Accepted:
		return look.Mint
	case Killed, Released:
		return look.Red
	}
	panic("orders: unknown order kind " + string(kind))
}
