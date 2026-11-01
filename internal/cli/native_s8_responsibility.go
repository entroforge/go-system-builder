package cli

import (
	"encoding/json"
	"regexp"
	"strings"
)

// This recognizes an actual dispatch expression such as "for `hypothesis-X`
// (owner `investigator-X`)". References to another finding, hypothesis or
// owner elsewhere in the professional context are not dispatch assignments.
var nativeS8DispatchPair = regexp.MustCompile("(?im)(?:^|[.;])[ \\t]*(?:(?:you[ \\t]+are[ \\t]+the[ \\t]+native[ \\t]+S8[ \\t]+investigator[ \\t]+)|(?:S8[ \\t]+responsibilities[ \\t]*:[ \\t]*)|(?:you[ \\t]+are[ \\t]+assigned[ \\t]+(?:exactly[ \\t]+one[ \\t]+|the[ \\t]+)?S8[ \\t]+investigation[ \\t]*:[ \\t]*))?(?:for|investigate)[ \\t]+[\"'\\x60]?([A-Za-z0-9][A-Za-z0-9._-]*)[\"'\\x60]?[ \\t]*\\([ \\t]*owner(?:_agent_id)?[ \\t]*(?:[:=][ \\t]*|[\"'\\x60])([A-Za-z0-9._-]+)[\"'\\x60]?[ \\t]*\\)")
var nativeS8ContinuedDispatch = regexp.MustCompile("(?i)^[ \\t]*(?:investigate|for)[ \\t]+[\"'\\x60]?([A-Za-z0-9][A-Za-z0-9._-]*)[\"'\\x60]?[ \\t]*\\([ \\t]*owner(?:_agent_id)?[ \\t]*(?:[:=][ \\t]*|[\"'\\x60])([A-Za-z0-9._-]+)[\"'\\x60]?[ \\t]*\\)")
var nativeS8StructuredAssignment = regexp.MustCompile(`(?im)(?:^|[.;])[ \t]*(?:assignment|assigned[ \t]+responsibility)[ \t]*:[ \t]*(\{[^{}]*\})`)
var nativeReviewAssignedOwner = regexp.MustCompile("(?im)(?:^|[.;])[ \\t]*(?:assigned[ \\t]+owner|responsibility[ \\t]+owner)[ \\t]*(?:[:=][ \\t]*|[\"'\\x60])([A-Za-z0-9._-]+)")

