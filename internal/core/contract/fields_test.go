package contract

import (
	"testing"

	"github.com/hex1n/sofarpc-cli/internal/javamodel"
)

func TestResolvedFields_IncludesSuperclassFields(t *testing.T) {
	store := NewInMemoryStore(
		javamodel.Class{
			FQN:    "com.foo.Base",
			Kind:   javamodel.KindClass,
			Fields: []javamodel.Field{{Name: "id", JavaType: "java.lang.Long"}},
		},
		javamodel.Class{
			FQN:        "com.foo.Child",
			Kind:       javamodel.KindClass,
			Superclass: "com.foo.Base",
			Fields:     []javamodel.Field{{Name: "name", JavaType: "java.lang.String"}},
		},
	)

	fields := ResolvedFields(store, "com.foo.Child")
	if len(fields) != 2 {
		t.Fatalf("len(fields): got %d want 2", len(fields))
	}
	if fields[0].Name != "id" || fields[1].Name != "name" {
		t.Fatalf("fields: %#v", fields)
	}
}

func TestResolvedFields_IncludesParameterizedSuperclassFields(t *testing.T) {
	store := NewInMemoryStore(
		javamodel.Class{
			FQN:    "com.foo.Base",
			Kind:   javamodel.KindClass,
			Fields: []javamodel.Field{{Name: "id", JavaType: "java.lang.Long"}},
		},
		javamodel.Class{
			FQN:        "com.foo.Child",
			Kind:       javamodel.KindClass,
			Superclass: "com.foo.Base<com.foo.UserDto>",
			Fields:     []javamodel.Field{{Name: "name", JavaType: "java.lang.String"}},
		},
	)

	fields := ResolvedFields(store, "com.foo.Child")
	if len(fields) != 2 {
		t.Fatalf("len(fields): got %d want 2", len(fields))
	}
	if fields[0].Name != "id" || fields[1].Name != "name" {
		t.Fatalf("fields: %#v", fields)
	}
}

func TestResolvedFields_ChildOverridesParentField(t *testing.T) {
	store := NewInMemoryStore(
		javamodel.Class{
			FQN:    "com.foo.Base",
			Kind:   javamodel.KindClass,
			Fields: []javamodel.Field{{Name: "id", JavaType: "java.lang.Long"}},
		},
		javamodel.Class{
			FQN:        "com.foo.Child",
			Kind:       javamodel.KindClass,
			Superclass: "com.foo.Base",
			Fields:     []javamodel.Field{{Name: "id", JavaType: "java.lang.String"}},
		},
	)

	fields := ResolvedFields(store, "com.foo.Child")
	if len(fields) != 1 {
		t.Fatalf("len(fields): got %d want 1", len(fields))
	}
	if fields[0].JavaType != "java.lang.String" {
		t.Fatalf("field type: got %q", fields[0].JavaType)
	}
}
