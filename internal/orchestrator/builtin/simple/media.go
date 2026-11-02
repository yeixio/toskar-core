package simple

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Pictures and clips are made by Toskar, not left to the model to choose:
// a small model asked to "draw a dog" often says it can't, though the tool
// is right there. When a message asks for an image, a change to an
// attached image, or a short clip, and the profile allows that tool, the
// model writes only what to make and Toskar calls the tool, as it does for
// files (makeFileFirst).
var (
	// imageAskRe asks for a picture: a verb that draws, or one that makes
	// with a kind of picture close after it.
	imageAskRe = regexp.MustCompile(`(?i)(\b(draw|paint|sketch|illustrate)\b|\b(make|create|generate|design|produce|render|give me|show me|do)\b[^.?!\n]{0,40}?\b(images?|pictures?|photos?|illustrations?|drawings?|paintings?|logos?|icons?|wallpapers?|artwork|portraits?|posters?|pics?)\b)`)
	// videoAskRe asks for a clip, or for a picture to move.
	videoAskRe = regexp.MustCompile(`(?i)(\b(make|create|generate|produce|render|give me|do)\b[^.?!\n]{0,40}?\b(videos?|clips?|animations?|movies?|gifs?)\b|\banimate\b|\bbring (it|this|that|the|my)( \w+)? to life\b|\bmake (it|this|that) move\b)`)
	// editAskRe asks to change something.
	editAskRe = regexp.MustCompile(`(?i)\b(edit|change|retouch|recolou?r|remove|replace|turn|make|add|brighten|darken|blur|fix)\b`)
	// refersRe points at what's already there: "it", "this photo".
	refersRe = regexp.MustCompile(`(?i)\b(it|this|that|the (image|picture|photo|background))\b`)
	// lookForRe is about pictures that exist: finding, describing, or
	// explaining them, not making one.
	lookForRe = regexp.MustCompile(`(?i)\b(find|search|look up|look for|browse|online|on the web|from the internet|google|stock|download|where can i|recommend|describe|explain|what'?s in)\b`)
	// aboutAbilityRe asks whether it can, with nothing to make: "can you
	// make images?". The capability answer has those.
	aboutAbilityRe = regexp.MustCompile(`(?i)^\s*(can|could|are|do|does|will|would)\b[^?]*\b(images?|pictures?|photos?|videos?|clips?|drawings?|animations?)\s*\??\s*$`)
	// editableRe and imageNoteRe find attached images in the reference
	// material.
	editableRe  = regexp.MustCompile(`call image\.edit with \{"file": "([^"]+)"\}`)
	imageNoteRe = regexp.MustCompile(`(?m)^Image [^\n:]*: ([^\n]+?)\. You cannot see it\.`)
)

// mediaRequest is a picture or clip a message asks Toskar to make.
type mediaRequest struct {
	Tool string
	// File is the attached image to change, or to bring to life.
	File string
}

// askedForMedia reports whether a message asks for an image, a change to
// an attached image, or a clip.
func askedForMedia(prompt, reference string) (mediaRequest, bool) {
	if howToRe.MatchString(prompt) || lookForRe.MatchString(prompt) || aboutAbilityRe.MatchString(prompt) {
		return mediaRequest{}, false
	}
	editable := ""
	if m := editableRe.FindStringSubmatch(reference); m != nil {
		editable = m[1]
	}
	attached := editable
	if attached == "" {
		if m := imageNoteRe.FindStringSubmatch(reference); m != nil {
			attached = m[1]
		}
	}
	switch {
	case videoAskRe.MatchString(prompt) && huginn.AsksForClip(prompt):
		req := mediaRequest{Tool: "video.generate"}
		if attached != "" && refersRe.MatchString(prompt) {
			req.File = attached
		}
		return req, true
	case editable != "" && editAskRe.MatchString(prompt) && (refersRe.MatchString(prompt) || !imageAskRe.MatchString(prompt)):
		return mediaRequest{Tool: "image.edit", File: editable}, true
	case imageAskRe.MatchString(prompt) || looseImageAsk(prompt):
		return mediaRequest{Tool: "image.generate"}, true
	}
	return mediaRequest{}, false
}

// People type fast: "make a picutre of a dog" asks for a picture too. A
// verb that makes, then within a few words one of these with a letter or
// two off, counts (#391 follow-up).
var (
	makeVerbs    = map[string]bool{"make": true, "create": true, "generate": true, "design": true, "produce": true, "render": true, "draw": true, "paint": true, "sketch": true, "give": true, "show": true}
	pictureNouns = []string{"image", "picture", "photo", "illustration", "drawing", "painting", "portrait", "poster", "wallpaper", "artwork"}
	wordRe       = regexp.MustCompile(`[\pL']+`)
)

