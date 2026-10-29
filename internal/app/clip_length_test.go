package app

import "testing"

func TestAskedSeconds(t *testing.T) {
	for message, want := range map[string]float64{
		"Make a video of waves rolling onto a beach, just one second long": 1,
		"A 3s clip of a cat":                            3,
		"make a 1-minute video of a parade":             60,
		"a video of rain, half a minute":                30,
		"Make a video that's a minute long":             60,
		"a 2.5 second clip":                             2.5,
		"Mach ein Video von Wellen, zwei Sekunden lang": 2,
		"Mach ein Video, 4 Sekunden":                    4,
		"Haz un video de olas de 3 segundos":            3,
		"Fais une vidéo de vagues de cinq secondes":     5,
		"Fai un video di onde di due secondi":           2,
		"Faça um vídeo de ondas de 4 segundos":          4,
		"波の動画を3秒で作って":                                   3,
		"做一个五秒的海浪视频":                                    5,
		"做一個兩秒鐘的海浪影片":                                   2,
		"파도 영상을 3초로 만들어 줘":                              3,
	} {
		if got := askedSeconds(message); got != want {
			t.Errorf("askedSeconds(%q) = %v, want %v", message, got, want)
		}
	}
	for _, message := range []string{
		"Make a video of waves rolling onto a beach",
		"Make a second video, of the sunset this time",
		"Fais une seconde vidéo du coucher de soleil",
		"Haz un segundo video del atardecer",
		"Fai un secondo video del tramonto",
		"Faça um segundo vídeo do pôr do sol",
		"A video of my 2 sisters",
		"Make a 3s clip and then a 5s clip",
		"自分の犬の動画を作って",
		"十分な長さの波の動画を作って",
		"길이 초과 없이 영상을 만들어 줘",
		"Make a video of the 1990s",
	} {
		if got := askedSeconds(message); got != 0 {
			t.Errorf("askedSeconds(%q) = %v, want none", message, got)
		}
	}
}

func TestWithAskedLength(t *testing.T) {
	args := map[string]any{"prompt": "waves", "seconds": 2.0}
	got := withAskedLength("just one second long", "video.generate", args)
	if got["seconds"] != 1.0 || args["seconds"] != 2.0 {
		t.Fatalf("got %v, args %v", got, args)
	}
	if got := withAskedLength("a 10 second clip", "video.generate", map[string]any{}); got["seconds"] != 10.0 {
		t.Fatalf("a length past the most is passed, so the clip says it was shortened: %v", got)
	}
	if got := withAskedLength("one second", "image.generate", map[string]any{}); got["seconds"] != nil {
		t.Fatalf("a picture got a length: %v", got)
	}
	if got := withAskedLength("make a video of waves", "video.generate", map[string]any{"seconds": 3.0}); got["seconds"] != 3.0 {
		t.Fatalf("the model's length was dropped: %v", got)
	}
}
