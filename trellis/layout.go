package trellis

import (
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/luiul/dashkit/loam"
	"github.com/mattn/go-runewidth"
)

// ColumnPolicy separates readable targets from the floors used in tight windows.
// Preferred is measured plain content, without cell padding. Weight controls only
// surplus after content fits. Higher ShrinkPriority yields space first and receives
// preferred content last. A zero Weight keeps an automatic column compact.
type ColumnPolicy struct {
	Minimum        int
	HardMinimum    int
	Preferred      int
	Weight         int
	ShrinkPriority int
}

// ContentWidth measures the plain label in the same display cells as bubbles/table.
// Styling and the selection marker must not become part of a content target.
func ContentWidth(label string) int {
	return runewidth.StringWidth(strings.ReplaceAll(ansi.Strip(label), loam.Sentinel, ""))
}

// Allocate fills viewport, including loam's two padding cells per column. Below
// the combined hard minimum it returns those floors and false: callers must show
// a too-narrow notice and allow the existing clipped view, not hide columns.
func Allocate(viewport int, policies []ColumnPolicy) ([]int, bool) {
	p := normalize(policies)
	widths := make([]int, len(p))
	budget := viewport - loam.CellPadding*len(p)
	for i, c := range p {
		widths[i] = c.HardMinimum
	}
	if budget < sum(widths) {
		return widths, false
	}
	for i, c := range p {
		widths[i] = c.Minimum
		if c.Weight == 0 {
			widths[i] = max(widths[i], c.Preferred)
		}
	}
	order := priorityOrder(p)
	if sum(widths) > budget {
		shrink(widths, p, order, sum(widths)-budget)
	}
	// Fund useful text before dividing blank space. Equal priorities get a fair
	// capped share of their remaining content deficits.
	remaining := budget - sum(widths)
	for start := 0; start < len(order); {
		end := start + 1
		for end < len(order) && p[order[end]].ShrinkPriority == p[order[start]].ShrinkPriority {
			end++
		}
		needs := make([]int, len(p))
		for _, i := range order[start:end] {
			needs[i] = max(p[i].Preferred-widths[i], 0)
		}
		// A useful higher-priority label can use a lower-priority text
		// column's normal target. For example, a long path must not keep
		// a model truncated when its eight-cell hard floor would let it fit.
		deficit := max(sum(needs)-remaining, 0)
		for n := len(order) - 1; n >= end && deficit > 0; n-- {
			i := order[n]
			if p[i].Weight == 0 {
				continue
			}
			take := min(deficit, widths[i]-p[i].HardMinimum)
			widths[i] -= take
			remaining += take
			deficit -= take
		}
		grant := min(remaining, sum(needs))
		shares := split(grant, needs)
		for i, share := range shares {
			widths[i] += share
		}
		remaining -= grant
		start = end
	}
	weights := make([]int, len(p))
	for i, c := range p {
		weights[i] = c.Weight
	}
	if sum(weights) == 0 && len(weights) > 0 {
		weights[len(weights)-1] = 1
	}
	for i, share := range split(remaining, weights) {
		widths[i] += share
	}
	return widths, true
}

// Preferences holds desired integer proportions, not the last projected widths.
// Its zero value is automatic. It lives in memory only. Capture only after a
// changed mouse motion, never after projecting a temporary narrow layout.
type Preferences struct {
	touched []bool
	weights []int
	fixed   []int
}

// Manual reports whether a changed drag has selected a manual layout.
func (p Preferences) Manual() bool { return len(p.weights) > 0 }

// Capture records both neighbors of border and the current stretch pool. Other
// compact fields stay compact. Narrowed compact fields keep their chosen target;
// deliberately widened compact fields join the proportional pool.
func (p *Preferences) Capture(widths []int, policies []ColumnPolicy, border int) {
	if len(widths) != len(policies) || border < 0 || border+1 >= len(widths) {
		return
	}
	policies = normalize(policies)
	if len(p.touched) != len(widths) {
		p.touched = make([]bool, len(widths))
		p.weights = make([]int, len(widths))
		p.fixed = make([]int, len(widths))
	}
	// Bubble Tea passes models by value. Do not mutate a previous model's
	// mask, or recapture an unrelated compact target from a clamped viewport.
	p.touched = append([]bool(nil), p.touched...)
	p.touched[border], p.touched[border+1] = true, true
	weights := make([]int, len(widths))
	fixed := make([]int, len(widths))
	for i, c := range policies {
		changed := i == border || i == border+1
		if c.Weight == 0 && p.touched[i] && !changed {
			if p.weights[i] > 0 {
				weights[i] = max(widths[i], 1)
			} else {
				fixed[i] = p.fixed[i]
			}
			continue
		}
		if c.Weight > 0 || (changed && widths[i] > max(c.Minimum, c.Preferred)) {
			weights[i] = max(widths[i], 1)
		} else if changed {
			fixed[i] = max(widths[i], c.HardMinimum)
		}
	}
	p.weights, p.fixed = weights, fixed
}

