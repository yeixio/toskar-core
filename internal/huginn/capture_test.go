package huginn

import "testing"

func TestCaptureRequest(t *testing.T) {
	cases := []struct {
		in   string
		want Capture
	}{
		{"Take a picture of my fridge and tell me what to cook", CaptureCamera},
		{"take a pic", CaptureCamera},
		{"Can you take a picutre of this?", CaptureCamera},
		{"scan this receipt", CaptureCamera},
		{"open the camera", CaptureCamera},
		{"What plant is this?", CapturePhoto},
		{"what kind of bird is that", CapturePhoto},
		{"Look at this", CapturePhoto},
		{"What's in my latest photo?", CapturePhoto},
		{"read this receipt", CapturePhoto},
		{"translate this menu please", CapturePhoto},
		{"Can you describe this image?", CapturePhoto},
		{"Summarize this PDF", CaptureFile},
		{"can you proofread my resume", CaptureFile},
		{"What does this contract say about rent?", CaptureFile},
		{"explain my spreadsheet", CaptureFile},

		{"What is a photograph?", NoCapture},
		{"Tell me about the photo booth's history", NoCapture},
		{"my file server is down", NoCapture},
		{"How do I convert a PDF to Word?", NoCapture},
		{"What is this called in French: dog", NoCapture},
		{"Hi!", NoCapture},
		{"What's the weather in Juneau?", NoCapture},
		{"Write a poem about my cat", NoCapture},
		{"Who painted the Mona Lisa?", NoCapture},
		// Asking for a new picture isn't asking to add one (toskar-apps#195):
		// "make" is a word, not a typo of "take".
		{"make a picture of a cat riding a bike", NoCapture},
		{"Make me a picture of a sunset", NoCapture},
		{"make an image of a dragon", NoCapture},
		{"Can you make a photo of a red car?", NoCapture},
		{"show me a picture of a cat", NoCapture},
	}
	for _, c := range cases {
		if got := CaptureRequest(c.in); got != c.want {
			t.Errorf("CaptureRequest(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
