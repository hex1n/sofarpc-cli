package contract

import "testing"

func TestParseTypeSpec_ListOfDTO(t *testing.T) {
	spec := ParseTypeSpec("java.util.List<com.foo.Req>")
	if spec.Base != "java.util.List" {
		t.Fatalf("base: got %q", spec.Base)
	}
	if len(spec.Args) != 1 || spec.Args[0].Base != "com.foo.Req" {
		t.Fatalf("args: %#v", spec.Args)
	}
}

func TestParseTypeSpec_MapNestedGeneric(t *testing.T) {
	spec := ParseTypeSpec("java.util.Map<java.lang.String, java.util.List<com.foo.Req>>")
	if spec.Base != "java.util.Map" {
		t.Fatalf("base: got %q", spec.Base)
	}
	if len(spec.Args) != 2 {
		t.Fatalf("args len: got %d", len(spec.Args))
	}
	if spec.Args[1].Base != "java.util.List" || len(spec.Args[1].Args) != 1 || spec.Args[1].Args[0].Base != "com.foo.Req" {
		t.Fatalf("nested arg: %#v", spec.Args[1])
	}
}

func TestParseTypeSpec_ArrayDepth(t *testing.T) {
	spec := ParseTypeSpec("com.foo.Req[][]")
	if spec.Base != "com.foo.Req" {
		t.Fatalf("base: got %q", spec.Base)
	}
	if spec.ArrayDepth != 2 {
		t.Fatalf("arrayDepth: got %d want 2", spec.ArrayDepth)
	}
}

func TestParseTypeSpec_WildcardExtends(t *testing.T) {
	spec := ParseTypeSpec("? extends com.foo.Base")
	if spec.Wildcard != WildcardExtends {
		t.Fatalf("wildcard: got %d", spec.Wildcard)
	}
	if spec.Base != "com.foo.Base" {
		t.Fatalf("base: got %q", spec.Base)
	}
}