// This is the paired responsibility in an actual S7 dispatch, not separate
// mentions of an Assignment and Claim in professional background material.
var nativeS7DispatchHeader = regexp.MustCompile(`(?im)(?:^|[.;])[ \t]*(?:you[ \t]+are[ \t]+(?:the[ \t]+native[ \t]+agent|responsible)[ \t]+for[ \t]+)?(?:(?:exactly[ \t]+one[ \t]+)?S7[ \t]+(?:verification[ \t]+)?responsibilit(?:y|ies)|assigned[ \t]+responsibilit(?:y|ies))[ \t]*:`)
var nativeS7FirstPair = regexp.MustCompile("(?i)^[ \\t]*(?:\\*\\*[^*\\r\\n]+\\*\\*[ \\t]*)?(?:\\r?\\n[ \\t]*(?:[-*][ \\t]*)?)?\\([ \\t]*[\"'\\x60]?(assignment-[A-Za-z0-9._-]+)[\"'\\x60]?[ \\t]*,[ \\t]*claim[ \\t]+[\"'\\x60]?(claim-[A-Za-z0-9._-]+)[\"'\\x60]?[ \\t]*\\)")
var nativeS7ItemSeparator = regexp.MustCompile(`(?i)^[ \t]*(?:(?:(?:,|;)[ \t]*(?:and[ \t]+)?|and[ \t]+)(?:\r?\n[ \t]*(?:[-*][ \t]*)?)?|(?:\r?\n[ \t]*(?:[-*][ \t]*)?))`)
var nativeS7AssignedPair = regexp.MustCompile("(?im)(?:^|[.;])[ \\t]*assigned[ \\t]+assignment(?:_id)?[ \\t]*[:=][ \\t]*[\"'\\x60]?(assignment-[A-Za-z0-9._-]+)[\"'\\x60]?[ \\t]*[,;][ \\t]*claim(?:_id)?[ \\t]*[:=][ \\t]*[\"'\\x60]?(claim-[A-Za-z0-9._-]+)")
var nativeS7OriginalPathField = regexp.MustCompile("(?im)(?:^|[.;)])[ \\t]*(?:[-*][ \\t]*)?(?:(?:original|private)[ \\t]+)?(?:questionnaire|view|answer)(?:[ \\t]+path)?(?:[ \\t]*\\([^\\r\\n)]*\\))?[ \\t]*:[ \\t]*(?:`([^`\\r\\n]+)`|\"([^\"\\r\\n]+)\"|'([^'\\r\\n]+)'|([^\\s,;]+))")
var nativeReviewMultipleDispatch = regexp.MustCompile(`(?im)(?:^|[.;])[ \t]*(?:(?:you[ \t]+are[ \t]+(?:the[ \t]+native[ \t]+agent|responsible)[ \t]+for[ \t]+)?(?:(?:the[ \t]+following|these|both)[ \t]+)?(?:S7|S8)?[ \t]*responsibilities|assigned[ \t]+responsibilities)[ \t]*:`)
var nativeReviewStageListHeader = regexp.MustCompile(`(?im)(?:^|[.;])[ \t]*(?:you[ \t]+are[ \t]+(?:the[ \t]+native[ \t]+agent|responsible)[ \t]+for[ \t]+)?(?:(?:the[ \t]+following|these|both)[ \t]+)?(S7|S8)[ \t]+responsibilities[ \t]*:`)
var nativeReviewListPath = regexp.MustCompile("(?i)^[ \\t]*(?:\\r?\\n[ \\t]*(?:[-*][ \\t]*)?)?(?:`([^`\\r\\n]+)`|\"([^\"\\r\\n]+)\"|'([^'\\r\\n]+)'|([^\\s,;(){}\\[\\]]+))")
var nativeS8ListItem = regexp.MustCompile("(?i)^[ \\t]*(?:\\r?\\n[ \\t]*(?:[-*][ \\t]*)?)?(?:(?:investigate|for)[ \\t]+)?[\"'\\x60]?([A-Za-z0-9][A-Za-z0-9._-]*)[\"'\\x60]?[ \\t]*\\([ \\t]*owner(?:_agent_id)?[ \\t]*(?:[:=][ \\t]*|[\"'\\x60])([A-Za-z0-9._-]+)[\"'\\x60]?[ \\t]*\\)")

type nativeReviewPathFact struct{ stage, path string }

type nativeReviewDispatchFacts struct {
	s7        [][2]string
	s8        []nativeS8Identity
	paths     []nativeReviewPathFact
	owner     string
	ownerMany bool
	owners    map[string]bool
	multiple  bool
	unknown   bool
}

type nativeReviewCandidate struct {
	owner, answer, source string
	packet, view          string
	assignment            string
	claims                []string
	hypothesis            string
	stage                 string
	refs                  []string
	err                   error
	diagnosticOnly        bool
}

func nativeReviewFacts(text string) nativeReviewDispatchFacts {
	s7, unknown := nativeS7DispatchIdentities(text)
	paths, listS8, listUnknown := nativeReviewStageLists(text)
	owner, ownerMany := nativeReviewDispatchOwner(text)
	owners := map[string]bool{}
	for _, item := range nativeReviewAssignedOwner.FindAllStringSubmatch(text, -1) {
		owners[item[1]] = true
	}
	return nativeReviewDispatchFacts{
		s7: s7, s8: append(nativeS8DispatchIdentities(text), listS8...), paths: paths, owner: owner,
		ownerMany: ownerMany, owners: owners, multiple: nativeReviewMultipleDispatch.MatchString(text), unknown: unknown || listUnknown,
	}
}

