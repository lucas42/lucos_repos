package conventions

import "fmt"

// checkInLucosConfigy is the check function for the in-lucos-configy
// convention, extracted so it can be called directly in tests.
func checkInLucosConfigy(repo RepoContext) ConventionResult {
	switch repo.Type {
	case RepoTypeSystem, RepoTypeComponent, RepoTypeScript:
		return ConventionResult{
			Convention: "in-lucos-configy",
			Pass:       true,
			Detail:     fmt.Sprintf("Repository is listed in lucos_configy as type %q", repo.Type),
		}
	case RepoTypeDuplicate:
		return ConventionResult{
			Convention: "in-lucos-configy",
			Pass:       false,
			Detail:     "Repository appears under more than one type in lucos_configy",
		}
	default:
		// RepoTypeUnconfigured (or any future unknown type)
		return ConventionResult{
			Convention: "in-lucos-configy",
			Pass:       false,
			Detail:     "Repository is not listed in lucos_configy",
		}
	}
}

func init() {
	// in-lucos-configy: every repo must appear in exactly one category in
	// lucos_configy (systems, components, or scripts).
	Register(Convention{
		ID: "in-lucos-configy",
		Description: "Repository appears in lucos_configy under exactly one of the " +
			"following types: system, component, or script",
		Rationale: "lucos_configy is the central configuration store that powers " +
			"monitoring, deployments, and other infrastructure tooling. A repo that " +
			"is not listed in configy is invisible to these systems, which can lead " +
			"to missed alerts, failed deploys, or incomplete inventory. A repo listed " +
			"under more than one type is a configuration error that can cause " +
			"unpredictable behaviour in tooling that relies on a single authoritative type.",
		Guidance: "Add the repository to lucos_configy by editing the appropriate " +
			"YAML file in the lucos_configy repo:\n\n" +
			"- **system** (`config/systems.yaml`): a service that is deployed and " +
			"runs continuously (e.g. an API, a web app, a worker). Most lucos repos " +
			"are systems.\n" +
			"- **component** (`config/components.yaml`): a shared library or " +
			"reusable piece of infrastructure that is not deployed independently " +
			"(e.g. a shared npm package, a base Docker image).\n" +
			"- **script** (`config/scripts.yaml`): a tool or script designed to run " +
			"locally rather than being deployed to a server (e.g. a CLI tool, a " +
			"migration script).\n\n" +
			"Each entry needs at minimum an `id` field matching the repository name " +
			"(without the `lucas42/` prefix). If the repo is already listed under " +
			"more than one type, remove the duplicate entries so it appears under " +
			"exactly one.",
		Check: checkInLucosConfigy,
	})
}
