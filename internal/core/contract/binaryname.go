package contract

import "strings"

// JVM class-name reality: nesting depth beyond a few levels is unheard
// of, and even constant-pool-limited names stay well under 4 KiB. The
// bounds only need to be far above real usage while keeping worst-case
// resolution cost at maxSeparators × maxLength per name — and at
// maxFallbackCandidatesPerRequest × maxLength per request.
const (
	maxResolvableTypeNameLength    = 4096
	maxResolvableNestingSeparators = 64
	// One request-scoped resolver (a WireParamTypes call, a
	// RewriteWireTypeNames tree, or a NormalizeArgs call) may build this
	// many fallback candidates in total. Real inputs hold a handful of
	// nested names with one or two dollars each — and repeats are served
	// from the memo — so the budget never bites them, while a flood of
	// just-under-the-per-name-limit junk tokens exhausts it and passes
	// through unchanged instead of multiplying allocations.
	maxFallbackCandidatesPerRequest = 256
	maxResolutionMemoEntries        = 256
)

// typeNameResolver shares one fallback budget and one memo across every
// name resolved within a single caller-facing request, so the volume of
// caller-supplied '$'-heavy tokens — whether packed into one expression,
// spread over many parameter types, or scattered through an args tree —
// cannot multiply the resolution work. All input reaching it runs before
// the invoke limiter, which is why the bound is per request, not per name.
type typeNameResolver struct {
	store  Store
	budget int
	memo   map[string]string
}

func newTypeNameResolver(store Store) *typeNameResolver {
	return &typeNameResolver{store: store, budget: maxFallbackCandidatesPerRequest}
}

func (r *typeNameResolver) storeName(name string) string {
	trimmed := strings.TrimSpace(name)
	if !strings.Contains(trimmed, "$") {
		return trimmed
	}
	if hit, ok := r.memo[trimmed]; ok {
		return hit
	}
	resolved := storeTypeNameBudget(trimmed, r.store, &r.budget)
	if len(r.memo) < maxResolutionMemoEntries {
		if r.memo == nil {
			r.memo = make(map[string]string)
		}
		r.memo[trimmed] = resolved
	}
	return resolved
}

func (r *typeNameResolver) wireName(original string) string {
	if cls, ok := classFor(r.store, r.storeName(original)); ok && cls.BinaryName != "" {
		return cls.BinaryName
	}
	return original
}

// requestScopedStore lets NormalizeArgs share one resolver across its
// whole recursion without threading a parameter through every normalize
// function: storeTypeName / wireTypeName unwrap it and charge the shared
// budget, while Store reads delegate to the embedded implementation.
type requestScopedStore struct {
	Store
	resolver *typeNameResolver
}

func withRequestScopedResolution(store Store) Store {
	if store == nil {
		return nil
	}
	return requestScopedStore{Store: store, resolver: newTypeNameResolver(store)}
}

// storeTypeName resolves a caller-supplied class name to the spelling the
// Store indexes it by. A literal '$' is legal in Java identifiers, so the
// exact spelling wins when the Store knows it (com.foo.Dollar$Request can
// be a top-level class). Otherwise dollars are reinterpreted as nesting
// separators right-to-left, one at a time, until the Store recognises the
// result — com.foo.Dollar$Request$Inner resolves to the indexed
// com.foo.Dollar$Request.Inner. A name the Store cannot resolve under any
// interpretation is returned unchanged: dollar/dot equivalence is a
// Store-verified fact, never assumed for unknown classes. (Known limit: a
// literal '$' inside a *nested* class's own simple name would need a
// non-suffix conversion and stays unresolved — its canonical spelling
// still works, only the explicit binary spelling is affected.)
func storeTypeName(name string, store Store) string {
	if rs, ok := store.(requestScopedStore); ok {
		return rs.resolver.storeName(name)
	}
	budget := maxResolvableNestingSeparators
	return storeTypeNameBudget(name, store, &budget)
}