// looseImageAsk reports a request for a picture with the kind of picture
// misspelled.
func looseImageAsk(prompt string) bool {
	words := wordRe.FindAllString(strings.ToLower(prompt), -1)
	for i, w := range words {
		if !makeVerbs[w] {
			continue
		}
		for _, next := range words[i+1 : min(len(words), i+7)] {
			if nearPictureNoun(next) {
				return true
			}
		}
	}
	return false
}

// nearPictureNoun is a word within reach of a kind of picture: one letter
// off, or two for a long word, singular or plural. Short words must be
// exact, so "log" isn't "logo".
func nearPictureNoun(word string) bool {
	word = strings.TrimSuffix(word, "s")
	for _, noun := range pictureNouns {
		limit := 1
		if len(noun) >= 9 {
			limit = 2
		}
		if len(word) >= 4 && editDistance(word, noun) <= limit {
			return true
		}
	}
	return false
}

// editDistance counts the letters to add, remove, change, or swap with the
// next one to turn a into b.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	d := make([][]int, len(ra)+1)
	for i := range d {
		d[i] = make([]int, len(rb)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(ra)][len(rb)]
}

// EventMakingMedia tells the UI that Toskar is making a picture or a clip:
// kind is image, edit, or video.
const EventMakingMedia = "chat.making_media"

const (
	describeImage = "Write only a description, in English, of the picture to make for an image model: the subject, the setting, the style, and the lighting, in one to three sentences. No explanation, no quotes."
	describeEdit  = "Write only the change to make to the image, in English, as one short instruction such as \"make it night\". No explanation, no quotes."
	describeVideo = "Write only a description, in English, of the short clip to make for a video model: the subject, the motion, the camera, and the style, in one to three sentences. No explanation, no quotes."
)

// maxMediaPrompt keeps a description within what the tools take.
const maxMediaPrompt = 1500

// makeMediaFirst makes a picture or clip a message asks for, when the
// profile allows the tool. It returns the reply to show.
func makeMediaFirst(ctx context.Context, env pluginapi.ExecutionEnvironment, profile contracts.AIProfile, role string, messages []pluginapi.ChatMessage, prompt, reference string) (string, *pluginapi.GenerationMetrics, bool) {
	req, ok := askedForMedia(prompt, reference)
	if !ok {
		return "", nil, false
	}
	// A profile that keeps the tool off says so, rather than leaving the
	// model to send the person to a website for one.
	if !toolEnabled(profile, req.Tool) {
		what := map[string]string{"image.generate": "Making pictures", "image.edit": "Changing pictures", "video.generate": "Making clips"}[req.Tool]
		tell := fmt.Sprintf("%s is turned off for this chat's profile, %q, so nothing was made. In one or two short sentences, in the language of the user's last message, tell them that, and that they can turn it on for the profile in Profiles & Orchestration (or pick another profile for this chat). Don't suggest other websites or apps.", what, profile.Name)
		reply, m, err := generateText(ctx, env, role, withInstruction(messages, tell))
		if err != nil || strings.TrimSpace(reply) == "" {
			reply = fmt.Sprintf("%s is turned off for the profile %q. Turn it on in Profiles & Orchestration, or pick another profile for this chat.", what, profile.Name)
		}
		return strings.TrimSpace(reply), m, true
	}
	return makeMedia(ctx, env, profile, role, messages, prompt, req)
}

// refusesPictureRe is an answer that won't make a picture, or sends the
// person elsewhere for one: "I can't create images", "try DALL-E", ASCII
// art.
var refusesPictureRe = regexp.MustCompile(`(?i)\b(can'?t|cannot|unable to|not able to|don'?t have the (ability|capability) to|not capable of)\s+(directly\s+)?(generate|create|make|draw|produce|render)\b[^.\n]{0,40}\b(images?|pictures?|photos?|drawings?|art(work)?)\b|\b(dall-?e|midjourney|stable diffusion|ascii art|online image generators?|image generator (website|site|tool)s?)\b`)

// pictureTalkRe is a message about pictures at all, so a refusal is about
// making one.
var pictureTalkRe = regexp.MustCompile(`(?i)\b(draw|paint|sketch|illustrate|images?|pictures?|photos?|pics?|drawings?|paintings?|illustrations?|portraits?)\b`)

// makeMediaAfterRefusal makes the picture a model declined to, when the
// message is about one and image generation is allowed: a backstop for a
// request worded in a way askedForMedia doesn't know.
func makeMediaAfterRefusal(ctx context.Context, env pluginapi.ExecutionEnvironment, profile contracts.AIProfile, role string, messages []pluginapi.ChatMessage, prompt, answer string) (string, *pluginapi.GenerationMetrics, bool) {
	if !refusesPictureRe.MatchString(answer) || howToRe.MatchString(prompt) || lookForRe.MatchString(prompt) || aboutAbilityRe.MatchString(prompt) {
		return "", nil, false
	}
	if !pictureTalkRe.MatchString(prompt) && !looseImageAsk(prompt) && !anyNearPictureNoun(prompt) {
		return "", nil, false
	}
	return makeMedia(ctx, env, profile, role, messages, prompt, mediaRequest{Tool: "image.generate"})
}