// Only items directly following a stage's explicit plural-dispatch header
// belong to that list. Free prose and unrelated paths are not list items.
func nativeReviewStageLists(text string) ([]nativeReviewPathFact, []nativeS8Identity, bool) {
	var paths []nativeReviewPathFact
	var identities []nativeS8Identity
	unknown := false
	for _, header := range nativeReviewStageListHeader.FindAllStringSubmatchIndex(text, -1) {
		stage := strings.ToUpper(text[header[2]:header[3]])
		rest := text[header[1]:]
		for {
			length := 0
			if stage == "S8" {
				if item := nativeS8ListItem.FindStringSubmatch(rest); item != nil {
					identities = append(identities, nativeS8Identity{item[1], item[2]})
					length = len(item[0])
				}
			}
			if length == 0 && stage == "S8" {
				trimmed := strings.TrimLeft(rest, " \t\r\n")
				prefix := len(rest) - len(trimmed)
				if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
					decoder := json.NewDecoder(strings.NewReader(trimmed))
					var raw json.RawMessage
					if decoder.Decode(&raw) != nil {
						unknown = true
						break
					}
					type entry struct {
						Hypothesis string `json:"hypothesis_id"`
						Owner      string `json:"owner_agent_id"`
					}
					var entries []entry
					if raw[0] == '[' {
						if json.Unmarshal(raw, &entries) != nil {
							unknown = true
							break
						}
					} else {
						var e entry
						if json.Unmarshal(raw, &e) != nil {
							unknown = true
							break
						}
						entries = append(entries, e)
					}
					for _, e := range entries {
						if e.Hypothesis == "" || e.Owner == "" {
							unknown = true
							continue
						}
						identities = append(identities, nativeS8Identity{e.Hypothesis, e.Owner})
					}
					length = prefix + int(decoder.InputOffset())
				}
			}
			if length == 0 {
				item := nativeReviewListPath.FindStringSubmatch(rest)
				if item == nil {
					break
				}
				value := ""
				for _, field := range item[1:] {
					if field != "" {
						value = field
						break
					}
				}
				if !strings.ContainsAny(value, "/\\") && !strings.HasPrefix(value, ".") {
					break
				}
				paths = append(paths, nativeReviewPathFact{stage: stage, path: value})
				length = len(item[0])
			}
			rest = rest[length:]
			separator := nativeS7ItemSeparator.FindString(rest)
			if separator == "" {
				break
			}
			rest = rest[len(separator):]
		}
	}
	return paths, identities, unknown
}

func nativeReviewOutputMessages(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return []string{string(raw)}
	}
	var out []string
	if context, ok := value["hookSpecificOutput"].(map[string]any); ok {
		if message, ok := context["additionalContext"].(string); ok && message != "" {
			out = append(out, message)
		}
	}
	if message, ok := value["systemMessage"].(string); ok && message != "" {
		out = append(out, message)
	}
	return out
}

func nativeS7ExplicitPathMatches(text, root, ref string) bool {
	for _, field := range nativeS7OriginalPathField.FindAllStringSubmatch(text, -1) {
		for _, value := range field[1:] {
			if value != "" && nativeS8TaskPath(value, root, ref) {
				return true
			}
		}
	}
	return false
}

func nativeS7DispatchIdentities(text string) ([][2]string, bool) {
	var pairs [][2]string
	uncertain := false
	for _, item := range nativeS7AssignedPair.FindAllStringSubmatch(text, -1) {
		pairs = append(pairs, [2]string{item[1], item[2]})
	}
	type structuredPair struct {
		Assignment string   `json:"assignment_id"`
		Claim      string   `json:"claim_id"`
		Claims     []string `json:"claim_ids"`
	}
	consume := func(rest string) (int, bool, bool) {
		if item := nativeS7FirstPair.FindStringSubmatch(rest); item != nil {
			pairs = append(pairs, [2]string{item[1], item[2]})
			return len(item[0]), true, true
		}
		trimmed := strings.TrimLeft(rest, " \t\r\n")
		prefix := len(rest) - len(trimmed)
		if len(trimmed) == 0 || (trimmed[0] != '(' && trimmed[0] != '{' && trimmed[0] != '[') {
			return 0, false, false
		}
		if trimmed[0] == '(' {
			return 0, true, false
		}
		decoder := json.NewDecoder(strings.NewReader(trimmed))
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil {
			return 0, true, false
		}
		var values []structuredPair
		if raw[0] == '[' {
			if json.Unmarshal(raw, &values) != nil {
				return 0, true, false
			}
		} else {
			var value structuredPair
			if json.Unmarshal(raw, &value) != nil {
				return 0, true, false
			}
			values = append(values, value)
		}
		for _, value := range values {
			if value.Claim != "" {
				value.Claims = append(value.Claims, value.Claim)
			}
			if value.Assignment == "" || len(value.Claims) == 0 {
				return 0, true, false
			}
			for _, claim := range value.Claims {
				pairs = append(pairs, [2]string{value.Assignment, claim})
			}
		}
		return prefix + int(decoder.InputOffset()), true, true
	}
	for _, header := range nativeS7DispatchHeader.FindAllStringIndex(text, -1) {
		rest := text[header[1]:]
		length, attempted, valid := consume(rest)
		if !valid {
			uncertain = uncertain || attempted
			continue
		}
		rest = rest[length:]
		for {
			separator := nativeS7ItemSeparator.FindString(rest)
			if separator == "" {
				break
			}
			next := rest[len(separator):]
			length, attempted, valid = consume(next)
			if !valid {
				uncertain = uncertain || attempted
				break
			}
			rest = next[length:]
		}
	}
	return pairs, uncertain
}

