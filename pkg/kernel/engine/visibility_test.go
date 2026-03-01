package engine

import (
	"testing"

	"github.com/ormasoftchile/gert/pkg/kernel/schema"
)

// T062: Visibility glob matching (* and **)
func TestCheckVisibility_NoVisibility(t *testing.T) {
	// nil visibility = everything allowed
	if !CheckVisibility(nil, "any.var.path") {
		t.Error("nil visibility should allow everything")
	}
}

func TestCheckVisibility_AllowOnly(t *testing.T) {
	vis := &schema.Visibility{
		Allow: []string{"question", "context.*"},
	}
	if !CheckVisibility(vis, "question") {
		t.Error("question should be allowed")
	}
	if !CheckVisibility(vis, "context.data") {
		t.Error("context.data should match context.*")
	}
	if CheckVisibility(vis, "secret") {
		t.Error("secret should be denied (not in allow list)")
	}
	if CheckVisibility(vis, "context.data.nested") {
		t.Error("context.data.nested should NOT match context.* (single segment)")
	}
}

func TestCheckVisibility_DenyOverridesAllow(t *testing.T) {
	vis := &schema.Visibility{
		Allow: []string{"scope.**"},
		Deny:  []string{"scope.round.0.*"},
	}
	if !CheckVisibility(vis, "scope.round.1.data") {
		t.Error("scope.round.1.data should be allowed")
	}
	if CheckVisibility(vis, "scope.round.0.secret") {
		t.Error("scope.round.0.secret should be denied (deny overrides allow)")
	}
}

func TestCheckVisibility_DoubleStarGlob(t *testing.T) {
	vis := &schema.Visibility{
		Allow: []string{"data.**"},
	}
	if !CheckVisibility(vis, "data.a") {
		t.Error("data.a should match data.**")
	}
	if !CheckVisibility(vis, "data.a.b.c") {
		t.Error("data.a.b.c should match data.**")
	}
	if CheckVisibility(vis, "other.a") {
		t.Error("other.a should not match data.**")
	}
}

func TestGlobMatch_Exact(t *testing.T) {
	if !globMatch("foo.bar", "foo.bar") {
		t.Error("exact match should work")
	}
	if globMatch("foo.bar", "foo.baz") {
		t.Error("non-match should fail")
	}
}

func TestGlobMatch_SingleStar(t *testing.T) {
	if !globMatch("foo.*", "foo.bar") {
		t.Error("foo.* should match foo.bar")
	}
	if globMatch("foo.*", "foo.bar.baz") {
		t.Error("foo.* should NOT match foo.bar.baz")
	}
}

func TestGlobMatch_DoubleStar(t *testing.T) {
	if !globMatch("foo.**", "foo.a.b.c") {
		t.Error("foo.** should match foo.a.b.c")
	}
	if !globMatch("**", "anything.at.all") {
		t.Error("** should match everything")
	}
	if !globMatch("foo.**.baz", "foo.a.b.baz") {
		t.Error("foo.**.baz should match foo.a.b.baz")
	}
}