// anyNearPictureNoun is a message with a kind of picture in it, however
// it's spelled.
func anyNearPictureNoun(prompt string) bool {
	for _, w := range wordRe.FindAllString(strings.ToLower(prompt), -1) {
		if nearPictureNoun(w) {
			return true
		}
	}
	return false
}

// makeMedia makes a picture or clip for req, when the profile allows the
// tool. It returns the reply to show.
func makeMedia(ctx context.Context, env pluginapi.ExecutionEnvironment, profile contracts.AIProfile, role string, messages []pluginapi.ChatMessage, prompt string, req mediaRequest) (string, *pluginapi.GenerationMetrics, bool) {
	if !toolEnabled(profile, req.Tool) {
		return "", nil, false
	}
	kind, ask := "image", describeImage
	switch req.Tool {
	case "image.edit":
		kind, ask = "edit", describeEdit
	case "video.generate":
		kind, ask = "video", describeVideo
	}
	env.Emit(EventMakingMedia, map[string]any{"kind": kind})
	// The model writes only what to make; a model that can't still gets
	// the person's own words.
	described, metrics, err := generateText(ctx, env, role, withInstruction(messages, ask+" Use the conversation above."))
	description := mediaDescription(described)
	if err != nil || description == "" {
		description = requestText(prompt)
	}
	args := map[string]any{"prompt": description}
	for k, v := range askedFormat(prompt, req.Tool) {
		args[k] = v
	}
	switch req.Tool {
	case "image.edit":
		args["file"] = req.File
	case "video.generate":
		if req.File != "" {
			args["image"] = req.File
		}
	}
	tell := "The " + map[string]string{"image": "picture", "edit": "changed picture", "video": "clip"}[kind] + " was made and is attached below your answer, where it is shown. In one short sentence, in the language of the user's last message, tell them it's below. Don't describe it, and don't say you can't make pictures: you just did."
	_, toolErr := env.ExecuteTool(ctx, req.Tool, args)
	if toolErr != nil {
		tell = "Making the " + map[string]string{"image": "picture", "edit": "change", "video": "clip"}[kind] + " failed: " + publicToolError(toolErr) + "\nIn one or two short sentences, in the language of the user's last message, tell them what happened and what they can do about it. Don't offer to describe a picture instead."
	}
	reply, m, err := generateText(ctx, env, role, withInstruction(messages, tell))
	if m != nil {
		metrics = m
	}
	if err != nil || strings.TrimSpace(reply) == "" {
		reply = fallbackMediaReply(kind, toolErr)
	}
	return strings.TrimSpace(reply), metrics, true
}

// withInstruction adds an instruction to the user's last turn, so roles
// keep alternating.
func withInstruction(messages []pluginapi.ChatMessage, instruction string) []pluginapi.ChatMessage {
	ask := append([]pluginapi.ChatMessage(nil), messages...)
	last := &ask[len(ask)-1]
	last.Content += "\n\n" + instruction
	return ask
}

var mediaLabelRe = regexp.MustCompile(`(?i)^\s*(prompt|description|image|picture|video|clip|instruction)\s*:\s*`)

// mediaDescription cleans what the model wrote: a fence, a label, quotes.
func mediaDescription(text string) string {
	text = fileBody(text, false)
	text = mediaLabelRe.ReplaceAllString(text, "")
	text = strings.Trim(strings.TrimSpace(text), "\"'“”")
	if r := []rune(text); len(r) > maxMediaPrompt {
		text = string(r[:maxMediaPrompt])
	}
	return strings.TrimSpace(text)
}

var politeRe = regexp.MustCompile(`(?i)^\s*((please|hey|hi|ok|okay)[, ]+)*((can|could|would|will) you|are you able to|i('d| would) like you to|i want you to)?\s*(please\s+)?`)

// requestText is the person's own words, without asking politely.
func requestText(prompt string) string {
	text := politeRe.ReplaceAllString(strings.TrimSpace(prompt), "")
	text = strings.TrimRight(text, " ?!.")
	if r := []rune(text); len(r) > maxMediaPrompt {
		text = string(r[:maxMediaPrompt])
	}
	if text == "" {
		return prompt
	}
	return text
}

// fallbackMediaReply is said when the model can't word the reply.
func fallbackMediaReply(kind string, failed error) string {
	if failed != nil {
		return "I couldn't make it: " + publicToolError(failed)
	}
	switch kind {
	case "video":
		return "Here is the clip. It's attached below."
	case "edit":
		return "Here is the changed picture. It's attached below."
	}
	return "Here is the picture. It's attached below."
}