type nativeS8Identity struct{ hypothesis, owner string }

func nativeS8DispatchIdentities(text string) []nativeS8Identity {
	var identities []nativeS8Identity
	for _, item := range nativeS8DispatchPair.FindAllStringSubmatchIndex(text, -1) {
		identities = append(identities, nativeS8Identity{text[item[2]:item[3]], text[item[4]:item[5]]})
		rest := text[item[1]:]
		for {
			separator := nativeS7ItemSeparator.FindString(rest)
			if separator == "" {
				break
			}
			next := rest[len(separator):]
			continuation := nativeS8ContinuedDispatch.FindStringSubmatch(next)
			if continuation == nil {
				break
			}
			identities = append(identities, nativeS8Identity{continuation[1], continuation[2]})
			rest = next[len(continuation[0]):]
		}
	}
	var objects []string
	if whole := strings.TrimSpace(text); strings.HasPrefix(whole, "{") && json.Valid([]byte(whole)) {
		objects = append(objects, whole)
	}
	for _, item := range nativeS8StructuredAssignment.FindAllStringSubmatch(text, -1) {
		objects = append(objects, item[1])
	}
	for _, object := range objects {
		var value struct {
			Hypothesis string `json:"hypothesis_id"`
			Owner      string `json:"owner_agent_id"`
		}
		if json.Unmarshal([]byte(object), &value) == nil && value.Hypothesis != "" && value.Owner != "" {
			identities = append(identities, nativeS8Identity{value.Hypothesis, value.Owner})
		}
	}
	return identities
}

// An explicitly assigned owner can disambiguate another original path quoted
// only as background. Mere owner names or professional references cannot.
func nativeReviewDispatchOwner(text string) (string, bool) {
	owners := map[string]bool{}
	for _, item := range nativeReviewAssignedOwner.FindAllStringSubmatch(text, -1) {
		owners[item[1]] = true
	}
	if len(owners) != 1 {
		return "", len(owners) > 1
	}
	for owner := range owners {
		return owner, false
	}
	return "", false
}

// nativeS8TaskIdentity identifies an explicitly labelled owner and an exact
// registered hypothesis in one actual task. Conflicting explicit identities
// cannot nominate any packet, including one whose path happens to be present.
func nativeS8TaskIdentity(text, owner, hypothesis string) (matches, conflict bool) {
	for _, item := range nativeS8DispatchIdentities(text) {
		if item.hypothesis == hypothesis && item.owner == owner {
			matches = true
		} else {
			conflict = true
		}
	}
	return matches && !conflict, conflict
}

// A correct registered relative path remains meaningful within the already
// selected repository root; do not require it to be copied as an absolute path.
func nativeS8TaskPath(text, root, ref string) bool {
	if ref == "" {
		return false
	}
	if nativeS6ExactReference(text, ref) || nativeS6ExactReference(text, ref+".") {
		return true
	}
	if !strings.HasPrefix(ref, "/") && !strings.HasPrefix(ref, "./") && nativeS6ExactReference(text, "./"+ref) {
		return true
	}
	rel := strings.TrimPrefix(ref, root+"/")
	return rel != ref && (nativeS6ExactReference(text, rel) || nativeS6ExactReference(text, rel+"."))
}

// Both the worker's natural events and the actual Main Agent return use the
// same original dispatch facts. Matching a candidate does not establish
// uniqueness: the caller must retain all matches and reject ambiguity.
func nativeReviewOriginalCandidateMatch(facts nativeReviewDispatchFacts, text, root, stage, owner, hypothesis, assignment string, claims, refs []string) (bool, bool) {
	matched, conflict := false, false
	if stage == "S8" {
		for _, pair := range facts.s8 {
			if pair.owner == owner && pair.hypothesis == hypothesis {
				matched = true
			} else if !facts.multiple {
				conflict = true
			}
		}
		conflict = conflict || !facts.multiple && len(facts.s7) > 0
	}
	if stage == "S7" {
		for _, pair := range facts.s7 {
			ownsClaim := false
			for _, claim := range claims {
				if claim == pair[1] {
					ownsClaim = true
					break
				}
			}
			if pair[0] == assignment && ownsClaim {
				matched = true
			} else if !facts.multiple {
				conflict = true
			}
		}
		conflict = conflict || !facts.multiple && len(facts.s8) > 0
	}
	for _, ref := range refs {
		if (len(facts.s7) != 0 || len(facts.s8) != 0) && !matched {
			// A known assignment may quote other packets as background. Only
			// the value of a labelled original question/answer can conflict.
			if nativeS7ExplicitPathMatches(text, root, ref) {
				matched = true
			}
		} else if nativeS8TaskPath(text, root, ref) {
			matched = true
		}
	}
	return matched, conflict
}

