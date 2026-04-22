package sourcecontract

import "strings"

// typeResolver turns raw Java type strings seen during parsing into the
// canonical FQN form the rest of the pipeline consumes. The resolver
// follows Java's usual resolution order: locally-declared types,
// explicit imports, java.lang, same package, wildcard imports.
type typeResolver struct {
	pkg        string
	explicit   map[string]string
	wildcards  []string
	localTypes map[string]string
}

func (r typeResolver) resolve(expr string) string {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return ""
	}
	if strings.HasPrefix(expr, "? extends ") {
		return "? extends " + r.resolve(strings.TrimSpace(strings.TrimPrefix(expr, "? extends ")))
	}
	if strings.HasPrefix(expr, "? super ") {
		return "? super " + r.resolve(strings.TrimSpace(strings.TrimPrefix(expr, "? super ")))
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
	resolvedBase := r.resolveBase(base)
	if len(args) == 0 {
		return resolvedBase + arraySuffix
	}
	resolvedArgs := make([]string, 0, len(args))
	for _, arg := range args {
		resolvedArgs = append(resolvedArgs, r.resolve(arg))
	}
	return resolvedBase + "<" + strings.Join(resolvedArgs, ", ") + ">" + arraySuffix
}

func (r typeResolver) resolveBase(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	if isPrimitive(base) {
		return base
	}
	if fqn, ok := r.resolveLocalBase(base); ok {
		return fqn
	}
	if strings.Contains(base, ".") {
		return base
	}
	if fqn, ok := r.explicit[base]; ok {
		return fqn
	}
	if isJavaLang(base) {
		return "java.lang." + base
	}
	if r.pkg != "" {
		return r.pkg + "." + base
	}
	for _, prefix := range r.wildcards {
		if prefix != "" {
			return prefix + "." + base
		}
	}
	return base
}

func (r typeResolver) resolveLocalBase(base string) (string, bool) {
	if len(r.localTypes) == 0 {
		return "", false
	}
	if fqn, ok := r.localTypes[base]; ok {
		return fqn, true
	}
	head, tail, ok := strings.Cut(base, ".")
	if !ok {
		return "", false
	}
	if fqn, ok := r.localTypes[head]; ok {
		return fqn + "." + tail, true
	}
	if fqn, ok := r.explicit[head]; ok {
		return fqn + "." + tail, true
	}
	if isJavaLang(head) {
		return "java.lang." + head + "." + tail, true
	}
	return "", false
}
