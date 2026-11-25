package replylang

import "testing"

// Spanish with few of the most common words still reads as Spanish, not a
// tie with Portuguese: a topic check's refusal in the quality run (#510).
func TestDetectSpanishNotPortuguese(t *testing.T) {
	if got, ok := Detect("Por favor, pregúnteme sobre neumáticos para temporadas frías o si necesita reservar una rotación de neumáticos."); !ok || got != "es" {
		t.Errorf("Detect = %q, %v; want es", got, ok)
	}
	if got, ok := Detect("Você pode me dizer qual é o melhor pneu para o inverno?"); !ok || got != "pt" {
		t.Errorf("Portuguese: Detect = %q, %v; want pt", got, ok)
	}
}

// Everyday Spanish a voice answer starts with reads as Spanish, not
// Portuguese or nothing, so it isn't read in another language's voice.
func TestDetectEverydaySpanish(t *testing.T) {
	for _, s := range []string{
		"No tengo acceso a internet ahora mismo.",
		"Hace mucho frío afuera, unos cinco grados.",
		"Toskar funciona en tu propia computadora, sin enviar nada a la nube.",
		"Esta noche la luna estará llena.",
		"Después, añade la cebolla picada.",
		"La capital de Francia es París.",
		"Lo siento, no entendí bien la pregunta.",
	} {
		if got, ok := Detect(s); !ok || got != "es" {
			t.Errorf("Detect(%q) = %q, %v; want es", s, got, ok)
		}
	}
}
