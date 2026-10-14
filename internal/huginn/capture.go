package huginn

import (
	"regexp"

	"github.com/yeixio/toskar-core/internal/typo"
)

// Capture is what a message asks the person to add before it can be
// answered: a photo, from the camera or the library, or a file.
type Capture string

const (
	// NoCapture: the message can be answered as it is.
	NoCapture Capture = ""
	// CaptureCamera: "take a picture of", "scan this receipt".
	CaptureCamera Capture = "camera"
	// CapturePhoto: "what plant is this?", "look at my latest photo".
	CapturePhoto Capture = "photo"
	// CaptureFile: "summarize this PDF", "read my document".
	CaptureFile Capture = "file"
)

// Some common misspellings (#451): picutre, pciture, foto, phot, pdf's.
const (
	pictureWord = `(?:pic(?:ture|utre|tue|ure)?s?|pciture|pics?|photo(?:graph)?s?|phot|foto|image|img|screenshot|screen ?shot|snap(?:shot)?)`
	fileWord    = `(?:pdfs?|pdf's|files?|fiel|document|doc|docx|spreadsheet|attachment|contract|report|resume|résumé|cv|slides|paper)`
)

// cameraRe matches asking to use the camera now.
var cameraRe = regexp.MustCompile(`(?i)\b(?:take|snap|shoot|grab)\s+(?:a|an|the|some)?\s*` + pictureWord + `\b|\b(?:scan|photograph)\s+(?:this|that|my|the|a|an)\b|\b(?:use|open)\s+(?:the|my)?\s*camera\b`)

// photoRe matches a question about a picture the person hasn't added: "what
// plant is this", "look at this", "my latest photo".
var photoRe = regexp.MustCompile(`(?i)\b(?:my|this|that|attached|last|latest|recent|newest)\s+(?:latest\s+|last\s+|recent\s+|newest\s+)?` + pictureWord + `\b|\b(?:look at|see|check out|identify)\s+(?:this|that|these|it)\b|^\s*(?:what|which|who)\s+(?:kind of\s+|type of\s+|sort of\s+)?(?:plant|flower|tree|bird|bug|insect|mushroom|animal|dog|cat|breed|rash|spider|snake|rock|car|thing|object|building|place|painting|font)\s+(?:is|are)\s+(?:this|that|these|those|it)\b|\b(?:read|translate)\s+(?:this|that)\s+(?:receipt|sign|label|menu|note|page|letter|bill|card|screen|handwriting)\b`)

// fileRe matches asking about a document the person hasn't added.
var fileRe = regexp.MustCompile(`(?i)\b(?:this|that|my|attached|these)\s+` + fileWord + `\b`)

// asksForHelp matches what the person wants done with what they'd add, so
// "my file server is down" isn't a request to add a file.
var asksForHelp = regexp.MustCompile(`(?i)\b(?:summari[sz]e|summary|read|review|explain|translate|check|look|see|what|which|who|identify|tell me|describe|analy[sz]e|proofread|extract|find|is (?:this|it)|does (?:this|it)|can you|could you|help)\b`)

// CaptureRequest reports what a message needs added before it can be
// answered, when nothing was attached: "What plant is this?" needs a
// photo, "Summarize this PDF" a file. Apps answer with a card to take or
// choose one, instead of a model guessing at a picture it can't see. Only
// the person's tap in the system camera or picker adds anything.
func CaptureRequest(message string) Capture {
	if c := captureRequest(message); c != NoCapture {
		return c
	}
	// The same request with its typos corrected (#451): "tke a foto".
	if fixed := captureWords.Fix(message); fixed != message {
		return captureRequest(fixed)
	}
	return NoCapture
}

var captureWords = typo.FromPatterns(cameraRe.String(), asksForHelp.String(), photoRe.String(), fileRe.String())

func captureRequest(message string) Capture {
	switch {
	case cameraRe.MatchString(message):
		return CaptureCamera
	case !asksForHelp.MatchString(message):
		return NoCapture
	case photoRe.MatchString(message):
		return CapturePhoto
	case fileRe.MatchString(message):
		return CaptureFile
	}
	return NoCapture
}