// Allocate reprojects the original desired weights onto the current flexible
// budget. Clamped columns leave the pool before the remainder is redistributed.
// Content changes cannot grow a manual stretch column behind the user's back.
func (p Preferences) Allocate(viewport int, policies []ColumnPolicy) ([]int, bool) {
	if !p.Manual() || len(p.weights) != len(policies) {
		return Allocate(viewport, policies)
	}
	policies = normalize(policies)
	widths := make([]int, len(policies))
	budget := viewport - loam.CellPadding*len(policies)
	for i, c := range policies {
		widths[i] = c.HardMinimum
	}
	if sum(widths) > budget {
		return widths, false
	}
	weights := append([]int(nil), p.weights...)
	for i, c := range policies {
		if weights[i] == 0 {
			widths[i] = max(c.Minimum, c.Preferred)
			if p.fixed[i] > 0 {
				widths[i] = max(p.fixed[i], c.HardMinimum)
			}
		}
	}
	if sum(widths) > budget {
		shrink(widths, policies, priorityOrder(policies), sum(widths)-budget)
	}
	remaining := budget
	for i, weight := range weights {
		if weight == 0 {
			remaining -= widths[i]
		}
	}
	for sum(weights) > 0 {
		shares := split(remaining, weights)
		clamped := false
		for i, weight := range weights {
			if weight > 0 && shares[i] < policies[i].HardMinimum {
				widths[i] = policies[i].HardMinimum
				remaining -= widths[i]
				weights[i] = 0
				clamped = true
			}
		}
		if clamped {
			continue
		}
		for i, weight := range weights {
			if weight > 0 {
				widths[i] = shares[i]
			}
		}
		remaining = 0
		break
	}
	// A compact-only table can still be dragged. Keep the exact viewport even
	// when the drag only narrowed compact targets and left no weighted pool.
	if remaining > 0 && len(widths) > 0 {
		widths[len(widths)-1] += remaining
	}
	return widths, true
}

func normalize(policies []ColumnPolicy) []ColumnPolicy {
	p := append([]ColumnPolicy(nil), policies...)
	for i := range p {
		p[i].HardMinimum = max(p[i].HardMinimum, 1)
		p[i].Minimum = max(p[i].Minimum, p[i].HardMinimum)
		p[i].Preferred = max(p[i].Preferred, p[i].Minimum)
		p[i].Weight = max(p[i].Weight, 0)
	}
	return p
}

// Lower priorities receive content first. Text always shrinks before compact
// fields, even if a compact field has the same numeric priority.
func priorityOrder(p []ColumnPolicy) []int {
	order := make([]int, len(p))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		return p[order[i]].ShrinkPriority < p[order[j]].ShrinkPriority
	})
	return order
}

func shrink(widths []int, p []ColumnPolicy, order []int, deficit int) {
	for _, text := range []bool{true, false} {
		for n := len(order) - 1; n >= 0 && deficit > 0; n-- {
			i := order[n]
			if (p[i].Weight > 0) != text {
				continue
			}
			take := min(deficit, widths[i]-p[i].HardMinimum)
			widths[i] -= take
			deficit -= take
		}
	}
}

// split uses largest remainders. Column order breaks ties, including odd budgets.
func split(budget int, weights []int) []int {
	out := make([]int, len(weights))
	total := int64(sum(weights))
	if budget <= 0 || total == 0 {
		return out
	}
	remainders := make([]int64, len(weights))
	order := make([]int, 0, len(weights))
	used := 0
	for i, weight := range weights {
		if weight <= 0 {
			continue
		}
		product := int64(budget) * int64(weight)
		out[i] = int(product / total)
		remainders[i] = product % total
		used += out[i]
		order = append(order, i)
	}
	sort.SliceStable(order, func(i, j int) bool { return remainders[order[i]] > remainders[order[j]] })
	for _, i := range order[:budget-used] {
		out[i]++
	}
	return out
}

func sum(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}
