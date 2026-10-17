package appctx

import (
	"sort"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/collectors"
	"github.com/niclasedge/git-planner-go/internal/md"
)

const builtinSkillsTok = 1700

// BuildData maps collector cards into the browser DATA object (mirrors build_data).
// view is "overview" or "project".
func BuildData(context map[string]any, view string) map[string]any {
	ctx := aMap(dyn(context))
	cards := aMap(ctx["cards"])

	av := func(key string) map[string]any {
		c := aMap(cards[key])
		if c != nil && aBool(c["available"]) {
			return c
		}
		return map[string]any{}
	}

	g, p, kn, tok := av("global"), av("project"), av("knowledge"), av("tokens")
	gitc := aMap(cards["git"])
	if gitc == nil {
		gitc = map[string]any{}
	}
	costc := aMap(cards["cost"])
	if costc == nil {
		costc = map[string]any{}
	}
	reportsc := aMap(cards["reports"])
	if reportsc == nil {
		reportsc = map[string]any{}
	}

	// --- Skills ---
	skillsCard := aMap(g["skills"])
	var skillItems []any
	if it, ok := skillsCard["items"].([]any); ok {
		skillItems = it
	} else {
		for _, n := range aArr(skillsCard["names"]) {
			skillItems = append(skillItems, map[string]any{"name": n})
		}
	}
	skills := make([]map[string]any, 0, len(skillItems))
	for _, raw := range skillItems {
		s := aMap(raw)
		skills = append(skills, map[string]any{
			"name":       s["name"],
			"cat":        "",
			"desc":       aStr(s["description"]),
			"tokens":     aNum(s["tokens"]),
			"metaTokens": aNum(s["meta_tokens"]),
			"size":       aNum(s["size_bytes"]),
			"has_md":     aBool(s["has_md"]),
			"scripts":    orEmptyArr(s["scripts"]),
		})
	}
	skillsTok := sumField(skills, "tokens")

	// --- Agents ---
	var agentItems []any
	if it, ok := g["agent_items"].([]any); ok {
		agentItems = it
	} else {
		for _, n := range aArr(g["agents"]) {
			agentItems = append(agentItems, map[string]any{"name": n})
		}
	}
	agents := make([]map[string]any, 0, len(agentItems))
	for _, raw := range agentItems {
		a := aMap(raw)
		agents = append(agents, map[string]any{
			"name":       a["name"],
			"role":       aStr(a["description"]),
			"tools":      []any{},
			"tokens":     aNum(a["tokens"]),
			"metaTokens": aNum(a["meta_tokens"]),
			"size":       aNum(a["size_bytes"]),
		})
	}
	agentsTok := sumField(agents, "tokens")

	// --- Hooks ---
	settings := aMap(g["settings"])
	hookEvents := aMap(settings["hook_events"])
	hookDetail := aMap(settings["hook_detail"])
	hookNames := sortedKeys(hookEvents)
	hooks := make([]map[string]any, 0, len(hookNames))
	for _, name := range hookNames {
		entries := orEmptyArr(hookDetail[name])
		hooks = append(hooks, map[string]any{"name": name, "count": hookEvents[name], "entries": entries})
	}

	// --- Project knowledge ---
	pknow := []map[string]any{}
	for _, raw := range aArr(p["knowledge_index"]) {
		e := aMap(raw)
		path := aStr(e["path"])
		typ := ""
		if path != "" {
			parts := strings.Split(path, ".")
			typ = parts[len(parts)-1]
		}
		pknow = append(pknow, map[string]any{"name": e["title"], "type": typ, "desc": nc(e["desc"], ""), "path": nc(e["path"], "")})
	}

	// --- Project CLAUDE.md ---
	psections := []map[string]any{}
	for _, raw := range aArr(p["claude_md_headers"]) {
		psections = append(psections, map[string]any{"name": raw, "type": "section", "desc": ""})
	}
	pdoc := map[string]any{
		"kind":   "claude-project",
		"tokens": aNum(p["claude_md_tokens"]),
		"size":   aNum(p["claude_md_size"]),
	}
	psectionsEff := psections
	if len(psectionsEff) == 0 && (aBool(pdoc["tokens"]) || aBool(pdoc["size"])) {
		psectionsEff = []map[string]any{{"name": "(ganze Datei)", "type": "section", "desc": ""}}
	}

	// --- Global CLAUDE.md ---
	gmd := aMap(g["claude_md"])
	gsections := []map[string]any{}
	for _, raw := range aArr(gmd["headers"]) {
		gsections = append(gsections, map[string]any{"name": raw, "type": "section", "desc": ""})
	}
	gdoc := map[string]any{
		"kind":    "claude-global",
		"tokens":  aNum(gmd["tokens"]),
		"size":    aNum(gmd["size_bytes"]),
		"managed": aBool(gmd["managed_block"]),
	}
	gsectionsEff := gsections
	if len(gsectionsEff) == 0 && (aBool(gdoc["tokens"]) || aBool(gdoc["size"])) {
		gsectionsEff = []map[string]any{{"name": "(ganze Datei)", "type": "section", "desc": ""}}
	}

	// --- Knowledge ---
	decisions := []map[string]any{}
	for _, raw := range aArr(kn["decisions"]) {
		d := aMap(raw)
		decisions = append(decisions, map[string]any{
			"name":   aStr(d["id"]) + " — " + aStr(d["title"]),
			"id":     d["id"],
			"status": aStr(d["status"]),
			"ctx":    "",
			"dec":    "",
			"body":   aStr(d["body"]),
			"date":   md.FmtMtime(numOr0(d["mtime"])),
		})
	}
	memory := []map[string]any{}
	for _, raw := range aArr(kn["memory"]) {
		if s, ok := raw.(string); ok {
			memory = append(memory, map[string]any{"name": s, "desc": "", "date": ""})
		} else {
			m := aMap(raw)
			memory = append(memory, map[string]any{"name": aStr(m["name"]), "desc": "", "date": md.FmtMtime(numOr0(m["mtime"]))})
		}
	}
	lessons := []map[string]any{}
	for _, raw := range aArr(kn["lektionen"]) {
		if s, ok := raw.(string); ok {
			lessons = append(lessons, map[string]any{"name": s, "desc": "", "date": ""})
		} else {
			x := aMap(raw)
			lessons = append(lessons, map[string]any{"name": aStr(x["name"]), "desc": "", "date": md.FmtMtime(numOr0(x["mtime"]))})
		}
	}
	changelog := []map[string]any{}
	for _, raw := range aArr(kn["changelog"]) {
		if s, ok := raw.(string); ok {
			changelog = append(changelog, map[string]any{"name": s, "body": "", "type": "changelog", "desc": ""})
		} else {
			c := aMap(raw)
			body := nc(c["body"], "")
			descParts := []string{}
			for _, ln := range strings.Split(body, "\n") {
				if ln != "" {
					descParts = append(descParts, ln)
				}
				if len(descParts) >= 2 {
					break
				}
			}
			changelog = append(changelog, map[string]any{
				"name": c["heading"], "body": body, "type": "changelog",
				"desc": strings.Join(descParts, " · "),
			})
		}
	}

	// --- Sessions ---
	var last map[string]any
	if lm, ok := tok["last_session"].(map[string]any); ok {
		last = lm
	}
	sessSrc := aArr(tok["sessions"])
	if sessSrc == nil && last != nil {
		sessSrc = []any{last}
	}
	sessions := []map[string]any{}
	for _, raw := range sessSrc {
		s := aMap(raw)
		if s == nil {
			continue
		}
		total := s["total"]
		var totalVal float64
		if total != nil {
			totalVal = aNum(total)
		} else {
			totalVal = aNum(s["input"]) + aNum(s["output"]) + aNum(s["cache_read"]) + aNum(s["cache_creation"])
		}
		sessions = append(sessions, map[string]any{
			"name":         s["session_id"],
			"date":         aStr(s["date"]),
			"turns":        aNum(s["turns"]),
			"input":        aNum(s["input"]),
			"output":       aNum(s["output"]),
			"cc":           aNum(s["cache_creation"]),
			"cr":           aNum(s["cache_read"]),
			"total":        totalVal,
			"cost":         aNum(s["cost"]),
			"error_count":  aNum(s["error_count"]),
			"errors":       orEmptyArr(s["errors"]),
			"repeat_count": aNum(s["repeat_count"]),
			"repeats":      orEmptyArr(s["repeats"]),
		})
	}
	sessionsTok := sumField(sessions, "total")
	sessionsCost := sumField(sessions, "cost")
	week := aMap(tok["week"])

	// --- Kontext view (CCS-034C) ---
	pluginSkills := aArr(ctx["plugin_skills"])
	pluginAgents := aArr(ctx["plugin_agents"])
	projAgents := aArr(ctx["project_agents"])

	memCard := aMap(cards["memory_md"])
	memMdAvail := aBool(memCard["available"])
	memMdTok := aNum(memCard["tokens"])

	memoryItems := []map[string]any{}
	if aBool(gdoc["tokens"]) {
		memoryItems = append(memoryItems, map[string]any{
			"name": "CLAUDE.md (global)", "tokens": collectors.CtxTok(int(aNum(gdoc["tokens"]))),
			"desc": "~/.claude/CLAUDE.md", "read": "claude-global",
		})
	}
	if aBool(pdoc["tokens"]) {
		repoName := nc(aMap(cards["project"])["repo"], "Projekt")
		memoryItems = append(memoryItems, map[string]any{
			"name": "CLAUDE.md (Projekt)", "tokens": collectors.CtxTok(int(aNum(pdoc["tokens"]))),
			"desc": repoName + "/CLAUDE.md", "read": "claude-project",
		})
	}
	if memMdAvail && memMdTok != 0 {
		memPath := aStr(memCard["path"])
		claudeDir := collectors.Home() + "/.claude"
		item := map[string]any{"name": "MEMORY.md", "tokens": collectors.CtxTok(int(memMdTok)), "desc": memPath}
		if strings.HasPrefix(memPath, claudeDir) && memPath != "" {
			item["read"] = "homefile"
			item["readPath"] = memPath
		}
		memoryItems = append(memoryItems, item)
	}
	memoryTok := sumField(memoryItems, "tokens")

	// Agents context (Project → User → Plugin)
	agentItemsCtx := []map[string]any{}
	for _, raw := range projAgents {
		a := aMap(raw)
		agentItemsCtx = append(agentItemsCtx, map[string]any{
			"name": a["name"], "tokens": aNum(a["meta_tokens"]), "desc": a["description"],
			"group": a["group"], "read": a["read"], "readPath": a["readPath"],
		})
	}
	for _, a := range agents {
		agentItemsCtx = append(agentItemsCtx, map[string]any{
			"name": aStr(a["name"]), "tokens": collectors.CtxTok(int(aNum(a["metaTokens"]))),
			"desc": a["role"], "group": "User", "read": "agent",
		})
	}
	for _, raw := range pluginAgents {
		a := aMap(raw)
		agentItemsCtx = append(agentItemsCtx, map[string]any{
			"name": a["name"], "tokens": aNum(a["meta_tokens"]), "desc": a["description"],
			"group": a["group"], "read": a["read"], "readPath": a["readPath"],
		})
	}
	agentsCtxTok := sumField(agentItemsCtx, "tokens")

	// Skills context (User → Plugin → Built-in)
	skillItemsCtx := []map[string]any{}
	for _, s := range skills {
		skillItemsCtx = append(skillItemsCtx, map[string]any{
			"name": aStr(s["name"]), "tokens": collectors.CtxTok(int(aNum(s["metaTokens"]))),
			"desc": s["desc"], "group": "User", "read": "skill",
		})
	}
	for _, raw := range pluginSkills {
		s := aMap(raw)
		skillItemsCtx = append(skillItemsCtx, map[string]any{
			"name": s["name"], "tokens": aNum(s["meta_tokens"]), "desc": s["description"],
			"group": s["group"], "read": s["read"], "readPath": s["readPath"],
		})
	}
	skillItemsCtx = append(skillItemsCtx, map[string]any{
		"name": "(Built-in ≈)", "tokens": float64(builtinSkillsTok),
		"desc": "run / browser / verify-ui u.a. — nicht lesbar", "group": "Built-in",
	})
	skillsCtxTok := sumField(skillItemsCtx, "tokens")

	contextCategories := []map[string]any{
		{"key": "system_prompt", "label": "System prompt", "tokens": float64(collectors.SysPromptTok), "fixed": true, "items": []any{}},
		{"key": "system_tools", "label": "System tools", "tokens": float64(collectors.SysToolsTok), "fixed": true, "items": []any{}},
		{"key": "agents", "label": "Custom agents", "tokens": agentsCtxTok, "items": agentItemsCtx},
		{"key": "skills", "label": "Skills", "tokens": skillsCtxTok, "items": skillItemsCtx},
		{"key": "memory", "label": "Memory files", "tokens": memoryTok, "items": memoryItems},
		{"key": "hooks", "label": "Hook-Injektion", "tokens": float64(0), "live": true, "items": []any{}},
	}
	contextMeasuredTotal := 0.0
	for _, c := range contextCategories {
		if c["live"] == true {
			continue
		}
		contextMeasuredTotal += aNum(c["tokens"])
	}

	coll := map[string]any{
		"context": map[string]any{
			"title": "Kontext", "scope": "session", "type": "context", "accent": "m",
			"window": collectors.CtxWindow, "categories": contextCategories, "measured_total": contextMeasuredTotal,
		},
		"skills":    map[string]any{"title": "Skills", "scope": "global", "type": "skill", "accent": "g", "items": skills},
		"agents":    map[string]any{"title": "Agents", "scope": "global", "type": "agent", "accent": "c", "items": agents},
		"hooks":     map[string]any{"title": "Hooks", "scope": "global", "type": "hook", "accent": "", "items": hooks},
		"gclaude":   map[string]any{"title": "CLAUDE.md", "scope": "global", "type": "claude", "accent": "c", "items": gsectionsEff, "doc": gdoc},
		"pknow":     map[string]any{"title": "knowledge/", "scope": "projekt", "type": "know", "accent": "c", "items": pknow},
		"psections": map[string]any{"title": "CLAUDE.md", "scope": "projekt", "type": "claude", "accent": "c", "items": psectionsEff, "doc": pdoc},
		"decisions": map[string]any{"title": "Decisions", "scope": "wissen", "type": "decision", "accent": "g", "items": decisions},
		"memory":    map[string]any{"title": "Memory", "scope": "wissen", "type": "memory", "accent": "", "items": memory},
		"lessons":   map[string]any{"title": "Lektionen", "scope": "wissen", "type": "lesson", "accent": "a", "items": lessons},
		"changelog": map[string]any{"title": "CHANGELOG", "scope": "wissen", "type": "changelog", "accent": "", "items": changelog},
		"sessions":  map[string]any{"title": "Sessions", "scope": "usage", "type": "session", "accent": "m", "items": sessions},
	}

	// --- Reports (demo-axk.4): entry + gap counts for the nav badge ---
	//
	// Der Abstieg ueber children ist nicht optional (gkp-8a0): seit gkp-1lp ist
	// der Aufgabenbaum beliebig tief, und diese Schleife las nur die erste
	// Ebene. Eine Luecke unter einer Unteraufgabe faerbte die Navigation also
	// nicht rot, und ein Beleg eine Ebene tiefer zaehlte gar nicht mit — der
	// Badge meldete "0" fuer eine Karte voller Belege. Dieselbe Fehlerklasse
	// wie bei knownBeadIDs, Other, ReadyCount und mapInProgress: die Struktur
	// war gruen, der Verbraucher stieg nicht mit.
	reportsEntries, reportsGaps := 0, 0
	var zaehleBeads func(beads []any)
	zaehleBeads = func(beads []any) {
		for _, rawBead := range beads {
			rb := aMap(rawBead)
			reportsEntries += len(aArr(rb["entries"]))
			if aBool(rb["gap"]) {
				reportsGaps++
			}
			zaehleBeads(aArr(rb["children"]))
		}
	}
	for _, rawMap := range aArr(reportsc["maps"]) {
		rm := aMap(rawMap)
		zaehleBeads(aArr(rm["beads"]))
		reportsEntries += len(aArr(rm["other"]))
	}
	reportsCnt := itoa(reportsEntries)
	reportsNavDot := "g"
	if reportsGaps > 0 {
		reportsCnt += " · " + itoa(reportsGaps) + " Lücke(n)"
		reportsNavDot = "r"
	}

	// Die Bereichsansicht (gkp-8ww) ist die einzige Ansicht, die ueber ALLE
	// Projekte geht — die Aufgaben-Ansichten zeigen den Baum des gerade
	// geoeffneten Projekts. Deshalb steht sie in beiden Navigationen, im
	// Ueberblick wie im Projekt, und deshalb liegt ihr Datenblock neben
	// "projects" und nicht unter "reports".
	bereicheBlock := BereicheBlock(BereichProjekteAus(aArr(ctx["projects"])))
	bereicheItem := bereicheNav(bereicheBlock)

	ovItem := map[string]any{"id": "ov", "label": "Übersicht", "dot": "g"}
	globalGroup := map[string]any{
		"g": "Global · alle Projekte",
		"items": []any{
			bereicheItem,
			map[string]any{"id": "skills", "label": "Skills", "dot": "g", "tok": FmtCompact(skillsTok)},
			map[string]any{"id": "agents", "label": "Agents", "dot": "c", "tok": FmtCompact(agentsTok)},
			map[string]any{"id": "hooks", "label": "Hooks", "dot": ""},
			map[string]any{"id": "gclaude", "label": "CLAUDE.md", "dot": "c", "tok": FmtCompact(gdoc["tokens"])},
		},
	}
	projectGroups := []any{
		map[string]any{"g": "Wissen", "items": []any{
			map[string]any{"id": "pknow", "label": "knowledge/", "dot": "c"},
			map[string]any{"id": "psections", "label": "CLAUDE.md", "dot": "c", "tok": FmtCompact(pdoc["tokens"])},
			map[string]any{"id": "decisions", "label": "Decisions", "dot": "g"},
			map[string]any{"id": "memory", "label": "Memory", "dot": ""},
			map[string]any{"id": "lessons", "label": "Lektionen", "dot": "a"},
			map[string]any{"id": "changelog", "label": "CHANGELOG", "dot": ""},
		}},
		map[string]any{"g": "Git", "items": []any{
			map[string]any{"id": "git", "label": "Git Status", "dot": "g"},
			// GitHub hat hier absichtlich keine Zahl: die Karte holt ihre Daten
			// erst beim Ansehen ueber GET /github. Eine Zahl in der Navigation
			// waere entweder erfunden oder wuerde zwei Netzaufrufe in jeden
			// Seitenaufbau ziehen.
			map[string]any{"id": "github", "label": "GitHub", "dot": "c"},
		}},
		// Die sechs Aufgaben-Ansichten stehen NICHT mehr hier: gkp-s9n hat sie
		// bereits in den Kopf gehoben (.viewnav, render_page.go), und bis
		// gkp-dnz standen sie dort UND hier doppelt — links wie oben dieselben
		// sechs Zeilen. browser.js' viewNavGrundmenge() faellt genau dann auf
		// ihre eigene, ansichtsUEBERGREIFENDE Grundmenge zurueck (AV_REG plus
		// "bereiche"), wenn NAV keine Gruppe namens "Aufgaben" mehr fuehrt —
		// das gilt jetzt in JEDEM View, nicht nur ohne aktives Projekt.
		//
		// "Bereiche" bleibt trotzdem hier: es ist die EINZIGE Ansicht, die ueber
		// ALLE Projekte geht (bereicheNav-Kommentar oben), aber KEINE der sechs
		// Regeln aus ansicht.go — ansichtenNav (und damit auch die Kopf-
		// Navigation) kennt nur Regeln() und liefert fuer sie nie eine Zahl.
		// Bliebe sie nur im Kopf, verloere sie den einzigen Ort, an dem
		// appctx.bereicheNav ihre echte "X ohne Bereich"-Zahl zeigt
		// (TestNavigationszahlErfindetKeineNullWoGoKeineZahlLiefert).
		map[string]any{"g": "Bereiche", "items": []any{bereicheItem}},
		map[string]any{"g": "Beads", "items": []any{
			map[string]any{"id": "reports", "label": "Reports", "dot": reportsNavDot, "cnt": reportsCnt},
		}},
		map[string]any{"g": "Usage", "items": []any{
			map[string]any{"id": "sessions", "label": "Sessions", "dot": "m", "tok": FmtCompact(sessionsTok)},
		}},
		map[string]any{"g": "Kontext", "items": []any{
			map[string]any{"id": "context", "label": "Kontext", "dot": "m", "tok": FmtCompact(contextMeasuredTotal)},
		}},
	}
	var nav []any
	if view == "overview" {
		nav = []any{
			map[string]any{"g": "Überblick · alle Projekte", "items": []any{ovItem}},
			globalGroup,
		}
	} else {
		nav = append([]any{map[string]any{"g": "Übersicht", "items": []any{ovItem}}}, projectGroups...)
	}

	branch := ncChain(gitc["branch"], p["branch"], "?")

	lastTotal := 0.0
	if last != nil {
		lastTotal = aNum(last["input"]) + aNum(last["output"]) + aNum(last["cache_read"]) + aNum(last["cache_creation"])
	}
	costAv := aBool(costc["available"])

	// decisions overview (first 3)
	decisionsOv := []any{}
	for i, raw := range aArr(kn["decisions"]) {
		if i >= 3 {
			break
		}
		d := aMap(raw)
		decisionsOv = append(decisionsOv, map[string]any{"id": d["id"], "title": d["title"]})
	}

	// top tools
	toolFreq := aMap(tok["tool_freq"])
	type kv struct {
		k string
		v float64
	}
	pairs := make([]kv, 0, len(toolFreq))
	for k, v := range toolFreq {
		pairs = append(pairs, kv{k, aNum(v)})
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].v != pairs[j].v {
			return pairs[i].v > pairs[j].v
		}
		return pairs[i].k < pairs[j].k
	})
	topTools := []any{}
	for i, pr := range pairs {
		if i >= 4 {
			break
		}
		topTools = append(topTools, []any{pr.k, pr.v})
	}

	costVal := func(key string) any {
		if costAv {
			return FmtCost(costc[key])
		}
		return "n/a"
	}
	rlVal := func(key string) any {
		if !costAv {
			return nil
		}
		if v, ok := costc[key]; ok && v != nil {
			return v
		}
		return nil
	}

	weekTotalStr := "—"
	if _, ok := week["total"]; ok {
		weekTotalStr = FmtCompact(week["total"])
	}

	overview := map[string]any{
		"subtitle":      nc(p["repo"], "demo") + " · " + branch + " · " + aStr(ctx["generated_at"]),
		"skills":        len(skills),
		"agents":        len(agents),
		"hooks":         sumField(hooks, "count"),
		"skills_tok":    FmtCompact(skillsTok),
		"agents_tok":    FmtCompact(agentsTok),
		"branch":        branch,
		"dirty":         aBool(gitc["dirty"]),
		"git_recommend": aStr(gitc["recommend"]),
		"cost_today":    costVal("today"),
		"cost_week":     costVal("week"),
		"cost_total":    costVal("total"),
		"cost_5h":       rlVal("five_hour"),
		"cost_7d":       rlVal("seven_day"),
		"decisions":     decisionsOv,
		"tok_last":      dashIf(last == nil, FmtCompact(lastTotal)),
		"tok_week":      weekTotalStr,
		"cost_last":     dashIf(last == nil, FmtCost(lastCost(last))),
		"tok_sessions":  FmtCompact(sessionsTok),
		"cost_sessions": FmtCost(sessionsCost),
		"know_counts": map[string]any{
			"decisions": len(decisions), "memory": len(memory), "lektionen": len(lessons),
			"changelog": len(changelog), "pknow": len(pknow),
		},
		"errors_total":       aNum(tok["errors_total"]),
		"repeats_total":      aNum(tok["repeats_total"]),
		"top_tools":          topTools,
		"claude_tok_global":  FmtCompact(gdoc["tokens"]),
		"claude_tok_project": FmtCompact(pdoc["tokens"]),
		"proj_skills":        aNum(p["proj_skills_count"]),
		"proj_agents":        aNum(p["proj_agents_count"]),
		"proj_hooks":         aNum(p["proj_hooks_count"]),
		"proj_init_tok":      FmtCompact(aNum(gmd["tokens"]) + aNum(p["claude_md_tokens"])),
	}

	turns := 0.0
	if last != nil {
		turns = aNum(last["turns"])
	}
	meta := map[string]any{
		"cwd":          ctx["cwd"],
		"generated_at": ctx["generated_at"],
		"branch":       branch,
		"turns":        turns,
		"tok":          weekTotalStr,
	}

	return map[string]any{
		"meta":           meta,
		"nav":            nav,
		"coll":           coll,
		"git":            gitc,
		"cost":           costc,
		"reports":        reportsc,
		"overview":       overview,
		"projects":       orEmptyArr(ctx["projects"]),
		"bereiche":       bereicheBlock,
		"active_project": aStr(ctx["active_project"]),
	}
}