// Selection consumes only issued Runtime coordinates. Plural dispatches are
// resolved item by item, so an unknown item does not erase a proven sibling.
func nativeReviewSelect(facts nativeReviewDispatchFacts, text, root string, candidates []nativeReviewCandidate) ([]nativeReviewCandidate, bool) {
	unknown := facts.unknown
	if facts.ownerMany && !facts.multiple {
		return nil, true
	}
	eligible := func(c nativeReviewCandidate) bool { return len(facts.owners) == 0 || facts.owners[c.owner] }
	narrowByOriginalPath := func(matches []nativeReviewCandidate) []nativeReviewCandidate {
		if len(matches) < 2 {
			return matches
		}
		var exact []nativeReviewCandidate
		for _, c := range matches {
			for _, ref := range c.refs {
				if nativeS7ExplicitPathMatches(text, root, ref) {
					exact = append(exact, c)
					break
				}
			}
		}
		if len(exact) == 1 {
			return exact
		}
		return matches
	}
	if !facts.multiple {
		var selected []nativeReviewCandidate
		current := false
		for _, c := range candidates {
			matched, conflict := nativeReviewOriginalCandidateMatch(facts, text, root, c.stage, c.owner, c.hypothesis, c.assignment, c.claims, c.refs)
			if !eligible(c) {
				matched = false
				for _, ref := range c.refs {
					matched = matched || nativeS7ExplicitPathMatches(text, root, ref)
				}
				conflict = matched
			}
			if !matched || c.diagnosticOnly && current {
				continue
			}
			if !c.diagnosticOnly && !current {
				selected, current = nil, true
			}
			selected = append(selected, c)
			unknown = unknown || conflict
		}
		if !unknown {
			selected = narrowByOriginalPath(selected)
		}
		if unknown || len(selected) != 1 {
			return nil, unknown || len(selected) > 1
		}
		return selected, false
	}
	var selected []nativeReviewCandidate
	seen := map[string]bool{}
	add := func(matches []nativeReviewCandidate) {
		matches = narrowByOriginalPath(matches)
		if len(matches) != 1 {
			unknown = true
			return
		}
		c := matches[0]
		if !seen[c.packet] {
			seen[c.packet] = true
			selected = append(selected, c)
		}
	}
	for _, pair := range facts.s7 {
		var matches []nativeReviewCandidate
		for _, c := range candidates {
			if c.stage != "S7" || !eligible(c) || c.assignment != pair[0] {
				continue
			}
			for _, claim := range c.claims {
				if claim == pair[1] {
					matches = append(matches, c)
					break
				}
			}
		}
		add(matches)
	}
	for _, pair := range facts.s8 {
		var matches []nativeReviewCandidate
		for _, c := range candidates {
			if c.stage == "S8" && eligible(c) && c.owner == pair.owner && c.hypothesis == pair.hypothesis {
				matches = append(matches, c)
			}
		}
		add(matches)
	}
	for _, path := range facts.paths {
		var matches []nativeReviewCandidate
		for _, c := range candidates {
			if c.stage != path.stage || !eligible(c) {
				continue
			}
			for _, ref := range c.refs {
				if nativeS8TaskPath(path.path, root, ref) {
					matches = append(matches, c)
					break
				}
			}
		}
		add(matches)
	}
	if len(facts.s7) == 0 && len(facts.s8) == 0 && len(facts.paths) == 0 {
		for _, c := range candidates {
			if !eligible(c) {
				continue
			}
			for _, ref := range c.refs {
				if nativeS7ExplicitPathMatches(text, root, ref) {
					if !seen[c.packet] {
						seen[c.packet] = true
						selected = append(selected, c)
					}
					break
				}
			}
		}
	}
	return selected, unknown
}
