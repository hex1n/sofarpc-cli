package javatype

import "testing"

type fakeLookup struct {
	supers map[string]string
	ifaces map[string][]string
}

func (f fakeLookup) Superclass(fqn string) (string, bool) {
	v, ok := f.supers[fqn]
	return v, ok
}

func (f fakeLookup) Interfaces(fqn string) ([]string, bool) {
	v, ok := f.ifaces[fqn]
	return v, ok
}

func TestClassify_Primitives(t *testing.T) {
	cases := []string{"int", "long", "boolean", "double"}
	for _, fqn := range cases {
		if got := Classify(fqn, nil); got != RolePassthrough {
			t.Fatalf("%s: want Passthrough, got %s", fqn, got)
		}
	}
}

func TestClassify_ArrayAndGenericsNormalized(t *testing.T) {
	if got := Classify("java.lang.String[]", nil); got != RolePassthrough {
		t.Fatalf("String[] should normalize to String: got %s", got)
	}
	if got := Classify("java.util.List<java.lang.String>", nil); got != RoleContainer {
		t.Fatalf("List<String> should be Container: got %s", got)
	}
}

func TestClassify_WalksSuperclassChain(t *testing.T) {
	lookup := fakeLookup{
		supers: map[string]string{
			"com.example.Order":      "com.example.BaseEntity",
			"com.example.BaseEntity": "java.lang.Object",
		},
	}
	if got := Classify("com.example.Order", lookup); got != RoleUserType {
		t.Fatalf("Order should be UserType (walks to Object which is passthrough but not container): got %s", got)
	}
}

func TestClassify_WalksInterfaceChainForCollection(t *testing.T) {
	lookup := fakeLookup{
		ifaces: map[string][]string{
			"com.example.PagedList": {"java.util.List"},
		},
	}
	if got := Classify("com.example.PagedList", lookup); got != RoleContainer {
		t.Fatalf("PagedList→List should be Container: got %s", got)
	}
}

func TestClassify_EnumViaSuperclass(t *testing.T) {
	lookup := fakeLookup{
		supers: map[string]string{
			"com.example.Color": "java.lang.Enum",
		},
	}
	if got := Classify("com.example.Color", lookup); got != RolePassthrough {
		t.Fatalf("enum via java.lang.Enum should be Passthrough: got %s", got)
	}
}

func TestClassify_UnknownWithoutLookupIsUserType(t *testing.T) {
	if got := Classify("com.example.Unknown", nil); got != RoleUserType {
		t.Fatalf("unknown types default to UserType: got %s", got)
	}
}

func TestClassify_EmptyReturnsUnknown(t *testing.T) {
	if got := Classify("", nil); got != RoleUnknown {
		t.Fatalf("empty input should be Unknown: got %s", got)
	}
}

func TestClassify_CyclesTolerated(t *testing.T) {
	lookup := fakeLookup{
		supers: map[string]string{
			"com.example.A": "com.example.B",
			"com.example.B": "com.example.A",
		},
	}
	if got := Classify("com.example.A", lookup); got != RoleUserType {
		t.Fatalf("cyclic chain should not hang, result UserType: got %s", got)
	}
}

func TestPlaceholder_Kinds(t *testing.T) {
	cases := []struct {
		fqn  string
		kind PlaceholderKind
	}{
		{"java.lang.String", PlaceholderString},
		{"java.lang.Boolean", PlaceholderBool},
		{"boolean", PlaceholderBool},
		{"int", PlaceholderNumber},
		{"java.lang.Long", PlaceholderNumber},
		{"java.math.BigDecimal", PlaceholderDecimal},
		{"java.time.LocalDate", PlaceholderDate},
		{"java.util.List", PlaceholderCollection},
		{"java.util.List<java.lang.String>", PlaceholderCollection},
		{"java.util.Map", PlaceholderMap},
		{"com.example.Order", PlaceholderObject},
	}
	for _, tc := range cases {
		if got := Placeholder(tc.fqn); got != tc.kind {
			t.Fatalf("%s: want %v got %v", tc.fqn, tc.kind, got)
		}
	}
}

func TestRenderPlaceholder_JSONLiterals(t *testing.T) {
	cases := map[PlaceholderKind]string{
		PlaceholderString:     `""`,
		PlaceholderBool:       `false`,
		PlaceholderNumber:     `0`,
		PlaceholderDecimal:    `"0"`,
		PlaceholderDate:       `"1970-01-01T00:00:00Z"`,
		PlaceholderCollection: `[]`,
		PlaceholderMap:        `{}`,
		PlaceholderObject:     `null`,
	}
	for kind, want := range cases {
		if got := string(RenderPlaceholder(kind)); got != want {
			t.Fatalf("%v: want %s got %s", kind, want, got)
		}
	}
}