// storeTypeNameBudget is storeTypeName with the fallback work charged to a
// caller-owned budget: every candidate built decrements it, and a spent
// budget skips resolution (the name comes back unchanged, like any other
// unresolvable spelling — for a JVM binary spelling that is already the
// correct wire form, so exhaustion never corrupts an ordinary request).
func storeTypeNameBudget(name string, store Store, budget *int) string {
	name = strings.TrimSpace(name)
	if !strings.Contains(name, "$") {
		return name
	}
	if _, ok := classFor(store, name); ok {
		return name
	}
	// Progressive resolution builds one full-length candidate per
	// separator. Caller-supplied names arrive unconstrained over MCP and
	// replay payloads, so bound the work before it turns quadratic —
	// real class names sit far inside both limits, and anything beyond
	// them is returned unchanged like any other unresolvable name.
	if len(name) > maxResolvableTypeNameLength ||
		strings.Count(name, "$") > maxResolvableNestingSeparators {
		return name
	}
	candidate := name
	for {
		if *budget <= 0 {
			return name
		}
		i := strings.LastIndex(candidate, "$")
		if i < 0 {
			return name
		}
		*budget--
		candidate = candidate[:i] + "." + candidate[i+1:]
		if _, ok := classFor(store, candidate); ok {
			return candidate
		}
	}
}

// wireTypeName returns the name a value's @type must carry on the Hessian2
// wire: the JVM binary name when the store knows the class, otherwise the
// caller's original spelling — the store cannot improve on a class it does
// not know, and rewriting blindly would corrupt external type names.
func wireTypeName(original string, store Store) string {
	if rs, ok := store.(requestScopedStore); ok {
		return rs.resolver.wireName(original)
	}
	budget := maxResolvableNestingSeparators
	if cls, ok := classFor(store, storeTypeNameBudget(original, store, &budget)); ok && cls.BinaryName != "" {
		return cls.BinaryName
	}
	return original
}

// RewriteWireTypeNames walks a JSON-like args tree and rewrites every
// "@type" string to its wire form via one shared resolver. It validates
// nothing and never fails: unknown classes keep their original spelling.
// This is the rescue pass for paths that bypass NormalizeArgs — the
// skeleton-as-args branch and replayed plans captured before type names
// were fixed.
func RewriteWireTypeNames(args []any, store Store) []any {
	if store == nil || len(args) == 0 {
		return args
	}
	resolver := newTypeNameResolver(store)
	out := make([]any, len(args))
	for i, arg := range args {
		out[i] = rewriteWireTypeValue(arg, resolver)
	}
	return out
}

// WireParamTypes rewrites the class names inside method parameter type
// expressions to their wire (JVM binary) form. SofaRequest.methodArgSigs
// carries these strings verbatim and the server loads them with
// Class.forName, exactly like @type values. Expressions whose classes the
// store does not know — or whose wire form is identical — stay
// byte-for-byte unchanged, so non-nested signatures never move. All
// expressions of one call share one resolver.
func WireParamTypes(paramTypes []string, store Store) []string {
	if store == nil || len(paramTypes) == 0 {
		return paramTypes
	}
	resolver := newTypeNameResolver(store)
	out := make([]string, len(paramTypes))
	for i, pt := range paramTypes {
		out[i] = wireTypeExpr(pt, resolver)
	}
	return out
}

// wireTypeExpr rewrites an expression in a single pass: each maximal run
// of type-name characters is one complete class name (so a shorter name
// can never corrupt a longer one sharing its prefix), rewritten through
// the shared resolver individually. Cost stays linear in the expression
// length no matter how many resolvable names it contains. Untouched
// expressions are returned byte-for-byte.
func wireTypeExpr(expr string, resolver *typeNameResolver) string {
	var b strings.Builder
	changed := false
	for i := 0; i < len(expr); {
		if !isTypeNameChar(expr[i]) {
			b.WriteByte(expr[i])
			i++
			continue
		}
		j := i + 1
		for j < len(expr) && isTypeNameChar(expr[j]) {
			j++
		}
		token := expr[i:j]
		wire := resolver.wireName(token)
		if wire != token {
			changed = true
		}
		b.WriteString(wire)
		i = j
	}
	if !changed {
		return expr
	}
	return b.String()
}

func isTypeNameChar(c byte) bool {
	return c == '.' || c == '$' || c == '_' ||
		(c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func rewriteWireTypeValue(value any, resolver *typeNameResolver) any {
	if values, ok := stringMap(value); ok {
		out := make(map[string]any, len(values))
		for key, item := range values {
			if key == "@type" {
				if name, ok := item.(string); ok && strings.TrimSpace(name) != "" {
					out[key] = resolver.wireName(strings.TrimSpace(name))
					continue
				}
			}
			out[key] = rewriteWireTypeValue(item, resolver)
		}
		return out
	}
	if items, ok := sliceValues(value); ok {
		out := make([]any, len(items))
		for i, item := range items {
			out[i] = rewriteWireTypeValue(item, resolver)
		}
		return out
	}
	return value
}
