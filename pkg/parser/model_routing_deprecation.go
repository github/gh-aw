package parser

// LegacyModelRoutingWarning describes the authored main-agent declaration, not
// the merged engine config (which also contains synthesized router settings).
func LegacyModelRoutingWarning(frontmatter map[string]any) string {
	engine, ok := frontmatter["engine"].(map[string]any)
	if !ok {
		return ""
	}
	path := "engine.model-routing"
	if pi, ok := engine["pi"].(map[string]any); ok {
		engine = pi
		path = "engine.pi.model-routing"
	}
	prefix := path + " is deprecated. "
	switch routing := engine["model-routing"].(type) {
	case bool:
		if !routing {
			return prefix + "Move model-routing: false to the top level to opt out without pinning a model."
		}
	case map[string]any:
		var migration string
		if routing["goal"] == "cost-speed" {
			migration = "The cost-speed objective is also deprecated and has no direct replacement. It still selects for speed; choose a cost level deliberately: model: router, router/economy, router/balanced, or router/robust."
		} else {
			selector := "router"
			switch routing["mode"] {
			case "economy":
				selector = "router/economy"
			case "balanced":
				selector = "router/balanced"
			case "robust":
				selector = "router/robust"
			}
			migration = "Use model: " + selector + " for cost routing."
		}
		if _, ok := routing["allowed-models"]; ok {
			migration += " Preserve the effective shortlist in one top-level model-routing.allowed-models declaration."
		}
		return prefix + migration
	}
	return ""
}
