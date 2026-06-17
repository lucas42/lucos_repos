package conventions

import (
	"fmt"
	"sort"
	"strings"
)

// RenderCatalogue renders every registered convention as a Markdown catalogue.
//
// This is the canonical, generated documentation of the enforced conventions —
// the single source of truth. Human-readable convention docs (e.g. the
// lucos_claude_config reference docs) MUST link to this catalogue for enforced
// rules rather than paraphrasing them: a hand-written paraphrase can silently
// drift from the enforcement, which is exactly the failure this catalogue exists
// to prevent. See lucos_repos ADR-0007.
//
// The committed copy at docs/conventions.md is kept in step with this function by
// TestConventionCatalogueIsCurrent. Regenerate it with:
//
//	go run ./src conventions > docs/conventions.md
func RenderCatalogue() string {
	convs := All()
	sort.Slice(convs, func(i, j int) bool { return convs[i].ID < convs[j].ID })

	var b strings.Builder
	b.WriteString("# Convention catalogue\n\n")
	b.WriteString("**This file is generated — do not edit by hand.** ")
	b.WriteString("It is rendered from the `Convention` definitions in `conventions/*.go`, ")
	b.WriteString("which are the authoritative, enforced source of truth.\n\n")
	b.WriteString("Regenerate with `go run ./src conventions > docs/conventions.md`. ")
	b.WriteString("`TestConventionCatalogueIsCurrent` fails the build if this file drifts from the source.\n\n")
	b.WriteString("Documentation elsewhere (e.g. the `lucos_claude_config` reference docs) should **link to this catalogue ")
	b.WriteString("for enforced rules rather than paraphrasing them** — see lucos_repos ADR-0007 for why.\n\n")
	b.WriteString(fmt.Sprintf("There are **%d** registered conventions.\n", len(convs)))

	for _, c := range convs {
		b.WriteString("\n---\n\n")
		b.WriteString(fmt.Sprintf("## `%s`\n\n", c.ID))
		b.WriteString(c.Description)
		b.WriteString("\n\n")

		b.WriteString(fmt.Sprintf("- **Applies to:** %s\n", appliesToText(c)))
		if c.ScheduledOnly {
			b.WriteString("- **Scheduled sweeps only** (skipped during PR audits)\n")
		}
		if len(c.ExcludeRepos) > 0 {
			b.WriteString(fmt.Sprintf("- **Excluded repos:** %s\n", strings.Join(c.ExcludeRepos, ", ")))
		}

		b.WriteString("\n**Why this matters**\n\n")
		b.WriteString(c.Rationale)
		b.WriteString("\n\n**Suggested fix**\n\n")
		b.WriteString(c.Guidance)
		b.WriteString("\n")
	}

	return b.String()
}

// appliesToText renders a convention's AppliesTo set as human-readable prose.
// An empty AppliesTo means the convention applies to every repo type.
func appliesToText(c Convention) string {
	if len(c.AppliesTo) == 0 {
		return "all repo types"
	}
	parts := make([]string, len(c.AppliesTo))
	for i, t := range c.AppliesTo {
		parts[i] = string(t)
	}
	return strings.Join(parts, ", ")
}
