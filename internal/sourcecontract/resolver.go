package sourcecontract

import "strings"

// typeResolver turns raw Java type strings seen during parsing into the
// canonical FQN form the rest of the pipeline consumes. The resolver
// follows Java's usual resolution order: locally-declared types,
// explicit imports, same-package source symbols, implicit java.lang, and
// explicit on-demand imports.
type typeResolver struct {
	pkg                string
	classFQN           string
	explicit           map[string]string
	wildcards          []string
	localTypes         map[string]string
	project            symbolTable
	typeParams         map[string]string
	issues             *[]typeResolutionIssue
	preserveTypeParams bool
}

type typeResolutionIssue struct {
	ClassFQN   string
	Type       string
	Reason     string
	Candidates []string
}

func (i typeResolutionIssue) message() string {
	msg := strings.TrimSpace(i.Type + ": " + i.Reason)
	if len(i.Candidates) > 0 {
		msg += " candidates=" + strings.Join(i.Candidates, ",")
	}
	return msg
}

func (r typeResolver) resolve(expr string) string {
	return r.resolveWithSeen(expr, nil)
}

func (r typeResolver) withTypeParams(params []parsedTypeParam) typeResolver {
	if len(params) == 0 {
		return r
	}
	next := r
	next.typeParams = cloneVisibleMap(r.typeParams)
	for _, param := range params {
		next.typeParams[param.name] = param.bound
	}
	return next
}

func (r typeResolver) resolveTemplate(expr string) string {
	r.preserveTypeParams = true
	return r.resolveWithSeen(expr, nil)
}

func (r typeResolver) resolveWithSeen(expr string, seenTypeParams map[string]bool) string {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return ""
	}
	if strings.HasPrefix(expr, "? extends ") {
		return "? extends " + r.resolveWithSeen(strings.TrimSpace(strings.TrimPrefix(expr, "? extends ")), seenTypeParams)
	}
	if strings.HasPrefix(expr, "? super ") {
		return "? super " + r.resolveWithSeen(strings.TrimSpace(strings.TrimPrefix(expr, "? super ")), seenTypeParams)
	}
	if expr == "?" {
		return expr
	}
	arraySuffix := ""
	for strings.HasSuffix(expr, "[]") {
		arraySuffix += "[]"
		expr = strings.TrimSpace(strings.TrimSuffix(expr, "[]"))
	}
	base, args := splitGeneric(expr)
	resolvedBase := r.resolveBaseWithSeen(base, seenTypeParams)
	if len(args) == 0 {
		return resolvedBase + arraySuffix
	}
	resolvedArgs := make([]string, 0, len(args))
	for _, arg := range args {
		resolvedArgs = append(resolvedArgs, r.resolveWithSeen(arg, seenTypeParams))
	}
	return resolvedBase + "<" + strings.Join(resolvedArgs, ", ") + ">" + arraySuffix
}

func (r typeResolver) resolveBase(base string) string {
	return r.resolveBaseWithSeen(base, nil)
}

func (r typeResolver) resolveBaseWithSeen(base string, seenTypeParams map[string]bool) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	if isPrimitive(base) {
		return base
	}
	if bound, ok := r.typeParams[base]; ok {
		if r.preserveTypeParams {
			return base
		}
		if strings.TrimSpace(bound) == "" {
			return "java.lang.Object"
		}
		if seenTypeParams[base] {
			return "java.lang.Object"
		}
		nextSeen := cloneBoolMap(seenTypeParams)
		nextSeen[base] = true
		return r.resolveWithSeen(bound, nextSeen)
	}
	if fqn, ok, ambiguous := r.resolveKnownBase(base); ok {
		return fqn
	} else if ambiguous {
		return base
	}
	if strings.Contains(base, ".") {
		return base
	}
	r.noteIssue(base, "unresolved type", nil)
	return base
}

func (r typeResolver) resolveKnownBase(base string) (string, bool, bool) {
	if fqn, ok, ambiguous := r.resolveSimpleBase(base); ok || ambiguous {
		return fqn, ok, ambiguous
	}
	head, tail, ok := strings.Cut(base, ".")
	if !ok {
		return "", false, false
	}
	if fqn, ok, ambiguous := r.resolveSimpleBase(head); ok {
		return fqn + "." + tail, true, false
	} else if ambiguous {
		return "", false, true
	}
	return "", false, false
}

func (r typeResolver) resolveSimpleBase(simple string) (string, bool, bool) {
	if simple == "" {
		return "", false, false
	}
	if fqn, ok := r.localTypes[simple]; ok {
		return fqn, true, false
	}
	if fqn, ok := r.explicit[simple]; ok {
		return fqn, true, false
	}
	if fqn, ok := r.project.lookup(r.pkg, simple); ok {
		return fqn, true, false
	}
	if fqn, ok := lookupJDK8PlatformSymbol("java.lang", simple); ok {
		return fqn, true, false
	}
	return r.resolveOnDemandBase(simple)
}

func (r typeResolver) resolveOnDemandBase(simple string) (string, bool, bool) {
	var candidates []string
	for _, prefix := range r.wildcards {
		if prefix == "" {
			continue
		}
		fqn, ok := r.resolvePackageSymbol(prefix, simple)
		if !ok {
			continue
		}
		if !containsString(candidates, fqn) {
			candidates = append(candidates, fqn)
		}
	}
	if len(candidates) == 1 {
		return candidates[0], true, false
	}
	if len(candidates) > 1 {
		r.noteIssue(simple, "ambiguous on-demand import", candidates)
		return "", false, true
	}
	return "", false, false
}

func (r typeResolver) resolvePackageSymbol(pkg, simple string) (string, bool) {
	if fqn, ok := r.project.lookup(pkg, simple); ok {
		return fqn, true
	}
	return lookupJDK8PlatformSymbol(pkg, simple)
}

func (r typeResolver) noteIssue(typeName, reason string, candidates []string) {
	if r.issues == nil {
		return
	}
	*r.issues = append(*r.issues, typeResolutionIssue{
		ClassFQN:   r.classFQN,
		Type:       typeName,
		Reason:     reason,
		Candidates: append([]string(nil), candidates...),
	})
}

func cloneBoolMap(input map[string]bool) map[string]bool {
	out := make(map[string]bool, len(input)+1)
	for key, value := range input {
		out[key] = value
	}
	return out
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
