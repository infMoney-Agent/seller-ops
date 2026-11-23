package screening

import "testing"

func TestPendingReviewCategories(t *testing.T) {
	tree := []*reviewCategoryNode{
		{Name: "completed", Products: 5, Pending: 0, Children: []*reviewCategoryNode{{Name: "done", Products: 5, Pending: 0}}},
		{Name: "mixed", Products: 12, Pending: 3, Children: []*reviewCategoryNode{
			{Name: "complete child", Products: 8, Pending: 0},
			{Name: "unfinished", Products: 4, Pending: 3, Children: []*reviewCategoryNode{{Name: "done leaf", Products: 1, Pending: 0}, {Name: "pending leaf", Products: 3, Pending: 3}}},
		}},
	}
	got := pendingReviewCategories(tree)
	if len(got) != 1 || got[0].Name != "mixed" || got[0].Products != 12 || got[0].Pending != 3 {
		t.Fatalf("ancestor counts changed: %#v", got)
	}
	if len(got[0].Children) != 1 || len(got[0].Children[0].Children) != 1 || got[0].Children[0].Children[0].Name != "pending leaf" {
		t.Fatal("completed descendants remain")
	}
	empty := pendingReviewCategories(tree[:1])
	if empty == nil || len(empty) != 0 {
		t.Fatal("completed queue must serialize as an empty array")
	}
}
