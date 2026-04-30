package modeltui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/singularity-ng/singularity-memory/go/internal/modelcatalog"
)

type App struct {
	client  Client
	catalog modelcatalog.Catalog
	err     error
	loading bool
	tab     int
	width   int
	height  int
}

type catalogMsg struct {
	catalog modelcatalog.Catalog
	err     error
}

var tabNames = []string{"Summary", "Sources", "Providers", "Models", "Conflicts"}

const (
	colorAccent = "99"
	colorActive = "135"
	colorMuted  = "240"
	colorError  = "196"
)

func New(serverURL string) App {
	return App{client: Client{BaseURL: serverURL}, loading: true}
}

func (a App) Init() tea.Cmd {
	return a.fetch(false)
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return a, tea.Quit
		case "tab", "right", "l":
			a.tab = (a.tab + 1) % len(tabNames)
		case "shift+tab", "left", "h":
			a.tab = (a.tab + len(tabNames) - 1) % len(tabNames)
		case "r":
			a.loading = true
			a.err = nil
			return a, a.fetch(true)
		}
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
	case catalogMsg:
		a.loading = false
		a.err = msg.err
		if msg.err == nil {
			a.catalog = msg.catalog
		}
	}
	return a, nil
}

func (a App) View() string {
	if a.width == 0 {
		a.width = 100
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorAccent)).Render("modelwalk")
	subtle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	var b strings.Builder
	b.WriteString(title)
	b.WriteString(" ")
	b.WriteString(subtle.Render("memory-backed model catalog"))
	b.WriteString("\n")
	b.WriteString(a.tabs())
	b.WriteString("\n\n")
	if a.loading {
		b.WriteString("syncing catalog...\n")
	} else if a.err != nil {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(colorError)).Render(a.err.Error()))
		b.WriteString("\n")
	} else {
		switch a.tab {
		case 0:
			b.WriteString(a.summary())
		case 1:
			b.WriteString(a.sources())
		case 2:
			b.WriteString(a.providers())
		case 3:
			b.WriteString(a.models())
		case 4:
			b.WriteString(a.conflicts())
		}
	}
	b.WriteString("\n")
	b.WriteString(subtle.Render("r refresh/sync  tab switch  q quit"))
	return b.String()
}

func (a App) fetch(sync bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		var catalog modelcatalog.Catalog
		var err error
		if sync {
			catalog, err = a.client.Sync(ctx)
		} else {
			catalog, err = a.client.Catalog(ctx)
		}
		return catalogMsg{catalog: catalog, err: err}
	}
}

func (a App) tabs() string {
	styles := []lipgloss.Style{
		lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)),
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorActive)),
	}
	parts := make([]string, 0, len(tabNames))
	for i, name := range tabNames {
		style := styles[0]
		if i == a.tab {
			style = styles[1]
		}
		parts = append(parts, style.Render(name))
	}
	return strings.Join(parts, "  ")
}

func (a App) summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "generated: %s\n", a.catalog.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "providers: %d\n", len(a.catalog.Providers))
	fmt.Fprintf(&b, "models:    %d\n\n", len(a.catalog.Models))
	for _, source := range a.catalog.Sources {
		status := "ok"
		if !source.OK {
			status = source.Error
		}
		fmt.Fprintf(&b, "%-10s providers=%-4d models=%-5d %s\n", source.Name, source.ProviderN, source.ModelCount, status)
	}
	return b.String()
}

func (a App) sources() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-22s %-7s %-9s %-10s %-30s %-10s %s\n", "source", "status", "models", "secret src", "secret ref", "secret", "endpoint")
	for _, source := range firstN(a.catalog.Sources, 32) {
		status := "ok"
		if !source.OK {
			status = "error"
		}
		key := "-"
		if source.AuthRef != "" {
			key = "missing"
			if source.AuthPresent {
				key = "present"
			}
		}
		secretRef := source.AuthRef
		if secretRef == "" {
			secretRef = "-"
		}
		secretSource := source.AuthSource
		if secretSource == "" {
			secretSource = "-"
		}
		endpoint := source.URL
		if endpoint == "" {
			endpoint = "-"
		}
		fmt.Fprintf(&b, "%-22s %-7s %-9d %-10s %-30s %-10s %s\n", clip(source.Name, 22), status, source.ModelCount, clip(secretSource, 10), clip(secretRef, 30), key, clip(endpoint, 72))
		if source.Error != "" {
			fmt.Fprintf(&b, "  %s\n", source.Error)
		}
	}
	return b.String()
}

func (a App) providers() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-18s %-22s %-24s %-24s %s\n", "provider", "source", "large", "small", "models")
	for _, p := range firstN(a.catalog.Providers, 28) {
		fmt.Fprintf(&b, "%-18s %-22s %-24s %-24s %d\n", clip(p.ID, 18), clip(p.Source, 22), clip(p.DefaultLargeModelID, 24), clip(p.DefaultSmallModelID, 24), p.ModelCount)
	}
	return b.String()
}

func (a App) models() string {
	models := append([]modelcatalog.Model(nil), a.catalog.Models...)
	sort.Slice(models, func(i, j int) bool {
		if models[i].SizeClass == models[j].SizeClass {
			return models[i].ProviderID+"/"+models[i].ID < models[j].ProviderID+"/"+models[j].ID
		}
		return sizeRank(models[i].SizeClass) < sizeRank(models[j].SizeClass)
	})
	var b strings.Builder
	fmt.Fprintf(&b, "%-16s %-34s %-12s %-22s %s\n", "provider", "model", "size", "family", "canon")
	for _, m := range firstN(models, 32) {
		fmt.Fprintf(&b, "%-16s %-34s %-12s %-22s %s\n", clip(m.ProviderID, 16), clip(m.ID, 34), clip(m.SizeClass, 12), clip(m.Family, 22), clip(m.CanonicalSlug, 40))
	}
	return b.String()
}

func (a App) conflicts() string {
	byCanon := map[string][]modelcatalog.Model{}
	for _, m := range a.catalog.Models {
		byCanon[m.CanonicalSlug] = append(byCanon[m.CanonicalSlug], m)
	}
	keys := make([]string, 0, len(byCanon))
	for key, models := range byCanon {
		if len(models) > 1 {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return "no duplicate canonical slugs in current catalog\n"
	}
	var b strings.Builder
	for _, key := range firstN(keys, 20) {
		fmt.Fprintf(&b, "%s\n", key)
		for _, m := range byCanon[key] {
			fmt.Fprintf(&b, "  %-14s %s\n", m.ProviderID, m.ID)
		}
	}
	return b.String()
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func firstN[T any](values []T, n int) []T {
	if len(values) <= n {
		return values
	}
	return values[:n]
}

func sizeRank(size string) int {
	switch size {
	case "tiny":
		return 0
	case "small":
		return 1
	case "standard":
		return 2
	case "large":
		return 3
	case "frontier":
		return 4
	default:
		return 5
	}
}
