package simple

import (
	"slices"
	"testing"

	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/inventory"
)

// The three readings of "make a picture" agree on every kind of picture
// (#1225): the request to make one, the tools Huginn offers, and the offer
// to set picture making up. Copies of the list had drifted, and "make a
// watercolor of a lighthouse" made nothing.
func TestPictureKindsAgree(t *testing.T) {
	for _, msg := range []string{
		"Make a watercolor of a lighthouse at dusk.",
		"Create a cartoon of my cat",
		"Make a logo for my bakery",
		"Generate a portrait of a fox in a suit",
		"Make a wallpaper of the northern lights",
		"Create an illustration of a dragon",
		"Make a poster for the bake sale",
	} {
		if _, ok := askedForMedia(msg, ""); !ok {
			t.Errorf("%q isn't a request to make a picture", msg)
		}
		if got := huginn.ToolsFor(huginn.Chat, msg, []string{"image.generate", "files.create"}); !slices.Contains(got, "image.generate") {
			t.Errorf("%q isn't offered image.generate: %v", msg, got)
		}
		if a, ok := inventory.Wants(inventory.Snapshot{}, msg); !ok || a.ID != "image_generation" {
			t.Errorf("%q doesn't offer to set up pictures: %+v %v", msg, a, ok)
		}
	}
}
